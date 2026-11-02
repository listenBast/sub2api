package service

// 团队管理员（sub2api 二开，fork-only）。
//
// 主账号可以把已加入的成员设为团队管理员。管理员拥有除“转让主账号”之外与主账号一致的团队管理权限
// （邀请/移除成员、划拨额度、审批退出、修改备注与限额、改名、解散、查看用量与流水、成员花费统计）。
//
// 权限不进入任何会话、令牌或 API Key 认证快照：每次请求都经 ResolveManager 从数据库读取当前
// 身份（主账号 / 活跃管理员 / 其他），所以升降级后下一次请求立即生效；站点管理员也可通过
// AdminSetMemberAdmin 设置或取消。转让主账号（TransferOwnership）仍只接受 ownerID，不经过本文件的解析。

import (
	"context"
	"fmt"
	"sort"
	"time"

	"database/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbteam "github.com/Wei-Shaw/sub2api/ent/team"
	dbmembership "github.com/Wei-Shaw/sub2api/ent/teammembership"
	dbusagelog "github.com/Wei-Shaw/sub2api/ent/usagelog"
	dbuser "github.com/Wei-Shaw/sub2api/ent/user"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	// TeamRoleAdmin 已激活的团队管理员。
	TeamRoleAdmin = "admin"

	TeamActionAdminGranted     = "member_admin_granted"
	TeamActionAdminRevoked     = "member_admin_revoked"
	TeamActionSiteAdminGranted = "admin_member_admin_granted"
	TeamActionSiteAdminRevoked = "admin_member_admin_revoked"
)

var (
	// ErrTeamManagerRequired 当前账号是团队成员，但不是主账号或管理员。
	ErrTeamManagerRequired = infraerrors.Forbidden("TEAM_MANAGER_REQUIRED", "仅团队主账号或管理员可以执行此操作")
	// ErrTeamAdminTarget 只有已加入（active）的成员可以被设为管理员。
	ErrTeamAdminTarget = infraerrors.BadRequest("TEAM_ADMIN_TARGET_INVALID", "只能把已加入团队的成员设为管理员")
)

// IsTeamManagerRole 主账号和团队管理员都属于“团队管理者”。
func IsTeamManagerRole(role string) bool {
	return role == TeamRoleOwner || role == TeamRoleAdmin
}

type teamOperatorKey struct{}

// WithTeamOperator 标记真实操作人。管理员代主账号操作时，流水里记录管理员本人，而不是主账号。
func WithTeamOperator(ctx context.Context, userID int64) context.Context {
	if userID <= 0 {
		return ctx
	}
	return context.WithValue(ctx, teamOperatorKey{}, userID)
}

func teamOperatorID(ctx context.Context, fallback int64) int64 {
	if id, ok := ctx.Value(teamOperatorKey{}).(int64); ok && id > 0 {
		return id
	}
	return fallback
}

// TeamManager 描述一次请求中解析出的团队管理者身份。
type TeamManager struct {
	TeamID  int64
	OwnerID int64
	UserID  int64
	// IsOwner 为 true 表示当前用户是主账号；false 表示团队管理员。
	IsOwner bool
}

// ResolveManager 每次从数据库解析 userID 是否为团队管理者（主账号，或已激活的管理员）。
// 调用方把 OwnerID 交给既有的“主账号”服务方法，并用 WithTeamOperator 记录真实操作人。
func (s *TeamService) ResolveManager(ctx context.Context, userID int64) (*TeamManager, error) {
	owned, err := s.client.Team.Query().Where(dbteam.OwnerIDEQ(userID)).Only(ctx)
	if err == nil {
		return &TeamManager{TeamID: owned.ID, OwnerID: userID, UserID: userID, IsOwner: true}, nil
	}
	if !dbent.IsNotFound(err) {
		return nil, err
	}
	membership, err := s.client.TeamMembership.Query().Where(dbmembership.UserIDEQ(userID)).Only(ctx)
	if dbent.IsNotFound(err) {
		return nil, ErrTeamNotFound
	}
	if err != nil {
		return nil, err
	}
	if !membership.IsAdmin || membership.Status != TeamMembershipActive {
		return nil, ErrTeamManagerRequired
	}
	teamEntity, err := s.client.Team.Get(ctx, membership.TeamID)
	if err != nil {
		return nil, translateTeamError(err)
	}
	return &TeamManager{TeamID: teamEntity.ID, OwnerID: teamEntity.OwnerID, UserID: userID}, nil
}

// SetMemberAdmin 主账号设置/取消团队管理员。只有主账号能调用（管理员不能任命或撤销管理员）。
func (s *TeamService) SetMemberAdmin(ctx context.Context, ownerID, memberID int64, isAdmin bool) (*TeamMemberView, error) {
	return s.setMemberAdmin(ctx, ownerID, memberID, isAdmin, ownerID, false)
}

// AdminSetMemberAdmin 站点管理员为任意团队设置/取消团队管理员。
func (s *TeamService) AdminSetMemberAdmin(ctx context.Context, adminID, teamID, memberID int64, isAdmin bool) (*TeamMemberView, error) {
	return s.setMemberAdmin(ctx, teamID, memberID, isAdmin, adminID, true)
}

func (s *TeamService) setMemberAdmin(ctx context.Context, ownerOrTeamID, memberID int64, isAdmin bool, operatorID int64, siteAdmin bool) (*TeamMemberView, error) {
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin team admin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	teamQuery := client.Team.Query()
	if siteAdmin {
		teamQuery = teamQuery.Where(dbteam.IDEQ(ownerOrTeamID))
	} else {
		teamQuery = teamQuery.Where(dbteam.OwnerIDEQ(ownerOrTeamID))
	}
	teamEntity, err := s.lockTeamQuery(teamQuery).Only(txCtx)
	if err != nil {
		return nil, translateTeamError(err)
	}
	if !siteAdmin && teamEntity.Status != TeamStatusActive {
		return nil, ErrTeamSuspended
	}
	if memberID == teamEntity.OwnerID {
		return nil, ErrTeamAdminTarget
	}
	membership, err := s.lockMembershipQuery(client.TeamMembership.Query().Where(
		dbmembership.TeamIDEQ(teamEntity.ID),
		dbmembership.UserIDEQ(memberID),
		dbmembership.StatusIn(TeamMembershipActive, TeamMembershipExitPending),
	)).Only(txCtx)
	if err != nil {
		return nil, translateTeamMemberError(err)
	}
	if isAdmin && membership.Status != TeamMembershipActive {
		return nil, ErrTeamAdminTarget
	}
	user, err := client.User.Get(txCtx, memberID)
	if err != nil {
		return nil, translateTeamUserError(err)
	}
	if membership.IsAdmin == isAdmin {
		view := teamMemberView(membership, user)
		return &view, nil
	}
	updated, err := client.TeamMembership.UpdateOneID(membership.ID).SetIsAdmin(isAdmin).Save(txCtx)
	if err != nil {
		return nil, fmt.Errorf("update team member admin flag: %w", err)
	}
	owner, err := client.User.Get(txCtx, teamEntity.OwnerID)
	if err != nil {
		return nil, translateTeamUserError(err)
	}
	action := TeamActionAdminGranted
	switch {
	case siteAdmin && isAdmin:
		action = TeamActionSiteAdminGranted
	case siteAdmin:
		action = TeamActionSiteAdminRevoked
	case !isAdmin:
		action = TeamActionAdminRevoked
	}
	if err := createTeamTransaction(txCtx, client, teamEntity.ID, operatorID, &memberID, action, 0, owner.Balance, owner.Balance, nil, nil, user.Email); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit team admin change: %w", err)
	}
	// 团队角色不在认证快照里，这里仍失效该成员的缓存，保证任何派生缓存都不会滞后。
	s.invalidateBalanceCaches(ctx, memberID)
	view := teamMemberView(updated, user)
	return &view, nil
}

// AdminOwnerID 返回团队主账号用户 ID（站点管理员为主账号加减余额时使用）。
func (s *TeamService) AdminOwnerID(ctx context.Context, teamID int64) (int64, error) {
	teamEntity, err := s.client.Team.Get(ctx, teamID)
	if err != nil {
		return 0, translateTeamError(err)
	}
	return teamEntity.OwnerID, nil
}

// TeamMemberSpend 一个成员在统计区间内的花费。
type TeamMemberSpend struct {
	UserID   int64  `json:"user_id"`
	Email    string `json:"email"`
	Username string `json:"username"`
	Remark   string `json:"remark,omitempty"`
	// Role 为 owner / admin / member。
	Role   string `json:"role"`
	Status string `json:"status"`
	// Requests 请求次数；Tokens 总 token（输入+输出+缓存）。
	Requests int64 `json:"requests"`
	Tokens   int64 `json:"tokens"`
	// TotalCost 标准计价费用；ActualCost 实际扣费（含倍率）。
	TotalCost  float64 `json:"total_cost"`
	ActualCost float64 `json:"actual_cost"`
}

// TeamMemberSpendStats 团队成员花费统计。
type TeamMemberSpendStats struct {
	TeamID  int64             `json:"team_id"`
	Start   *time.Time        `json:"start,omitempty"`
	End     *time.Time        `json:"end,omitempty"`
	Members []TeamMemberSpend `json:"members"`
	Totals  TeamMemberSpend   `json:"totals"`
}

// GetMemberSpendStats 主账号/管理员查看自己团队各成员在 [Start, End) 内的花费。
// ownerID 必须是已经由 ResolveManager 解析出的主账号 ID。
func (s *TeamService) GetMemberSpendStats(ctx context.Context, ownerID int64, start, end *time.Time) (*TeamMemberSpendStats, error) {
	teamEntity, err := s.requireOwnedTeam(ctx, ownerID, false)
	if err != nil {
		return nil, err
	}
	return s.memberSpendStats(ctx, teamEntity, start, end)
}

// GetAdminMemberSpendStats 站点管理员查看任意团队各成员的花费。
func (s *TeamService) GetAdminMemberSpendStats(ctx context.Context, teamID int64, start, end *time.Time) (*TeamMemberSpendStats, error) {
	teamEntity, err := s.client.Team.Get(ctx, teamID)
	if err != nil {
		return nil, translateTeamError(err)
	}
	return s.memberSpendStats(ctx, teamEntity, start, end)
}

func (s *TeamService) memberSpendStats(ctx context.Context, teamEntity *dbent.Team, start, end *time.Time) (*TeamMemberSpendStats, error) {
	if start != nil && end != nil && !start.Before(*end) {
		return nil, infraBadRequest("TEAM_RANGE_INVALID", "开始时间必须早于结束时间")
	}
	memberships, err := s.client.TeamMembership.Query().Where(
		dbmembership.TeamIDEQ(teamEntity.ID),
		dbmembership.StatusIn(TeamMembershipActive, TeamMembershipExitPending),
	).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list team memberships for stats: %w", err)
	}
	rows := make(map[int64]*TeamMemberSpend, len(memberships)+1)
	order := make([]int64, 0, len(memberships)+1)
	userIDs := make([]int64, 0, len(memberships)+1)
	rows[teamEntity.OwnerID] = &TeamMemberSpend{UserID: teamEntity.OwnerID, Role: TeamRoleOwner, Status: TeamMembershipActive}
	order = append(order, teamEntity.OwnerID)
	userIDs = append(userIDs, teamEntity.OwnerID)
	for _, membership := range memberships {
		role := TeamRoleMember
		if membership.IsAdmin {
			role = TeamRoleAdmin
		}
		rows[membership.UserID] = &TeamMemberSpend{UserID: membership.UserID, Role: role, Status: membership.Status, Remark: membership.Remark}
		order = append(order, membership.UserID)
		userIDs = append(userIDs, membership.UserID)
	}
	users, err := s.client.User.Query().Where(dbuser.IDIn(userIDs...)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load team users for stats: %w", err)
	}
	for _, user := range users {
		if row := rows[user.ID]; row != nil {
			row.Email = user.Email
			row.Username = user.Username
		}
	}

	query := s.client.UsageLog.Query().Where(dbusagelog.UserIDIn(userIDs...))
	if start != nil {
		query = query.Where(dbusagelog.CreatedAtGTE(*start))
	}
	if end != nil {
		query = query.Where(dbusagelog.CreatedAtLT(*end))
	}
	var grouped []struct {
		UserID     int64           `json:"user_id"`
		Requests   int64           `json:"requests"`
		Input      sql.NullFloat64 `json:"input_tokens"`
		Output     sql.NullFloat64 `json:"output_tokens"`
		CacheWrite sql.NullFloat64 `json:"cache_creation_tokens"`
		CacheRead  sql.NullFloat64 `json:"cache_read_tokens"`
		TotalCost  sql.NullFloat64 `json:"total_cost"`
		ActualCost sql.NullFloat64 `json:"actual_cost"`
	}
	if err := query.GroupBy(dbusagelog.FieldUserID).Aggregate(
		dbent.As(dbent.Count(), "requests"),
		dbent.As(dbent.Sum(dbusagelog.FieldInputTokens), "input_tokens"),
		dbent.As(dbent.Sum(dbusagelog.FieldOutputTokens), "output_tokens"),
		dbent.As(dbent.Sum(dbusagelog.FieldCacheCreationTokens), "cache_creation_tokens"),
		dbent.As(dbent.Sum(dbusagelog.FieldCacheReadTokens), "cache_read_tokens"),
		dbent.As(dbent.Sum(dbusagelog.FieldTotalCost), "total_cost"),
		dbent.As(dbent.Sum(dbusagelog.FieldActualCost), "actual_cost"),
	).Scan(ctx, &grouped); err != nil {
		return nil, fmt.Errorf("aggregate team member spend: %w", err)
	}
	for _, g := range grouped {
		row := rows[g.UserID]
		if row == nil {
			continue
		}
		row.Requests = g.Requests
		row.Tokens = int64(g.Input.Float64 + g.Output.Float64 + g.CacheWrite.Float64 + g.CacheRead.Float64)
		row.TotalCost = roundTeamAmount(g.TotalCost.Float64)
		row.ActualCost = roundTeamAmount(g.ActualCost.Float64)
	}

	stats := &TeamMemberSpendStats{TeamID: teamEntity.ID, Start: start, End: end, Members: make([]TeamMemberSpend, 0, len(order))}
	for _, id := range order {
		row := *rows[id]
		stats.Members = append(stats.Members, row)
		stats.Totals.Requests += row.Requests
		stats.Totals.Tokens += row.Tokens
		stats.Totals.TotalCost += row.TotalCost
		stats.Totals.ActualCost += row.ActualCost
	}
	stats.Totals.TotalCost = roundTeamAmount(stats.Totals.TotalCost)
	stats.Totals.ActualCost = roundTeamAmount(stats.Totals.ActualCost)
	sort.SliceStable(stats.Members, func(i, j int) bool {
		if stats.Members[i].ActualCost != stats.Members[j].ActualCost {
			return stats.Members[i].ActualCost > stats.Members[j].ActualCost
		}
		return stats.Members[i].UserID < stats.Members[j].UserID
	})
	return stats, nil
}
