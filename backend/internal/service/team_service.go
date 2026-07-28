package service

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbpaymentorder "github.com/Wei-Shaw/sub2api/ent/paymentorder"
	dbteam "github.com/Wei-Shaw/sub2api/ent/team"
	dbmembership "github.com/Wei-Shaw/sub2api/ent/teammembership"
	dbuser "github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const teamBalanceEpsilon = 0.00000001

// TeamService 负责团队成员生命周期和资金一致性。
type TeamService struct {
	client               *dbent.Client
	sqlDB                *sql.DB
	authCacheInvalidator APIKeyAuthCacheInvalidator
	rowLocks             bool
}

func NewTeamService(client *dbent.Client, sqlDB *sql.DB, authCacheInvalidator APIKeyAuthCacheInvalidator) *TeamService {
	rowLocks := true
	if sqlDB != nil && strings.Contains(strings.ToLower(fmt.Sprintf("%T", sqlDB.Driver())), "sqlite") {
		rowLocks = false
	}
	return &TeamService{client: client, sqlDB: sqlDB, authCacheInvalidator: authCacheInvalidator, rowLocks: rowLocks}
}

func (s *TeamService) Upgrade(ctx context.Context, userID int64, name string) (*TeamContext, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "我的团队"
	}
	if len([]rune(name)) > 100 {
		return nil, infraBadRequest("TEAM_NAME_INVALID", "团队名称不能超过 100 个字符")
	}

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin team upgrade transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	user, err := s.lockUserQuery(client.User.Query().Where(dbuser.IDEQ(userID))).Only(txCtx)
	if err != nil {
		return nil, translateTeamUserError(err)
	}
	if user.Status != domain.StatusActive || user.Role == domain.RoleAdmin {
		return nil, infraBadRequest("TEAM_OWNER_INELIGIBLE", "只有状态正常的普通用户可以开启团队模式")
	}
	owned, err := client.Team.Query().Where(dbteam.OwnerIDEQ(userID)).Exist(txCtx)
	if err != nil {
		return nil, err
	}
	member, err := client.TeamMembership.Query().Where(dbmembership.UserIDEQ(userID)).Exist(txCtx)
	if err != nil {
		return nil, err
	}
	if owned || member {
		return nil, ErrTeamAlreadyExists
	}

	teamEntity, err := client.Team.Create().SetName(name).SetOwnerID(userID).SetStatus(TeamStatusActive).Save(txCtx)
	if err != nil {
		return nil, fmt.Errorf("create team: %w", err)
	}
	if _, err := client.TeamTransaction.Create().
		SetTeamID(teamEntity.ID).
		SetOperatorID(userID).
		SetAction(TeamActionCreated).
		SetOwnerBalanceBefore(user.Balance).
		SetOwnerBalanceAfter(user.Balance).
		Save(txCtx); err != nil {
		return nil, fmt.Errorf("record team creation: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit team upgrade: %w", err)
	}
	return s.GetContext(ctx, userID)
}

func (s *TeamService) Rename(ctx context.Context, ownerID int64, name string) (*TeamContext, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 100 {
		return nil, infraBadRequest("TEAM_NAME_INVALID", "请输入团队名称，且不能超过 100 个字符")
	}
	teamEntity, err := s.requireOwnedTeam(ctx, ownerID, true)
	if err != nil {
		return nil, err
	}
	if _, err := s.client.Team.UpdateOneID(teamEntity.ID).SetName(name).Save(ctx); err != nil {
		return nil, fmt.Errorf("rename team: %w", err)
	}
	owner, err := s.client.User.Get(ctx, ownerID)
	if err != nil {
		return nil, translateTeamUserError(err)
	}
	_, _ = s.client.TeamTransaction.Create().
		SetTeamID(teamEntity.ID).
		SetOperatorID(ownerID).
		SetAction(TeamActionRenamed).
		SetOwnerBalanceBefore(owner.Balance).
		SetOwnerBalanceAfter(owner.Balance).
		SetNote(name).
		Save(ctx)
	return s.GetContext(ctx, ownerID)
}

func (s *TeamService) Invite(ctx context.Context, ownerID int64, email string) (*TeamMemberView, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return nil, infraBadRequest("TEAM_INVITEE_REQUIRED", "请输入成员邮箱")
	}

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin team invite transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	teamEntity, err := s.lockTeamQuery(client.Team.Query().Where(dbteam.OwnerIDEQ(ownerID))).Only(txCtx)
	if err != nil {
		return nil, translateTeamError(err)
	}
	if teamEntity.Status != TeamStatusActive {
		return nil, ErrTeamSuspended
	}
	count, err := client.TeamMembership.Query().Where(dbmembership.TeamIDEQ(teamEntity.ID)).Count(txCtx)
	if err != nil {
		return nil, err
	}
	if count >= MaxTeamMembers {
		return nil, ErrTeamMemberLimit
	}

	target, err := s.lockUserQuery(client.User.Query().Where(dbuser.EmailEqualFold(email))).Only(txCtx)
	if err != nil {
		return nil, translateTeamUserError(err)
	}
	if target.ID == ownerID || target.Status != domain.StatusActive || target.Role == domain.RoleAdmin {
		return nil, infraBadRequest("TEAM_INVITEE_INELIGIBLE", "该用户不符合加入团队的条件")
	}
	owned, err := client.Team.Query().Where(dbteam.OwnerIDEQ(target.ID)).Exist(txCtx)
	if err != nil {
		return nil, err
	}
	member, err := client.TeamMembership.Query().Where(dbmembership.UserIDEQ(target.ID)).Exist(txCtx)
	if err != nil {
		return nil, err
	}
	if owned || member {
		return nil, ErrTeamMemberExists
	}

	membership, err := client.TeamMembership.Create().
		SetTeamID(teamEntity.ID).
		SetUserID(target.ID).
		SetInvitedBy(ownerID).
		SetStatus(TeamMembershipInvited).
		Save(txCtx)
	if err != nil {
		return nil, fmt.Errorf("create team invitation: %w", err)
	}
	owner, err := client.User.Get(txCtx, ownerID)
	if err != nil {
		return nil, translateTeamUserError(err)
	}
	if err := createTeamTransaction(txCtx, client, teamEntity.ID, ownerID, &target.ID, TeamActionInvited, 0, owner.Balance, owner.Balance, &target.Balance, &target.Balance, ""); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit team invitation: %w", err)
	}
	view := teamMemberView(membership, target)
	return &view, nil
}

func (s *TeamService) RespondInvitation(ctx context.Context, userID int64, accept bool) (*TeamContext, error) {
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin invitation response transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	membershipQuery := client.TeamMembership.Query().
		Where(dbmembership.UserIDEQ(userID), dbmembership.StatusEQ(TeamMembershipInvited))
	membership, err := s.lockMembershipQuery(membershipQuery).Only(txCtx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, ErrTeamInviteNotFound
		}
		return nil, err
	}
	teamEntity, err := s.lockTeamQuery(client.Team.Query().Where(dbteam.IDEQ(membership.TeamID))).Only(txCtx)
	if err != nil {
		return nil, translateTeamError(err)
	}
	owner, member, err := s.lockTeamUsers(txCtx, client, teamEntity.OwnerID, userID)
	if err != nil {
		return nil, err
	}

	action := TeamActionInviteRejected
	amount := 0.0
	ownerBefore := owner.Balance
	ownerAfter := owner.Balance
	memberBefore := member.Balance
	memberAfter := member.Balance
	if accept {
		if teamEntity.Status != TeamStatusActive {
			return nil, ErrTeamSuspended
		}
		if math.Abs(member.FrozenBalance) >= teamBalanceEpsilon {
			return nil, ErrTeamFrozenBalancePending
		}
		pendingPayment, paymentErr := hasPendingTeamPayment(txCtx, client, userID)
		if paymentErr != nil {
			return nil, paymentErr
		}
		if pendingPayment {
			return nil, ErrTeamPendingPayments
		}
		amount = roundTeamAmount(member.Balance)
		ownerAfter = roundTeamAmount(owner.Balance + amount)
		memberAfter = 0
		if _, err := client.User.UpdateOneID(owner.ID).SetBalance(ownerAfter).Save(txCtx); err != nil {
			return nil, fmt.Errorf("merge member balance into team owner: %w", err)
		}
		if _, err := client.User.UpdateOneID(member.ID).SetBalance(memberAfter).Save(txCtx); err != nil {
			return nil, fmt.Errorf("clear joining member balance: %w", err)
		}
		now := time.Now()
		if _, err := client.TeamMembership.UpdateOneID(membership.ID).
			SetStatus(TeamMembershipActive).
			SetJoinedAt(now).
			ClearExitRequestedAt().
			Save(txCtx); err != nil {
			return nil, fmt.Errorf("accept team invitation: %w", err)
		}
		action = TeamActionInviteAccepted
	} else if err := client.TeamMembership.DeleteOneID(membership.ID).Exec(txCtx); err != nil {
		return nil, fmt.Errorf("reject team invitation: %w", err)
	}
	if err := createTeamTransaction(txCtx, client, teamEntity.ID, userID, &userID, action, amount, ownerBefore, ownerAfter, &memberBefore, &memberAfter, ""); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit invitation response: %w", err)
	}
	if accept {
		s.invalidateBalanceCaches(ctx, owner.ID, member.ID)
	}
	return s.GetContext(ctx, userID)
}

func (s *TeamService) AllocateBalance(ctx context.Context, ownerID, memberID int64, amount float64, note string) (*TeamMemberView, error) {
	amount = roundTeamAmount(amount)
	if amount == 0 || math.IsNaN(amount) || math.IsInf(amount, 0) {
		return nil, infraBadRequest("TEAM_AMOUNT_INVALID", "额度调整值不能为 0")
	}
	note = strings.TrimSpace(note)
	if len([]rune(note)) > 500 {
		return nil, infraBadRequest("TEAM_NOTE_INVALID", "备注不能超过 500 个字符")
	}

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin team allocation transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	teamEntity, err := s.lockTeamQuery(client.Team.Query().Where(dbteam.OwnerIDEQ(ownerID))).Only(txCtx)
	if err != nil {
		return nil, translateTeamError(err)
	}
	if teamEntity.Status != TeamStatusActive {
		return nil, ErrTeamSuspended
	}
	membership, err := s.lockMembershipQuery(client.TeamMembership.Query().Where(
		dbmembership.TeamIDEQ(teamEntity.ID),
		dbmembership.UserIDEQ(memberID),
		dbmembership.StatusIn(TeamMembershipActive, TeamMembershipExitPending),
	)).Only(txCtx)
	if err != nil {
		return nil, translateTeamMemberError(err)
	}
	owner, member, err := s.lockTeamUsers(txCtx, client, ownerID, memberID)
	if err != nil {
		return nil, err
	}
	if amount > 0 && owner.Balance+teamBalanceEpsilon < amount {
		return nil, ErrTeamInsufficientBalance
	}
	if amount < 0 && member.Balance+teamBalanceEpsilon < -amount {
		return nil, ErrTeamMemberInsufficientBalance
	}
	ownerAfter := roundTeamAmount(owner.Balance - amount)
	memberAfter := roundTeamAmount(member.Balance + amount)
	if _, err := client.User.UpdateOneID(ownerID).SetBalance(ownerAfter).Save(txCtx); err != nil {
		return nil, fmt.Errorf("debit team owner: %w", err)
	}
	if _, err := client.User.UpdateOneID(memberID).SetBalance(memberAfter).Save(txCtx); err != nil {
		return nil, fmt.Errorf("credit team member: %w", err)
	}
	action := TeamActionBalanceAdded
	if amount < 0 {
		action = TeamActionBalanceRecovered
	}
	if err := createTeamTransaction(txCtx, client, teamEntity.ID, ownerID, &memberID, action, amount, owner.Balance, ownerAfter, &member.Balance, &memberAfter, note); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit team allocation: %w", err)
	}
	s.invalidateBalanceCaches(ctx, ownerID, memberID)
	member.Balance = memberAfter
	view := teamMemberView(membership, member)
	return &view, nil
}

func (s *TeamService) RequestExit(ctx context.Context, userID int64) (*TeamContext, error) {
	membership, err := s.client.TeamMembership.Query().
		Where(dbmembership.UserIDEQ(userID)).
		WithTeam().
		Only(ctx)
	if err != nil {
		return nil, translateTeamMemberError(err)
	}
	if membership.Status == TeamMembershipExitPending {
		return nil, ErrTeamExitAlreadyPending
	}
	if membership.Status != TeamMembershipActive {
		return nil, ErrTeamMemberNotFound
	}
	now := time.Now()
	if _, err := s.client.TeamMembership.UpdateOneID(membership.ID).
		SetStatus(TeamMembershipExitPending).
		SetExitRequestedAt(now).
		Save(ctx); err != nil {
		return nil, fmt.Errorf("request team exit: %w", err)
	}
	owner, err := s.client.User.Get(ctx, membership.Edges.Team.OwnerID)
	if err == nil {
		_, _ = s.client.TeamTransaction.Create().
			SetTeamID(membership.TeamID).
			SetOperatorID(userID).
			SetMemberID(userID).
			SetAction(TeamActionExitRequested).
			SetOwnerBalanceBefore(owner.Balance).
			SetOwnerBalanceAfter(owner.Balance).
			Save(ctx)
	}
	return s.GetContext(ctx, userID)
}

func (s *TeamService) ReviewExit(ctx context.Context, ownerID, memberID int64, approve bool) error {
	if approve {
		return s.reclaimAndRemove(ctx, ownerID, memberID, ownerID, TeamActionExitApproved, false)
	}
	teamEntity, err := s.requireOwnedTeam(ctx, ownerID, true)
	if err != nil {
		return err
	}
	membership, err := s.client.TeamMembership.Query().Where(
		dbmembership.TeamIDEQ(teamEntity.ID),
		dbmembership.UserIDEQ(memberID),
		dbmembership.StatusEQ(TeamMembershipExitPending),
	).Only(ctx)
	if err != nil {
		return translateTeamMemberError(err)
	}
	if _, err := s.client.TeamMembership.UpdateOneID(membership.ID).
		SetStatus(TeamMembershipActive).
		ClearExitRequestedAt().
		Save(ctx); err != nil {
		return fmt.Errorf("reject team exit: %w", err)
	}
	owner, _ := s.client.User.Get(ctx, ownerID)
	if owner != nil {
		_, _ = s.client.TeamTransaction.Create().SetTeamID(teamEntity.ID).SetOperatorID(ownerID).SetMemberID(memberID).
			SetAction(TeamActionExitRejected).SetOwnerBalanceBefore(owner.Balance).SetOwnerBalanceAfter(owner.Balance).Save(ctx)
	}
	return nil
}

func (s *TeamService) RemoveMember(ctx context.Context, ownerID, memberID int64) error {
	return s.reclaimAndRemove(ctx, ownerID, memberID, ownerID, TeamActionMemberRemoved, false)
}

func (s *TeamService) UpdateMemberRemark(ctx context.Context, ownerID, memberID int64, remark string) (*TeamMemberView, error) {
	return s.updateMemberRemark(ctx, ownerID, memberID, remark, false)
}

func (s *TeamService) AdminUpdateMemberRemark(ctx context.Context, teamID, memberID int64, remark string) (*TeamMemberView, error) {
	return s.updateMemberRemark(ctx, teamID, memberID, remark, true)
}

func (s *TeamService) updateMemberRemark(ctx context.Context, ownerOrTeamID, memberID int64, remark string, admin bool) (*TeamMemberView, error) {
	remark = strings.TrimSpace(remark)
	if len([]rune(remark)) > 100 {
		return nil, infraBadRequest("TEAM_REMARK_INVALID", "成员备注不能超过 100 个字符")
	}
	teamQuery := s.client.Team.Query()
	if admin {
		teamQuery = teamQuery.Where(dbteam.IDEQ(ownerOrTeamID))
	} else {
		teamQuery = teamQuery.Where(dbteam.OwnerIDEQ(ownerOrTeamID))
	}
	teamEntity, err := teamQuery.Only(ctx)
	if err != nil {
		return nil, translateTeamError(err)
	}
	if !admin && teamEntity.Status != TeamStatusActive {
		return nil, ErrTeamSuspended
	}
	membership, err := s.client.TeamMembership.Query().Where(
		dbmembership.TeamIDEQ(teamEntity.ID),
		dbmembership.UserIDEQ(memberID),
	).Only(ctx)
	if err != nil {
		return nil, translateTeamMemberError(err)
	}
	membership, err = s.client.TeamMembership.UpdateOneID(membership.ID).SetRemark(remark).Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("更新成员备注失败: %w", err)
	}
	user, err := s.client.User.Get(ctx, memberID)
	if err != nil {
		return nil, translateTeamUserError(err)
	}
	view := teamMemberView(membership, user)
	return &view, nil
}

func (s *TeamService) UpdateMemberLimits(ctx context.Context, ownerID, memberID int64, concurrency, rpmLimit *int) (*TeamMemberView, error) {
	if concurrency == nil && rpmLimit == nil {
		return nil, infraBadRequest("TEAM_LIMITS_REQUIRED", "请至少设置并发数或每分钟请求数")
	}
	if concurrency != nil && (*concurrency < 1 || *concurrency > 1000) {
		return nil, infraBadRequest("TEAM_CONCURRENCY_INVALID", "并发数必须在 1 到 1000 之间")
	}
	if rpmLimit != nil && (*rpmLimit < 0 || *rpmLimit > 1000000) {
		return nil, infraBadRequest("TEAM_RPM_LIMIT_INVALID", "每分钟请求数必须在 0 到 1000000 之间")
	}

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin team member limits transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	teamEntity, err := s.lockTeamQuery(client.Team.Query().Where(dbteam.OwnerIDEQ(ownerID))).Only(txCtx)
	if err != nil {
		return nil, translateTeamError(err)
	}
	if teamEntity.Status != TeamStatusActive {
		return nil, ErrTeamSuspended
	}
	membership, err := s.lockMembershipQuery(client.TeamMembership.Query().Where(
		dbmembership.TeamIDEQ(teamEntity.ID),
		dbmembership.UserIDEQ(memberID),
		dbmembership.StatusIn(TeamMembershipActive, TeamMembershipExitPending),
	)).Only(txCtx)
	if err != nil {
		return nil, translateTeamMemberError(err)
	}
	owner, member, err := s.lockTeamUsers(txCtx, client, ownerID, memberID)
	if err != nil {
		return nil, err
	}

	update := client.User.UpdateOneID(memberID)
	noteParts := make([]string, 0, 2)
	if concurrency != nil {
		update.SetConcurrency(*concurrency)
		noteParts = append(noteParts, fmt.Sprintf("concurrency: %d -> %d", member.Concurrency, *concurrency))
	}
	if rpmLimit != nil {
		update.SetRpmLimit(*rpmLimit)
		noteParts = append(noteParts, fmt.Sprintf("rpm_limit: %d -> %d", member.RpmLimit, *rpmLimit))
	}
	updated, err := update.Save(txCtx)
	if err != nil {
		return nil, fmt.Errorf("update team member limits: %w", err)
	}
	if err := createTeamTransaction(txCtx, client, teamEntity.ID, ownerID, &memberID, TeamActionMemberLimitsUpdated, 0, owner.Balance, owner.Balance, &member.Balance, &member.Balance, strings.Join(noteParts, ", ")); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit team member limits: %w", err)
	}
	s.invalidateBalanceCaches(ctx, memberID)
	view := teamMemberView(membership, updated)
	return &view, nil
}

func (s *TeamService) Dissolve(ctx context.Context, ownerID int64) error {
	return s.dissolveTeam(ctx, ownerID, false)
}

func (s *TeamService) AdminDeleteTeam(ctx context.Context, _ int64, teamID int64) error {
	return s.dissolveTeam(ctx, teamID, true)
}

func (s *TeamService) dissolveTeam(ctx context.Context, ownerOrTeamID int64, admin bool) error {
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin team dissolution transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	teamQuery := client.Team.Query()
	if admin {
		teamQuery = teamQuery.Where(dbteam.IDEQ(ownerOrTeamID))
	} else {
		teamQuery = teamQuery.Where(dbteam.OwnerIDEQ(ownerOrTeamID))
	}
	teamEntity, err := s.lockTeamQuery(teamQuery).Only(txCtx)
	if err != nil {
		return translateTeamError(err)
	}
	if !admin && teamEntity.Status != TeamStatusActive {
		return ErrTeamSuspended
	}
	ownerID := teamEntity.OwnerID
	memberships, err := s.lockMembershipQuery(client.TeamMembership.Query().Where(dbmembership.TeamIDEQ(teamEntity.ID))).All(txCtx)
	if err != nil {
		return fmt.Errorf("lock team memberships: %w", err)
	}

	userIDs := []int64{ownerID}
	memberIDs := make([]int64, 0, len(memberships))
	for _, membership := range memberships {
		if membership.Status == TeamMembershipActive || membership.Status == TeamMembershipExitPending {
			userIDs = append(userIDs, membership.UserID)
			memberIDs = append(memberIDs, membership.UserID)
		}
	}
	users, err := s.lockUserQuery(client.User.Query().Where(dbuser.IDIn(userIDs...)).Order(dbent.Asc(dbuser.FieldID))).All(txCtx)
	if err != nil {
		return fmt.Errorf("lock team users for dissolution: %w", err)
	}
	if len(users) != len(userIDs) {
		return ErrTeamUserNotFound
	}

	ownerAfter := 0.0
	ownerFound := false
	for _, user := range users {
		if user.ID == ownerID {
			ownerAfter = user.Balance
			ownerFound = true
			break
		}
	}
	if !ownerFound {
		return ErrTeamUserNotFound
	}
	for _, user := range users {
		if user.ID == ownerID {
			continue
		}
		if math.Abs(user.FrozenBalance) >= teamBalanceEpsilon {
			return ErrTeamFrozenBalancePending
		}
		ownerAfter = roundTeamAmount(ownerAfter + user.Balance)
	}
	if _, err := client.User.UpdateOneID(ownerID).SetBalance(ownerAfter).Save(txCtx); err != nil {
		return fmt.Errorf("return team balances to owner: %w", err)
	}
	for _, memberID := range memberIDs {
		if _, err := client.User.UpdateOneID(memberID).SetBalance(0).Save(txCtx); err != nil {
			return fmt.Errorf("clear dissolved team member balance: %w", err)
		}
	}
	if err := client.Team.DeleteOneID(teamEntity.ID).Exec(txCtx); err != nil {
		return fmt.Errorf("delete dissolved team: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit team dissolution: %w", err)
	}
	s.invalidateBalanceCaches(ctx, userIDs...)
	return nil
}

func (s *TeamService) AdminRemoveMember(ctx context.Context, adminID, teamID, memberID int64) error {
	return s.reclaimAndRemove(ctx, teamID, memberID, adminID, TeamActionAdminRemoved, true)
}

func (s *TeamService) AdminCreateTeam(ctx context.Context, adminID int64, ownerEmail, name string) (*TeamSummary, error) {
	ownerEmail = strings.TrimSpace(ownerEmail)
	name = strings.TrimSpace(name)
	if ownerEmail == "" {
		return nil, infraBadRequest("TEAM_OWNER_REQUIRED", "请输入主账号邮箱")
	}
	if name == "" || len([]rune(name)) > 100 {
		return nil, infraBadRequest("TEAM_NAME_INVALID", "请输入团队名称，且不能超过 100 个字符")
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("开始管理员创建团队事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	owner, err := s.lockUserQuery(client.User.Query().Where(dbuser.EmailEqualFold(ownerEmail))).Only(txCtx)
	if err != nil {
		return nil, translateTeamUserError(err)
	}
	if owner.Status != domain.StatusActive || owner.Role == domain.RoleAdmin {
		return nil, infraBadRequest("TEAM_OWNER_INELIGIBLE", "只有状态正常的普通用户可以成为团队主账号")
	}
	owned, err := client.Team.Query().Where(dbteam.OwnerIDEQ(owner.ID)).Exist(txCtx)
	if err != nil {
		return nil, err
	}
	member, err := client.TeamMembership.Query().Where(dbmembership.UserIDEQ(owner.ID)).Exist(txCtx)
	if err != nil {
		return nil, err
	}
	if owned || member {
		return nil, ErrTeamAlreadyExists
	}
	teamEntity, err := client.Team.Create().SetName(name).SetOwnerID(owner.ID).SetStatus(TeamStatusActive).Save(txCtx)
	if err != nil {
		return nil, fmt.Errorf("创建团队失败: %w", err)
	}
	if err := createTeamTransaction(txCtx, client, teamEntity.ID, adminID, nil, TeamActionCreated, 0, owner.Balance, owner.Balance, nil, nil, "管理员创建团队"); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("提交管理员创建团队事务失败: %w", err)
	}
	return s.GetAdminTeam(ctx, teamEntity.ID)
}

func (s *TeamService) AdminAddMember(ctx context.Context, adminID, teamID int64, email, remark string) (*TeamMemberView, error) {
	email = strings.TrimSpace(email)
	remark = strings.TrimSpace(remark)
	if email == "" {
		return nil, infraBadRequest("TEAM_INVITEE_REQUIRED", "请输入成员邮箱")
	}
	if len([]rune(remark)) > 100 {
		return nil, infraBadRequest("TEAM_REMARK_INVALID", "成员备注不能超过 100 个字符")
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("开始管理员添加成员事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	teamEntity, err := s.lockTeamQuery(client.Team.Query().Where(dbteam.IDEQ(teamID))).Only(txCtx)
	if err != nil {
		return nil, translateTeamError(err)
	}
	if teamEntity.Status != TeamStatusActive {
		return nil, ErrTeamSuspended
	}
	count, err := client.TeamMembership.Query().Where(dbmembership.TeamIDEQ(teamID)).Count(txCtx)
	if err != nil {
		return nil, err
	}
	if count >= MaxTeamMembers {
		return nil, ErrTeamMemberLimit
	}
	targetRef, err := client.User.Query().Where(dbuser.EmailEqualFold(email)).Only(txCtx)
	if err != nil {
		return nil, translateTeamUserError(err)
	}
	if targetRef.ID == teamEntity.OwnerID {
		return nil, infraBadRequest("TEAM_INVITEE_INELIGIBLE", "主账号不能重复添加为团队成员")
	}
	owner, member, err := s.lockTeamUsers(txCtx, client, teamEntity.OwnerID, targetRef.ID)
	if err != nil {
		return nil, err
	}
	if member.Status != domain.StatusActive || member.Role == domain.RoleAdmin {
		return nil, infraBadRequest("TEAM_INVITEE_INELIGIBLE", "该用户不符合加入团队的条件")
	}
	owned, err := client.Team.Query().Where(dbteam.OwnerIDEQ(member.ID)).Exist(txCtx)
	if err != nil {
		return nil, err
	}
	membershipExists, err := client.TeamMembership.Query().Where(dbmembership.UserIDEQ(member.ID)).Exist(txCtx)
	if err != nil {
		return nil, err
	}
	if owned || membershipExists {
		return nil, ErrTeamMemberExists
	}
	if math.Abs(member.FrozenBalance) >= teamBalanceEpsilon {
		return nil, ErrTeamFrozenBalancePending
	}
	pendingPayment, err := hasPendingTeamPayment(txCtx, client, member.ID)
	if err != nil {
		return nil, err
	}
	if pendingPayment {
		return nil, ErrTeamPendingPayments
	}
	memberBefore := member.Balance
	memberAfter := 0.0
	ownerAfter := roundTeamAmount(owner.Balance + memberBefore)
	if _, err := client.User.UpdateOneID(owner.ID).SetBalance(ownerAfter).Save(txCtx); err != nil {
		return nil, fmt.Errorf("合并成员余额到主账号失败: %w", err)
	}
	if _, err := client.User.UpdateOneID(member.ID).SetBalance(memberAfter).Save(txCtx); err != nil {
		return nil, fmt.Errorf("清空新成员原余额失败: %w", err)
	}
	now := time.Now()
	membership, err := client.TeamMembership.Create().
		SetTeamID(teamID).
		SetUserID(member.ID).
		SetInvitedBy(adminID).
		SetRemark(remark).
		SetStatus(TeamMembershipActive).
		SetJoinedAt(now).
		Save(txCtx)
	if err != nil {
		return nil, fmt.Errorf("添加团队成员失败: %w", err)
	}
	if err := createTeamTransaction(txCtx, client, teamID, adminID, &member.ID, TeamActionAdminMemberAdded, memberBefore, owner.Balance, ownerAfter, &memberBefore, &memberAfter, "管理员直接添加成员"); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("提交管理员添加成员事务失败: %w", err)
	}
	s.invalidateBalanceCaches(ctx, owner.ID, member.ID)
	member.Balance = memberAfter
	view := teamMemberView(membership, member)
	return &view, nil
}

func (s *TeamService) reclaimAndRemove(ctx context.Context, ownerOrTeamID, memberID, operatorID int64, action string, admin bool) error {
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin team member removal transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	teamQuery := client.Team.Query()
	if admin {
		teamQuery = teamQuery.Where(dbteam.IDEQ(ownerOrTeamID))
	} else {
		teamQuery = teamQuery.Where(dbteam.OwnerIDEQ(ownerOrTeamID))
	}
	teamEntity, err := s.lockTeamQuery(teamQuery).Only(txCtx)
	if err != nil {
		return translateTeamError(err)
	}
	if !admin && teamEntity.Status != TeamStatusActive {
		return ErrTeamSuspended
	}
	membershipQuery := client.TeamMembership.Query().Where(
		dbmembership.TeamIDEQ(teamEntity.ID),
		dbmembership.UserIDEQ(memberID),
		dbmembership.StatusIn(TeamMembershipInvited, TeamMembershipActive, TeamMembershipExitPending),
	)
	if action == TeamActionExitApproved {
		membershipQuery = membershipQuery.Where(dbmembership.StatusEQ(TeamMembershipExitPending))
	}
	membership, err := s.lockMembershipQuery(membershipQuery).Only(txCtx)
	if err != nil {
		return translateTeamMemberError(err)
	}
	if membership.Status == TeamMembershipInvited {
		owner, err := s.lockUserQuery(client.User.Query().Where(dbuser.IDEQ(teamEntity.OwnerID))).Only(txCtx)
		if err != nil {
			return translateTeamUserError(err)
		}
		if err := client.TeamMembership.DeleteOneID(membership.ID).Exec(txCtx); err != nil {
			return fmt.Errorf("cancel team invitation: %w", err)
		}
		inviteAction := TeamActionInviteCancelled
		if admin {
			inviteAction = TeamActionAdminInviteCancelled
		}
		if err := createTeamTransaction(txCtx, client, teamEntity.ID, operatorID, &memberID, inviteAction, 0, owner.Balance, owner.Balance, nil, nil, ""); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit team invitation cancellation: %w", err)
		}
		return nil
	}
	owner, member, err := s.lockTeamUsers(txCtx, client, teamEntity.OwnerID, memberID)
	if err != nil {
		return err
	}
	if math.Abs(member.FrozenBalance) >= teamBalanceEpsilon {
		return ErrTeamFrozenBalancePending
	}
	ownerAfter := roundTeamAmount(owner.Balance + member.Balance)
	memberAfter := 0.0
	if _, err := client.User.UpdateOneID(owner.ID).SetBalance(ownerAfter).Save(txCtx); err != nil {
		return fmt.Errorf("return member balance to owner: %w", err)
	}
	if _, err := client.User.UpdateOneID(member.ID).SetBalance(memberAfter).Save(txCtx); err != nil {
		return fmt.Errorf("clear member balance: %w", err)
	}
	if err := client.TeamMembership.DeleteOneID(membership.ID).Exec(txCtx); err != nil {
		return fmt.Errorf("remove team membership: %w", err)
	}
	if err := createTeamTransaction(txCtx, client, teamEntity.ID, operatorID, &memberID, action, member.Balance, owner.Balance, ownerAfter, &member.Balance, &memberAfter, ""); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit team member removal: %w", err)
	}
	s.invalidateBalanceCaches(ctx, owner.ID, member.ID)
	return nil
}

func (s *TeamService) SetStatus(ctx context.Context, adminID, teamID int64, status, reason string) (*TeamSummary, error) {
	if status != TeamStatusActive && status != TeamStatusSuspended {
		return nil, infraBadRequest("TEAM_STATUS_INVALID", "团队状态只能是正常或已冻结")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" || len([]rune(reason)) > 450 {
		return nil, infraBadRequest("TEAM_STATUS_REASON_INVALID", "请输入操作原因，且不能超过 450 个字符")
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin team status transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	teamEntity, err := s.lockTeamQuery(client.Team.Query().Where(dbteam.IDEQ(teamID))).Only(txCtx)
	if err != nil {
		return nil, translateTeamError(err)
	}
	owner, err := s.lockUserQuery(client.User.Query().Where(dbuser.IDEQ(teamEntity.OwnerID))).Only(txCtx)
	if err != nil {
		return nil, translateTeamUserError(err)
	}
	if _, err := client.Team.UpdateOneID(teamID).SetStatus(status).Save(txCtx); err != nil {
		return nil, fmt.Errorf("update team status: %w", err)
	}
	if err := createTeamTransaction(txCtx, client, teamID, adminID, nil, TeamActionStatusChanged, 0, owner.Balance, owner.Balance, nil, nil, status+": "+reason); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit team status transaction: %w", err)
	}
	return s.GetAdminTeam(ctx, teamID)
}

func (s *TeamService) IsFinancialActionRestricted(ctx context.Context, userID int64) (bool, error) {
	return s.client.TeamMembership.Query().Where(
		dbmembership.UserIDEQ(userID),
		dbmembership.StatusIn(TeamMembershipActive, TeamMembershipExitPending),
	).Exist(ctx)
}

func (s *TeamService) requireOwnedTeam(ctx context.Context, ownerID int64, active bool) (*dbent.Team, error) {
	teamEntity, err := s.client.Team.Query().Where(dbteam.OwnerIDEQ(ownerID)).Only(ctx)
	if err != nil {
		return nil, translateTeamError(err)
	}
	if active && teamEntity.Status != TeamStatusActive {
		return nil, ErrTeamSuspended
	}
	return teamEntity, nil
}

func (s *TeamService) lockTeamUsers(ctx context.Context, client *dbent.Client, ownerID, memberID int64) (*dbent.User, *dbent.User, error) {
	query := client.User.Query().Where(dbuser.IDIn(ownerID, memberID)).Order(dbent.Asc(dbuser.FieldID))
	users, err := s.lockUserQuery(query).All(ctx)
	if err != nil {
		return nil, nil, err
	}
	if len(users) != 2 {
		return nil, nil, ErrTeamUserNotFound
	}
	var owner, member *dbent.User
	for _, user := range users {
		if user.ID == ownerID {
			owner = user
		}
		if user.ID == memberID {
			member = user
		}
	}
	if owner == nil || member == nil {
		return nil, nil, ErrTeamUserNotFound
	}
	return owner, member, nil
}

func (s *TeamService) lockUserQuery(query *dbent.UserQuery) *dbent.UserQuery {
	if s.rowLocks {
		return query.ForUpdate()
	}
	return query
}

func (s *TeamService) lockTeamQuery(query *dbent.TeamQuery) *dbent.TeamQuery {
	if s.rowLocks {
		return query.ForUpdate()
	}
	return query
}

func (s *TeamService) lockMembershipQuery(query *dbent.TeamMembershipQuery) *dbent.TeamMembershipQuery {
	if s.rowLocks {
		return query.ForUpdate()
	}
	return query
}

func createTeamTransaction(ctx context.Context, client *dbent.Client, teamID, operatorID int64, memberID *int64, action string, amount, ownerBefore, ownerAfter float64, memberBefore, memberAfter *float64, note string) error {
	_, err := client.TeamTransaction.Create().
		SetTeamID(teamID).
		SetOperatorID(operatorID).
		SetNillableMemberID(memberID).
		SetAction(action).
		SetAmount(amount).
		SetOwnerBalanceBefore(ownerBefore).
		SetOwnerBalanceAfter(ownerAfter).
		SetNillableMemberBalanceBefore(memberBefore).
		SetNillableMemberBalanceAfter(memberAfter).
		SetNote(note).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("record team transaction: %w", err)
	}
	return nil
}

func hasPendingTeamPayment(ctx context.Context, client *dbent.Client, userID int64) (bool, error) {
	return client.PaymentOrder.Query().Where(
		dbpaymentorder.UserIDEQ(userID),
		dbpaymentorder.StatusIn(
			OrderStatusPending,
			OrderStatusPaid,
			OrderStatusRecharging,
			OrderStatusRefundRequested,
			OrderStatusRefunding,
			OrderStatusRefundPending,
		),
	).Exist(ctx)
}

func teamMemberView(membership *dbent.TeamMembership, user *dbent.User) TeamMemberView {
	return TeamMemberView{
		MembershipID:    membership.ID,
		UserID:          user.ID,
		Email:           user.Email,
		Username:        user.Username,
		Remark:          membership.Remark,
		Status:          membership.Status,
		Balance:         user.Balance,
		FrozenBalance:   user.FrozenBalance,
		Concurrency:     user.Concurrency,
		RPMLimit:        user.RpmLimit,
		JoinedAt:        membership.JoinedAt,
		ExitRequestedAt: membership.ExitRequestedAt,
		CreatedAt:       membership.CreatedAt,
	}
}

func roundTeamAmount(value float64) float64 {
	return math.Round(value*1e8) / 1e8
}

func (s *TeamService) invalidateBalanceCaches(ctx context.Context, userIDs ...int64) {
	if s.authCacheInvalidator == nil {
		return
	}
	for _, userID := range userIDs {
		s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
	}
}

func translateTeamError(err error) error {
	if dbent.IsNotFound(err) {
		return ErrTeamNotFound
	}
	return err
}

func translateTeamMemberError(err error) error {
	if dbent.IsNotFound(err) {
		return ErrTeamMemberNotFound
	}
	return err
}

func translateTeamUserError(err error) error {
	if dbent.IsNotFound(err) {
		return ErrTeamUserNotFound
	}
	return err
}

func infraBadRequest(code, message string) error {
	return infraerrors.BadRequest(code, message)
}
