package repository

// 团队计费的仓储层实现（sub2api 二开，fork-only）。
//
// 上游 usage_billing_repo.go 的 applyUsageBillingEffects 只保留一行带
// `// sub2api-custom: team billing` 标记的调用 deductTeamAwareUsageBillingBalance；
// 上游原有的 deductUsageBillingBalance 原样保留（便于合并），生产路径不再调用它。
// 合并上游后请运行 service/team_billing.go 顶部列出的守护测试。

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// 上游的单资金池扣费保留原样以减少合并冲突（仍有上游单测覆盖），这里显式引用避免 unused 告警。
var _ = deductUsageBillingBalance

// deductTeamAwareUsageBillingBalance 是 applyUsageBillingEffects 中余额扣费的团队版：
// 返回值与上游 deductUsageBillingBalance 相同（扣后个人余额、是否足额扣到），
// 并把个人 / 团队拆分结果以及未能扣到的金额写进 result。
func deductTeamAwareUsageBillingBalance(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand, result *service.UsageBillingApplyResult) (float64, bool, error) {
	deduction, err := deductTeamUsageBillingBalance(ctx, tx, cmd.UserID, cmd.BalanceCost, cmd.BalanceMode)
	if err != nil {
		return 0, false, err
	}
	newTeamBalance := deduction.newTeamBalance
	result.NewTeamBalance = &newTeamBalance
	result.PersonalBalanceCost = deduction.personalPart
	result.TeamBalanceCost = deduction.teamPart
	result.UncoveredBalanceCost = deduction.uncovered
	return deduction.newBalance, deduction.sufficient, nil
}

// usageBillingBalanceDeduction 描述一次余额扣费在个人余额与团队额度之间的拆分结果。
type usageBillingBalanceDeduction struct {
	newBalance     float64
	newTeamBalance float64
	personalPart   float64
	teamPart       float64
	// uncovered 是两个资金池都不够、因此没有扣到的金额（不透支，由站点承担）。
	uncovered float64
	// sufficient 为 false 表示本次费用没有足额扣到（uncovered > 0）。
	sufficient bool
}

// teamUsageBillingDeductSQL 是“不透支”的团队拆分扣费（$1 金额，$2 用户 ID，$3 已规范化的扣费方式）。
// 个人余额与团队额度都只扣到 0 为止，绝不会因为用量扣费变成负数：
//
//	非团队成员（任何模式）  只扣个人余额
//	team_first             先扣团队额度，不够的部分再扣个人余额
//	personal_first         先扣个人余额，不够的部分再扣团队额度
//	team_only              只扣团队额度（尊重用户的显式选择，不动个人余额）
//	personal_only          只扣个人余额（同上，不动团队额度）
//
// 两个资金池按上述规则都扣完仍不够的部分不再扣（uncovered），由调用方记录日志。
// 已经是负数的旧余额（历史透支）按 0 处理：不会被继续扣，也不会被这里补平。
// 与 DeductBalanceByMode（兼容路径）共用同一条 SQL，保证两条路径规则一致。
const teamUsageBillingDeductSQL = `
	WITH current_user_row AS (
		SELECT u.id,
			GREATEST(u.balance, 0) AS personal_avail,
			GREATEST(COALESCE(u.team_balance, 0), 0) AS team_avail,
			EXISTS (
				SELECT 1 FROM team_memberships tm
				WHERE tm.user_id = u.id AND tm.status IN ('active', 'exit_pending')
			) AS is_team_member
		FROM users u
		WHERE u.id = $2 AND u.deleted_at IS NULL
		FOR UPDATE
	), split AS (
		SELECT id,
			CASE
				WHEN NOT is_team_member OR $3::text = 'personal_only' THEN 0::numeric
				WHEN $3::text = 'personal_first' THEN LEAST(team_avail, $1::numeric - LEAST(personal_avail, $1::numeric))
				ELSE LEAST(team_avail, $1::numeric)
			END AS team_part,
			CASE
				WHEN NOT is_team_member OR $3::text IN ('personal_only', 'personal_first') THEN LEAST(personal_avail, $1::numeric)
				WHEN $3::text = 'team_only' THEN 0::numeric
				ELSE LEAST(personal_avail, $1::numeric - LEAST(team_avail, $1::numeric))
			END AS personal_part
		FROM current_user_row
	), updated AS (
		UPDATE users u
		SET balance = u.balance - s.personal_part,
			team_balance = u.team_balance - s.team_part,
			updated_at = NOW()
		FROM split s
		WHERE u.id = s.id
		RETURNING u.id, u.balance, u.team_balance
	)
	SELECT updated.balance, updated.team_balance, split.team_part, split.personal_part,
		$1::numeric - split.personal_part - split.team_part AS uncovered
	FROM updated
	JOIN split ON split.id = updated.id
`

// deductTeamUsageBillingBalance 按 API Key 的扣费方式在同一条原子 UPDATE 中
// 拆分扣减个人余额（users.balance）与团队额度（users.team_balance），规则见 teamUsageBillingDeductSQL。
// 资金不足时不透支：能扣多少扣多少，剩余部分记为 uncovered，sufficient=false。
func deductTeamUsageBillingBalance(ctx context.Context, tx *sql.Tx, userID int64, amount float64, mode string) (*usageBillingBalanceDeduction, error) {
	mode = service.NormalizeBalanceMode(mode)
	var out usageBillingBalanceDeduction
	err := tx.QueryRowContext(ctx, teamUsageBillingDeductSQL, amount, userID, mode).
		Scan(&out.newBalance, &out.newTeamBalance, &out.teamPart, &out.personalPart, &out.uncovered)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	out.normalize()
	return &out, nil
}

func (d *usageBillingBalanceDeduction) normalize() {
	d.teamPart = service.QuantizeUsageBillingAmount(d.teamPart)
	d.personalPart = service.QuantizeUsageBillingAmount(d.personalPart)
	d.uncovered = service.QuantizeUsageBillingAmount(d.uncovered)
	if d.uncovered < 0 {
		d.uncovered = 0
	}
	d.sufficient = d.uncovered <= 0
}
