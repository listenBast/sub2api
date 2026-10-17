package service

import (
	"context"
	"fmt"
	"math"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbteam "github.com/Wei-Shaw/sub2api/ent/team"
	dbmembership "github.com/Wei-Shaw/sub2api/ent/teammembership"
)

// 管理员调整成员团队额度（fork）。
//
// 操作方式与管理员调整个人余额一致：set（设为）/ add（增加）/ subtract（扣减）。
// 资金来源 source：
//   - owner_pool（默认）：与主账号划拨完全一致，增加的额度从主账号余额（团队资金池）扣出，
//     扣减的额度归还主账号余额，团队资金总额（主账号余额 + 成员团队额度）保持不变。
//   - system：管理员直接增发 / 扣除团队额度，不动主账号余额，语义同管理员直接调整个人余额。
//
// 成员团队额度调整后不能为负；set 可用于把透支（负数）的团队额度直接校正为 0 或指定值。
// 每次调整都写入团队资金流水（team_transactions），并失效相关用户的认证快照与余额缓存。
const (
	TeamAdminBalanceSet      = "set"
	TeamAdminBalanceAdd      = "add"
	TeamAdminBalanceSubtract = "subtract"

	TeamAdminBalanceSourceOwnerPool = "owner_pool"
	TeamAdminBalanceSourceSystem    = "system"

	TeamActionAdminBalanceAllocated = "admin_balance_allocated"
	TeamActionAdminBalanceRecovered = "admin_balance_recovered"
	TeamActionAdminBalanceAdjusted  = "admin_balance_adjusted"
)

// TeamAdminBalanceAdjustment 管理员调整成员团队额度的请求参数。
type TeamAdminBalanceAdjustment struct {
	Operation string
	Amount    float64
	Source    string
	Note      string
}

// AdminAdjustMemberTeamBalance 管理员设置 / 增加 / 扣减指定团队成员的团队额度。
func (s *TeamService) AdminAdjustMemberTeamBalance(ctx context.Context, adminID, teamID, memberID int64, req TeamAdminBalanceAdjustment) (*TeamMemberView, error) {
	operation := strings.TrimSpace(req.Operation)
	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = TeamAdminBalanceSourceOwnerPool
	}
	if source != TeamAdminBalanceSourceOwnerPool && source != TeamAdminBalanceSourceSystem {
		return nil, infraBadRequest("TEAM_BALANCE_SOURCE_INVALID", "额度来源无效")
	}
	amount := req.Amount
	if math.IsNaN(amount) || math.IsInf(amount, 0) {
		return nil, infraBadRequest("TEAM_AMOUNT_INVALID", "额度调整值无效")
	}
	amount = roundTeamAmount(amount)
	switch operation {
	case TeamAdminBalanceSet:
		if amount < 0 {
			return nil, infraBadRequest("TEAM_AMOUNT_INVALID", "团队额度不能设为负数")
		}
	case TeamAdminBalanceAdd, TeamAdminBalanceSubtract:
		if amount <= 0 {
			return nil, infraBadRequest("TEAM_AMOUNT_INVALID", "额度调整值必须大于 0")
		}
	default:
		return nil, infraBadRequest("TEAM_BALANCE_OPERATION_INVALID", "不支持的额度调整方式")
	}
	note := strings.TrimSpace(req.Note)
	if len([]rune(note)) > 500 {
		return nil, infraBadRequest("TEAM_NOTE_INVALID", "备注不能超过 500 个字符")
	}

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin admin team balance transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	// 与管理员移除成员 / 更换主账号一致：管理员操作不受团队冻结状态限制。
	teamEntity, err := s.lockTeamQuery(client.Team.Query().Where(dbteam.IDEQ(teamID))).Only(txCtx)
	if err != nil {
		return nil, translateTeamError(err)
	}
	membership, err := s.lockMembershipQuery(client.TeamMembership.Query().Where(
		dbmembership.TeamIDEQ(teamEntity.ID),
		dbmembership.UserIDEQ(memberID),
		dbmembership.StatusIn(TeamMembershipActive, TeamMembershipExitPending),
	)).Only(txCtx)
	if err != nil {
		return nil, translateTeamMemberError(err)
	}
	owner, member, err := s.lockTeamUsers(txCtx, client, teamEntity.OwnerID, memberID)
	if err != nil {
		return nil, err
	}

	memberBefore := member.TeamBalance
	var delta float64
	switch operation {
	case TeamAdminBalanceSet:
		delta = roundTeamAmount(amount - memberBefore)
	case TeamAdminBalanceAdd:
		delta = amount
	case TeamAdminBalanceSubtract:
		delta = -amount
	}
	if delta == 0 {
		view := teamMemberView(membership, member)
		return &view, nil
	}
	memberAfter := roundTeamAmount(memberBefore + delta)
	if memberAfter < -teamBalanceEpsilon {
		return nil, ErrTeamMemberInsufficientBalance
	}
	if memberAfter < 0 {
		memberAfter = 0
	}

	ownerBefore := owner.Balance
	ownerAfter := ownerBefore
	action := TeamActionAdminBalanceAdjusted
	if source == TeamAdminBalanceSourceOwnerPool {
		// 只有从资金池划出（delta > 0）时校验主账号余额；收回总是归还资金池。
		if delta > 0 && ownerBefore+teamBalanceEpsilon < delta {
			return nil, ErrTeamInsufficientBalance
		}
		ownerAfter = roundTeamAmount(ownerBefore - delta)
		if _, err := client.User.UpdateOneID(owner.ID).SetBalance(ownerAfter).Save(txCtx); err != nil {
			return nil, fmt.Errorf("update team owner balance: %w", err)
		}
		action = TeamActionAdminBalanceAllocated
		if delta < 0 {
			action = TeamActionAdminBalanceRecovered
		}
	}
	if _, err := client.User.UpdateOneID(member.ID).SetTeamBalance(memberAfter).Save(txCtx); err != nil {
		return nil, fmt.Errorf("update member team balance: %w", err)
	}
	if err := createTeamTransaction(txCtx, client, teamEntity.ID, adminID, &member.ID, action, delta, ownerBefore, ownerAfter, &memberBefore, &memberAfter, note); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit admin team balance adjustment: %w", err)
	}
	if source == TeamAdminBalanceSourceOwnerPool {
		s.invalidateBalanceCaches(ctx, owner.ID, member.ID)
	} else {
		s.invalidateBalanceCaches(ctx, member.ID)
	}
	member.TeamBalance = memberAfter
	view := teamMemberView(membership, member)
	return &view, nil
}
