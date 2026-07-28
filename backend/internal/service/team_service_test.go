//go:build unit

package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/ent/teammembership"
	"github.com/Wei-Shaw/sub2api/ent/teamtransaction"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

func newTeamTestService(t *testing.T) (*TeamService, *dbent.Client) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared&_fk=1", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	driver := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(driver)))
	t.Cleanup(func() { _ = client.Close() })
	return NewTeamService(client, db, nil), client
}

func createTeamTestUser(t *testing.T, client *dbent.Client, email string, balance, frozen float64) *dbent.User {
	t.Helper()
	user, err := client.User.Create().
		SetEmail(email).
		SetPasswordHash("hash").
		SetRole(RoleUser).
		SetStatus(StatusActive).
		SetBalance(balance).
		SetFrozenBalance(frozen).
		Save(context.Background())
	require.NoError(t, err)
	return user
}

func activateTeamMember(t *testing.T, svc *TeamService, owner, member *dbent.User) {
	t.Helper()
	ctx := context.Background()
	_, err := svc.Upgrade(ctx, owner.ID, "Engineering")
	require.NoError(t, err)
	_, err = svc.Invite(ctx, owner.ID, member.Email)
	require.NoError(t, err)
	_, err = svc.RespondInvitation(ctx, member.ID, true)
	require.NoError(t, err)
}

func TestTeamBalanceAllocationAndApprovedExitAreAtomic(t *testing.T) {
	svc, client := newTeamTestService(t)
	owner := createTeamTestUser(t, client, "owner@example.com", 100, 0)
	member := createTeamTestUser(t, client, "member@example.com", 0, 0)
	activateTeamMember(t, svc, owner, member)

	view, err := svc.AllocateBalance(context.Background(), owner.ID, member.ID, 25, "monthly budget")
	require.NoError(t, err)
	require.InDelta(t, 25, view.Balance, teamBalanceEpsilon)

	reloadedOwner, err := client.User.Get(context.Background(), owner.ID)
	require.NoError(t, err)
	reloadedMember, err := client.User.Get(context.Background(), member.ID)
	require.NoError(t, err)
	require.InDelta(t, 75, reloadedOwner.Balance, teamBalanceEpsilon)
	require.InDelta(t, 25, reloadedMember.Balance, teamBalanceEpsilon)

	_, err = svc.RequestExit(context.Background(), member.ID)
	require.NoError(t, err)
	require.NoError(t, svc.ReviewExit(context.Background(), owner.ID, member.ID, true))

	reloadedOwner, err = client.User.Get(context.Background(), owner.ID)
	require.NoError(t, err)
	reloadedMember, err = client.User.Get(context.Background(), member.ID)
	require.NoError(t, err)
	require.InDelta(t, 100, reloadedOwner.Balance, teamBalanceEpsilon)
	require.InDelta(t, 0, reloadedMember.Balance, teamBalanceEpsilon)
	exists, err := client.TeamMembership.Query().Where(teammembership.UserIDEQ(member.ID)).Exist(context.Background())
	require.NoError(t, err)
	require.False(t, exists)
}

func TestTeamAllocationRejectsInsufficientOwnerBalanceWithoutPartialCredit(t *testing.T) {
	svc, client := newTeamTestService(t)
	owner := createTeamTestUser(t, client, "owner-low@example.com", 10, 0)
	member := createTeamTestUser(t, client, "member-low@example.com", 0, 0)
	activateTeamMember(t, svc, owner, member)

	_, err := svc.AllocateBalance(context.Background(), owner.ID, member.ID, 20, "")
	require.ErrorIs(t, err, ErrTeamInsufficientBalance)
	reloadedOwner, _ := client.User.Get(context.Background(), owner.ID)
	reloadedMember, _ := client.User.Get(context.Background(), member.ID)
	require.InDelta(t, 10, reloadedOwner.Balance, teamBalanceEpsilon)
	require.InDelta(t, 0, reloadedMember.Balance, teamBalanceEpsilon)
}

func TestTeamBalanceReclaimReturnsBalanceToOwnerAndRecordsMember(t *testing.T) {
	svc, client := newTeamTestService(t)
	owner := createTeamTestUser(t, client, "owner-reclaim@example.com", 100, 0)
	member := createTeamTestUser(t, client, "member-reclaim@example.com", 0, 0)
	_, err := client.User.UpdateOneID(member.ID).SetUsername("member-reclaim").Save(context.Background())
	require.NoError(t, err)
	activateTeamMember(t, svc, owner, member)
	_, err = client.TeamMembership.Update().
		Where(teammembership.UserIDEQ(member.ID)).
		SetRemark("研发一组").
		Save(context.Background())
	require.NoError(t, err)

	_, err = svc.AllocateBalance(context.Background(), owner.ID, member.ID, 40, "首笔额度")
	require.NoError(t, err)
	view, err := svc.AllocateBalance(context.Background(), owner.ID, member.ID, -10, "收回未使用额度")
	require.NoError(t, err)
	require.InDelta(t, 30, view.Balance, teamBalanceEpsilon)

	reloadedOwner, err := client.User.Get(context.Background(), owner.ID)
	require.NoError(t, err)
	reloadedMember, err := client.User.Get(context.Background(), member.ID)
	require.NoError(t, err)
	require.InDelta(t, 70, reloadedOwner.Balance, teamBalanceEpsilon)
	require.InDelta(t, 30, reloadedMember.Balance, teamBalanceEpsilon)

	teamEntity, err := client.Team.Query().Only(context.Background())
	require.NoError(t, err)
	transactions, _, err := svc.ListAdminTransactions(context.Background(), teamEntity.ID, 1, 20)
	require.NoError(t, err)
	var recovered *TeamTransactionView
	for index := range transactions {
		if transactions[index].Action == TeamActionBalanceRecovered {
			recovered = &transactions[index]
			break
		}
	}
	require.NotNil(t, recovered)
	require.InDelta(t, -10, recovered.Amount, teamBalanceEpsilon)
	require.Equal(t, member.Email, recovered.MemberEmail)
	require.Equal(t, "member-reclaim", recovered.MemberUsername)
	require.Equal(t, "研发一组", recovered.MemberRemark)
	require.InDelta(t, 40, *recovered.MemberBalanceBefore, teamBalanceEpsilon)
	require.InDelta(t, 30, *recovered.MemberBalanceAfter, teamBalanceEpsilon)
}

func TestTeamBalanceReclaimRejectsInsufficientMemberBalanceWithoutPartialDebit(t *testing.T) {
	svc, client := newTeamTestService(t)
	owner := createTeamTestUser(t, client, "owner-reclaim-low@example.com", 100, 0)
	member := createTeamTestUser(t, client, "member-reclaim-low@example.com", 0, 0)
	activateTeamMember(t, svc, owner, member)
	_, err := svc.AllocateBalance(context.Background(), owner.ID, member.ID, 5, "")
	require.NoError(t, err)

	_, err = svc.AllocateBalance(context.Background(), owner.ID, member.ID, -10, "")
	require.ErrorIs(t, err, ErrTeamMemberInsufficientBalance)
	reloadedOwner, _ := client.User.Get(context.Background(), owner.ID)
	reloadedMember, _ := client.User.Get(context.Background(), member.ID)
	require.InDelta(t, 95, reloadedOwner.Balance, teamBalanceEpsilon)
	require.InDelta(t, 5, reloadedMember.Balance, teamBalanceEpsilon)
}

func TestTeamRemovalRejectsFrozenBalance(t *testing.T) {
	svc, client := newTeamTestService(t)
	owner := createTeamTestUser(t, client, "owner-frozen@example.com", 50, 0)
	member := createTeamTestUser(t, client, "member-frozen@example.com", 0, 0)
	activateTeamMember(t, svc, owner, member)
	_, err := client.User.UpdateOneID(member.ID).SetFrozenBalance(3).Save(context.Background())
	require.NoError(t, err)

	err = svc.RemoveMember(context.Background(), owner.ID, member.ID)
	require.ErrorIs(t, err, ErrTeamFrozenBalancePending)
	exists, queryErr := client.TeamMembership.Query().Where(teammembership.UserIDEQ(member.ID)).Exist(context.Background())
	require.NoError(t, queryErr)
	require.True(t, exists)
}

func TestTeamFinancialRestrictionStartsAfterInvitationAcceptance(t *testing.T) {
	svc, client := newTeamTestService(t)
	owner := createTeamTestUser(t, client, "owner-guard@example.com", 50, 0)
	member := createTeamTestUser(t, client, "member-guard@example.com", 0, 0)
	_, err := svc.Upgrade(context.Background(), owner.ID, "Guard Team")
	require.NoError(t, err)
	_, err = svc.Invite(context.Background(), owner.ID, member.Email)
	require.NoError(t, err)

	restricted, err := svc.IsFinancialActionRestricted(context.Background(), member.ID)
	require.NoError(t, err)
	require.False(t, restricted)
	_, err = svc.RespondInvitation(context.Background(), member.ID, true)
	require.NoError(t, err)
	restricted, err = svc.IsFinancialActionRestricted(context.Background(), member.ID)
	require.NoError(t, err)
	require.True(t, restricted)
}

func TestResolveUsageUserIDsUsesJoinedTeamMembersOnly(t *testing.T) {
	svc, client := newTeamTestService(t)
	ctx := context.Background()
	owner := createTeamTestUser(t, client, "owner-usage-scope@example.com", 50, 0)
	activeMember := createTeamTestUser(t, client, "active-usage-scope@example.com", 0, 0)
	exitPendingMember := createTeamTestUser(t, client, "exit-usage-scope@example.com", 0, 0)
	invitee := createTeamTestUser(t, client, "invitee-usage-scope@example.com", 0, 0)

	activateTeamMember(t, svc, owner, activeMember)
	_, err := svc.Invite(ctx, owner.ID, exitPendingMember.Email)
	require.NoError(t, err)
	_, err = svc.RespondInvitation(ctx, exitPendingMember.ID, true)
	require.NoError(t, err)
	_, err = svc.RequestExit(ctx, exitPendingMember.ID)
	require.NoError(t, err)
	_, err = svc.Invite(ctx, owner.ID, invitee.Email)
	require.NoError(t, err)

	userIDs, err := svc.ResolveUsageUserIDs(ctx, owner.ID, 0)
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{owner.ID, activeMember.ID, exitPendingMember.ID}, userIDs)
	require.NotContains(t, userIDs, invitee.ID)

	selected, err := svc.ResolveUsageUserIDs(ctx, owner.ID, activeMember.ID)
	require.NoError(t, err)
	require.Equal(t, []int64{activeMember.ID}, selected)

	_, err = svc.ResolveUsageUserIDs(ctx, owner.ID, invitee.ID)
	require.ErrorIs(t, err, ErrTeamMemberNotFound)

	personal, err := svc.ResolveUsageUserIDs(ctx, activeMember.ID, 0)
	require.NoError(t, err)
	require.Equal(t, []int64{activeMember.ID}, personal)
}

func TestTeamOwnerCanCancelInvitationWithoutTakingInviteeBalance(t *testing.T) {
	svc, client := newTeamTestService(t)
	owner := createTeamTestUser(t, client, "owner-cancel@example.com", 50, 0)
	invitee := createTeamTestUser(t, client, "invitee-cancel@example.com", 17, 0)
	_, err := svc.Upgrade(context.Background(), owner.ID, "Cancel Team")
	require.NoError(t, err)
	_, err = svc.Invite(context.Background(), owner.ID, invitee.Email)
	require.NoError(t, err)

	require.NoError(t, svc.RemoveMember(context.Background(), owner.ID, invitee.ID))

	reloadedOwner, err := client.User.Get(context.Background(), owner.ID)
	require.NoError(t, err)
	reloadedInvitee, err := client.User.Get(context.Background(), invitee.ID)
	require.NoError(t, err)
	require.InDelta(t, 50, reloadedOwner.Balance, teamBalanceEpsilon)
	require.InDelta(t, 17, reloadedInvitee.Balance, teamBalanceEpsilon)
	exists, err := client.TeamMembership.Query().Where(teammembership.UserIDEQ(invitee.ID)).Exist(context.Background())
	require.NoError(t, err)
	require.False(t, exists)
	transaction, err := client.TeamTransaction.Query().Where(teamtransaction.ActionEQ(TeamActionInviteCancelled)).Only(context.Background())
	require.NoError(t, err)
	require.Equal(t, invitee.ID, *transaction.MemberID)
}

func TestAdminTeamOverviewExcludesPendingInviteBalance(t *testing.T) {
	svc, client := newTeamTestService(t)
	ownerOne := createTeamTestUser(t, client, "overview-owner-1@example.com", 100, 0)
	member := createTeamTestUser(t, client, "overview-member@example.com", 0, 0)
	activateTeamMember(t, svc, ownerOne, member)
	_, err := svc.AllocateBalance(context.Background(), ownerOne.ID, member.ID, 25, "")
	require.NoError(t, err)

	ownerTwo := createTeamTestUser(t, client, "overview-owner-2@example.com", 40, 0)
	invitee := createTeamTestUser(t, client, "overview-invitee@example.com", 17, 0)
	teamTwo, err := svc.Upgrade(context.Background(), ownerTwo.ID, "Second Team")
	require.NoError(t, err)
	_, err = svc.Invite(context.Background(), ownerTwo.ID, invitee.Email)
	require.NoError(t, err)
	_, err = svc.SetStatus(context.Background(), 999, teamTwo.Team.ID, TeamStatusSuspended, "policy review")
	require.NoError(t, err)

	overview, err := svc.GetAdminOverview(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, overview.TotalTeams)
	require.Equal(t, 1, overview.ActiveTeams)
	require.Equal(t, 1, overview.SuspendedTeams)
	require.Equal(t, 1, overview.MemberCount)
	require.Equal(t, 1, overview.PendingInvites)
	require.Equal(t, 0, overview.ExitPending)
	require.InDelta(t, 140, overview.TotalBalance, teamBalanceEpsilon)
}

func TestAdminTeamStatusRequiresReasonAndRecordsAudit(t *testing.T) {
	svc, client := newTeamTestService(t)
	owner := createTeamTestUser(t, client, "status-owner@example.com", 30, 0)
	contextView, err := svc.Upgrade(context.Background(), owner.ID, "Status Team")
	require.NoError(t, err)

	_, err = svc.SetStatus(context.Background(), 999, contextView.Team.ID, TeamStatusSuspended, "  ")
	require.Error(t, err)
	unchanged, err := client.Team.Get(context.Background(), contextView.Team.ID)
	require.NoError(t, err)
	require.Equal(t, TeamStatusActive, unchanged.Status)

	_, err = svc.SetStatus(context.Background(), 999, contextView.Team.ID, TeamStatusSuspended, "risk review")
	require.NoError(t, err)
	transaction, err := client.TeamTransaction.Query().Where(teamtransaction.ActionEQ(TeamActionStatusChanged)).Only(context.Background())
	require.NoError(t, err)
	require.Equal(t, "suspended: risk review", transaction.Note)
}

func TestTeamInvitationAcceptanceMergesExistingBalanceIntoOwner(t *testing.T) {
	svc, client := newTeamTestService(t)
	owner := createTeamTestUser(t, client, "owner-merge@example.com", 50, 0)
	member := createTeamTestUser(t, client, "member-merge@example.com", 17, 0)
	_, err := svc.Upgrade(context.Background(), owner.ID, "Merge Team")
	require.NoError(t, err)
	_, err = svc.Invite(context.Background(), owner.ID, member.Email)
	require.NoError(t, err)

	memberContext, err := svc.RespondInvitation(context.Background(), member.ID, true)
	require.NoError(t, err)
	require.Equal(t, TeamRoleMember, memberContext.Role)
	require.InDelta(t, 0, memberContext.CurrentMembership.Balance, teamBalanceEpsilon)

	reloadedOwner, err := client.User.Get(context.Background(), owner.ID)
	require.NoError(t, err)
	reloadedMember, err := client.User.Get(context.Background(), member.ID)
	require.NoError(t, err)
	require.InDelta(t, 67, reloadedOwner.Balance, teamBalanceEpsilon)
	require.InDelta(t, 0, reloadedMember.Balance, teamBalanceEpsilon)

	transaction, err := client.TeamTransaction.Query().Where(teamtransaction.ActionEQ(TeamActionInviteAccepted)).Only(context.Background())
	require.NoError(t, err)
	require.InDelta(t, 17, transaction.Amount, teamBalanceEpsilon)
	require.InDelta(t, 50, transaction.OwnerBalanceBefore, teamBalanceEpsilon)
	require.InDelta(t, 67, transaction.OwnerBalanceAfter, teamBalanceEpsilon)
	require.InDelta(t, 17, *transaction.MemberBalanceBefore, teamBalanceEpsilon)
	require.InDelta(t, 0, *transaction.MemberBalanceAfter, teamBalanceEpsilon)
}

func TestTeamInvitationAcceptanceRejectsFrozenBalanceWithoutTransfer(t *testing.T) {
	svc, client := newTeamTestService(t)
	owner := createTeamTestUser(t, client, "owner-join-frozen@example.com", 50, 0)
	member := createTeamTestUser(t, client, "member-join-frozen@example.com", 17, 1)
	_, err := svc.Upgrade(context.Background(), owner.ID, "Frozen Join Team")
	require.NoError(t, err)
	_, err = svc.Invite(context.Background(), owner.ID, member.Email)
	require.NoError(t, err)

	_, err = svc.RespondInvitation(context.Background(), member.ID, true)
	require.ErrorIs(t, err, ErrTeamFrozenBalancePending)
	reloadedOwner, _ := client.User.Get(context.Background(), owner.ID)
	reloadedMember, _ := client.User.Get(context.Background(), member.ID)
	require.InDelta(t, 50, reloadedOwner.Balance, teamBalanceEpsilon)
	require.InDelta(t, 17, reloadedMember.Balance, teamBalanceEpsilon)
	membership, err := client.TeamMembership.Query().Where(teammembership.UserIDEQ(member.ID)).Only(context.Background())
	require.NoError(t, err)
	require.Equal(t, TeamMembershipInvited, membership.Status)
}

func TestTeamOwnerCanUpdateMemberLimits(t *testing.T) {
	svc, client := newTeamTestService(t)
	owner := createTeamTestUser(t, client, "owner-limits@example.com", 50, 0)
	member := createTeamTestUser(t, client, "member-limits@example.com", 0, 0)
	activateTeamMember(t, svc, owner, member)
	concurrency := 8
	rpmLimit := 60

	view, err := svc.UpdateMemberLimits(context.Background(), owner.ID, member.ID, &concurrency, &rpmLimit)
	require.NoError(t, err)
	require.Equal(t, 8, view.Concurrency)
	require.Equal(t, 60, view.RPMLimit)
	reloaded, err := client.User.Get(context.Background(), member.ID)
	require.NoError(t, err)
	require.Equal(t, 8, reloaded.Concurrency)
	require.Equal(t, 60, reloaded.RpmLimit)
	transaction, err := client.TeamTransaction.Query().Where(teamtransaction.ActionEQ(TeamActionMemberLimitsUpdated)).Only(context.Background())
	require.NoError(t, err)
	require.Contains(t, transaction.Note, "concurrency: 5 -> 8")
	require.Contains(t, transaction.Note, "rpm_limit: 0 -> 60")
}

func TestTeamOwnerCanSetMemberRemark(t *testing.T) {
	svc, client := newTeamTestService(t)
	owner := createTeamTestUser(t, client, "owner-remark@example.com", 50, 0)
	member := createTeamTestUser(t, client, "member-remark@example.com", 0, 0)
	activateTeamMember(t, svc, owner, member)

	view, err := svc.UpdateMemberRemark(context.Background(), owner.ID, member.ID, "E2E Member One")
	require.NoError(t, err)
	require.Equal(t, "E2E Member One", view.Remark)
	membership, err := client.TeamMembership.Query().Where(teammembership.UserIDEQ(member.ID)).Only(context.Background())
	require.NoError(t, err)
	require.Equal(t, "E2E Member One", membership.Remark)

	teamContext, err := svc.GetContext(context.Background(), owner.ID)
	require.NoError(t, err)
	require.Len(t, teamContext.Team.Members, 1)
	require.Equal(t, "E2E Member One", teamContext.Team.Members[0].Remark)
}

func TestAdminCanCreateEmptyTeamAndDirectlyAddMember(t *testing.T) {
	svc, client := newTeamTestService(t)
	owner := createTeamTestUser(t, client, "admin-created-owner@example.com", 28, 0)
	member := createTeamTestUser(t, client, "admin-added-member@example.com", 7, 0)

	teamSummary, err := svc.AdminCreateTeam(context.Background(), 999, owner.Email, "Admin Team")
	require.NoError(t, err)
	require.Empty(t, teamSummary.Members)
	require.Equal(t, owner.ID, teamSummary.Owner.UserID)

	view, err := svc.AdminAddMember(context.Background(), 999, teamSummary.ID, member.Email, "财务负责人")
	require.NoError(t, err)
	require.Equal(t, TeamMembershipActive, view.Status)
	require.Equal(t, "财务负责人", view.Remark)
	require.InDelta(t, 0, view.Balance, teamBalanceEpsilon)

	reloadedOwner, err := client.User.Get(context.Background(), owner.ID)
	require.NoError(t, err)
	reloadedMember, err := client.User.Get(context.Background(), member.ID)
	require.NoError(t, err)
	require.InDelta(t, 35, reloadedOwner.Balance, teamBalanceEpsilon)
	require.InDelta(t, 0, reloadedMember.Balance, teamBalanceEpsilon)
	transaction, err := client.TeamTransaction.Query().Where(teamtransaction.ActionEQ(TeamActionAdminMemberAdded)).Only(context.Background())
	require.NoError(t, err)
	require.InDelta(t, 7, transaction.Amount, teamBalanceEpsilon)
}

func TestAdminRemoveMemberReturnsBalanceToOwner(t *testing.T) {
	svc, client := newTeamTestService(t)
	owner := createTeamTestUser(t, client, "admin-remove-owner@example.com", 28, 0)
	member := createTeamTestUser(t, client, "admin-remove-member@example.com", 7, 0)

	teamSummary, err := svc.AdminCreateTeam(context.Background(), 999, owner.Email, "Admin Remove Team")
	require.NoError(t, err)
	_, err = svc.AdminAddMember(context.Background(), 999, teamSummary.ID, member.Email, "")
	require.NoError(t, err)
	_, err = svc.AllocateBalance(context.Background(), owner.ID, member.ID, 6, "")
	require.NoError(t, err)

	require.NoError(t, svc.AdminRemoveMember(context.Background(), 999, teamSummary.ID, member.ID))
	reloadedOwner, _ := client.User.Get(context.Background(), owner.ID)
	reloadedMember, _ := client.User.Get(context.Background(), member.ID)
	require.InDelta(t, 35, reloadedOwner.Balance, teamBalanceEpsilon)
	require.InDelta(t, 0, reloadedMember.Balance, teamBalanceEpsilon)
	membershipCount, err := client.TeamMembership.Query().Count(context.Background())
	require.NoError(t, err)
	require.Zero(t, membershipCount)
}

func TestAdminDeleteSuspendedTeamReturnsMemberBalances(t *testing.T) {
	svc, client := newTeamTestService(t)
	owner := createTeamTestUser(t, client, "admin-delete-owner@example.com", 28, 0)
	member := createTeamTestUser(t, client, "admin-delete-member@example.com", 7, 0)

	teamSummary, err := svc.AdminCreateTeam(context.Background(), 999, owner.Email, "Admin Delete Team")
	require.NoError(t, err)
	_, err = svc.AdminAddMember(context.Background(), 999, teamSummary.ID, member.Email, "")
	require.NoError(t, err)
	_, err = svc.AllocateBalance(context.Background(), owner.ID, member.ID, 5, "")
	require.NoError(t, err)
	_, err = svc.SetStatus(context.Background(), 999, teamSummary.ID, TeamStatusSuspended, "管理员删除前冻结")
	require.NoError(t, err)

	require.NoError(t, svc.AdminDeleteTeam(context.Background(), 999, teamSummary.ID))
	reloadedOwner, _ := client.User.Get(context.Background(), owner.ID)
	reloadedMember, _ := client.User.Get(context.Background(), member.ID)
	require.InDelta(t, 35, reloadedOwner.Balance, teamBalanceEpsilon)
	require.InDelta(t, 0, reloadedMember.Balance, teamBalanceEpsilon)
	teamCount, err := client.Team.Query().Count(context.Background())
	require.NoError(t, err)
	require.Zero(t, teamCount)
	membershipCount, err := client.TeamMembership.Query().Count(context.Background())
	require.NoError(t, err)
	require.Zero(t, membershipCount)
}

func TestTeamDissolveReturnsMemberBalancesAndLeavesInviteeUntouched(t *testing.T) {
	svc, client := newTeamTestService(t)
	owner := createTeamTestUser(t, client, "owner-dissolve@example.com", 100, 0)
	member := createTeamTestUser(t, client, "member-dissolve@example.com", 0, 0)
	invitee := createTeamTestUser(t, client, "invitee-dissolve@example.com", 7, 0)
	activateTeamMember(t, svc, owner, member)
	_, err := svc.AllocateBalance(context.Background(), owner.ID, member.ID, 25, "")
	require.NoError(t, err)
	_, err = svc.Invite(context.Background(), owner.ID, invitee.Email)
	require.NoError(t, err)

	require.NoError(t, svc.Dissolve(context.Background(), owner.ID))
	reloadedOwner, _ := client.User.Get(context.Background(), owner.ID)
	reloadedMember, _ := client.User.Get(context.Background(), member.ID)
	reloadedInvitee, _ := client.User.Get(context.Background(), invitee.ID)
	require.InDelta(t, 100, reloadedOwner.Balance, teamBalanceEpsilon)
	require.InDelta(t, 0, reloadedMember.Balance, teamBalanceEpsilon)
	require.InDelta(t, 7, reloadedInvitee.Balance, teamBalanceEpsilon)
	teamCount, err := client.Team.Query().Count(context.Background())
	require.NoError(t, err)
	require.Zero(t, teamCount)
	membershipCount, err := client.TeamMembership.Query().Count(context.Background())
	require.NoError(t, err)
	require.Zero(t, membershipCount)
}

func TestTeamDissolveRejectsFrozenMemberWithoutPartialChanges(t *testing.T) {
	svc, client := newTeamTestService(t)
	owner := createTeamTestUser(t, client, "owner-dissolve-frozen@example.com", 100, 0)
	member := createTeamTestUser(t, client, "member-dissolve-frozen@example.com", 0, 0)
	activateTeamMember(t, svc, owner, member)
	_, err := svc.AllocateBalance(context.Background(), owner.ID, member.ID, 25, "")
	require.NoError(t, err)
	_, err = client.User.UpdateOneID(member.ID).SetFrozenBalance(2).Save(context.Background())
	require.NoError(t, err)

	err = svc.Dissolve(context.Background(), owner.ID)
	require.ErrorIs(t, err, ErrTeamFrozenBalancePending)
	reloadedOwner, _ := client.User.Get(context.Background(), owner.ID)
	reloadedMember, _ := client.User.Get(context.Background(), member.ID)
	require.InDelta(t, 75, reloadedOwner.Balance, teamBalanceEpsilon)
	require.InDelta(t, 25, reloadedMember.Balance, teamBalanceEpsilon)
	teamCount, countErr := client.Team.Query().Count(context.Background())
	require.NoError(t, countErr)
	require.Equal(t, 1, teamCount)
}
