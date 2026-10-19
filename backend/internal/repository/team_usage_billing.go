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
// 返回值与上游 deductUsageBillingBalance 相同（扣后个人余额、是否未透支），
// 并把个人 / 团队拆分结果写进 result。
func deductTeamAwareUsageBillingBalance(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand, result *service.UsageBillingApplyResult) (float64, bool, error) {
	deduction, err := deductTeamUsageBillingBalance(ctx, tx, cmd.UserID, cmd.BalanceCost, cmd.BalanceMode)
	if err != nil {
		return 0, false, err
	}
	newTeamBalance := deduction.newTeamBalance
	result.NewTeamBalance = &newTeamBalance
	result.PersonalBalanceCost = deduction.personalPart
	result.TeamBalanceCost = deduction.teamPart
	return deduction.newBalance, deduction.sufficient, nil
}

// usageBillingBalanceDeduction 描述一次余额扣费在个人余额与团队额度之间的拆分结果。
type usageBillingBalanceDeduction struct {
	newBalance     float64
	newTeamBalance float64
	personalPart   float64
	teamPart       float64
	// sufficient 为 false 表示被扣的资金池在扣费后出现了透支（与上游单资金池语义一致）。
	sufficient bool
}

// deductTeamUsageBillingBalance 按 API Key 的扣费方式在同一条原子 UPDATE 中
// 拆分扣减个人余额（users.balance）与团队额度（users.team_balance）：
//
//	team_first      团队额度优先，剩余部分扣个人余额
//	personal_first  个人余额优先，剩余部分扣团队额度
//	team_only       全部扣团队额度
//	personal_only   全部扣个人余额
//
// 非团队成员的所有模式都退化为只扣个人余额，避免 team_only 在独立账号上写出负的
// team_balance。资金池不足时仍然记账（允许透支），由 sufficient=false 标记。
func deductTeamUsageBillingBalance(ctx context.Context, tx *sql.Tx, userID int64, amount float64, mode string) (*usageBillingBalanceDeduction, error) {
	mode = service.NormalizeBalanceMode(mode)
	var out usageBillingBalanceDeduction
	err := tx.QueryRowContext(ctx, `
		WITH current_user_row AS (
			SELECT u.id, u.balance, COALESCE(u.team_balance, 0) AS team_balance,
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
					WHEN NOT is_team_member THEN 0::numeric
					WHEN $3::text = 'personal_only' THEN 0::numeric
					WHEN $3::text = 'team_only' THEN $1::numeric
					WHEN $3::text = 'personal_first' THEN $1::numeric - LEAST(GREATEST(balance, 0), $1::numeric)
					ELSE LEAST(GREATEST(team_balance, 0), $1::numeric)
				END AS team_part
			FROM current_user_row
		)
		, updated AS (
			UPDATE users u
			SET balance = u.balance - ($1::numeric - s.team_part),
				team_balance = u.team_balance - s.team_part,
				updated_at = NOW()
			FROM split s
			WHERE u.id = s.id
			RETURNING u.id, u.balance, u.team_balance
		)
		SELECT updated.balance, updated.team_balance, split.team_part
		FROM updated
		JOIN split ON split.id = updated.id
	`, amount, userID, mode).Scan(&out.newBalance, &out.newTeamBalance, &out.teamPart)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	out.teamPart = service.QuantizeUsageBillingAmount(out.teamPart)
	out.personalPart = service.QuantizeUsageBillingAmount(amount - out.teamPart)
	out.sufficient = (out.personalPart <= 0 || out.newBalance >= 0) && (out.teamPart <= 0 || out.newTeamBalance >= 0)
	return &out, nil
}
