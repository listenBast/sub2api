//go:build unit

package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbapikey "github.com/Wei-Shaw/sub2api/ent/apikey"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/ent/team"
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
	return NewTeamService(client, db, nil, nil), client
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

func createTeamTestAPIKey(t *testing.T, client *dbent.Client, userID int64, key, mode string) *dbent.APIKey {
	t.Helper()
	apiKey, err := client.APIKey.Create().
		SetUserID(userID).
		SetKey(key).
		SetName("key-" + key).
		SetBalanceMode(mode).
		Save(context.Background())
	require.NoError(t, err)
	return apiKey
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

func reloadTeamTestUser(t *testing.T, client *dbent.Client, id int64) *dbent.User {
	t.Helper()
	user, err := client.User.Get(context.Background(), id)
	require.NoError(t, err)
	return user
}

func TestTeamBalanceAllocationAndApprovedExitAreAtomic(t *testing.T) {
	svc, client := newTeamTestService(t)
	owner := createTeamTestUser(t, client, "owner@example.com", 100, 0)
	member := createTeamTestUser(t, client, "member@example.com", 3, 0)
	activateTeamMember(t, svc, owner, member)

	view, err := svc.AllocateBalance(context.Background(), owner.ID, member.ID, 25, "monthly budget")
	require.NoError(t, err)
	require.InDelta(t, 25, view.TeamBalance, teamBalanceEpsilon)
	require.InDelta(t, 3, view.Balance, teamBalanceEpsilon)
	require.InDelta(t, 28, view.TotalBalance, teamBalanceEpsilon)

	reloadedOwner := reloadTeamTestUser(t, client, owner.ID)
	reloadedMember := reloadTeamTestUser(t, client, member.ID)
	require.InDelta(t, 75, reloadedOwner.Balance, teamBalanceEpsilon)
	require.InDelta(t, 25, reloadedMember.TeamBalance, teamBalanceEpsilon)
	require.InDelta(t, 3, reloadedMember.Balance, teamBalanceEpsilon)

	_, err = svc.RequestExit(context.Background(), member.ID)
	require.NoError(t, err)
	require.NoError(t, svc.ReviewExit(context.Background(), owner.ID, member.ID, true))

	reloadedOwner = reloadTeamTestUser(t, client, owner.ID)
	reloadedMember = reloadTeamTestUser(t, client, member.ID)
	require.InDelta(t, 100, reloadedOwner.Balance, teamBalanceEpsilon)
	require.InDelta(t, 0, reloadedMember.TeamBalance, teamBalanceEpsilon)
	require.InDelta(t, 3, reloadedMember.Balance, teamBalanceEpsilon, "personal balance survives leaving the team")
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
	reloadedOwner := reloadTeamTestUser(t, client, owner.ID)
	reloadedMember := reloadTeamTestUser(t, client, member.ID)
	require.InDelta(t, 10, reloadedOwner.Balance, teamBalanceEpsilon)
	require.InDelta(t, 0, reloadedMember.TeamBalance, teamBalanceEpsilon)
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
	require.InDelta(t, 30, view.TeamBalance, teamBalanceEpsilon)

	reloadedOwner := reloadTeamTestUser(t, client, owner.ID)
	reloadedMember := reloadTeamTestUser(t, client, member.ID)
	require.InDelta(t, 70, reloadedOwner.Balance, teamBalanceEpsilon)
	require.InDelta(t, 30, reloadedMember.TeamBalance, teamBalanceEpsilon)

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
	member := createTeamTestUser(t, client, "member-reclaim-low@example.com", 50, 0)
	activateTeamMember(t, svc, owner, member)
	_, err := svc.AllocateBalance(context.Background(), owner.ID, member.ID, 5, "")
	require.NoError(t, err)

	// 成员个人余额 50 不能被主账号收回，只有团队额度 5 可以。
	_, err = svc.AllocateBalance(context.Background(), owner.ID, member.ID, -10, "")
	require.ErrorIs(t, err, ErrTeamMemberInsufficientBalance)
	reloadedOwner := reloadTeamTestUser(t, client, owner.ID)
	reloadedMember := reloadTeamTestUser(t, client, member.ID)
	require.InDelta(t, 95, reloadedOwner.Balance, teamBalanceEpsilon)
	require.InDelta(t, 5, reloadedMember.TeamBalance, teamBalanceEpsilon)
	require.InDelta(t, 50, reloadedMember.Balance, teamBalanceEpsilon)
}

func TestTeamRemovalKeepsPersonalAndFrozenBalance(t *testing.T) {
	svc, client := newTeamTestService(t)
	owner := createTeamTestUser(t, client, "owner-frozen@example.com", 50, 0)
	member := createTeamTestUser(t, client, "member-frozen@example.com", 12, 3)
	activateTeamMember(t, svc, owner, member)
	_, err := svc.AllocateBalance(context.Background(), owner.ID, member.ID, 20, "")
	require.NoError(t, err)

	require.NoError(t, svc.RemoveMember(context.Background(), owner.ID, member.ID))
	reloadedOwner := reloadTeamTestUser(t, client, owner.ID)
	reloadedMember := reloadTeamTestUser(t, client, member.ID)
	require.InDelta(t, 50, reloadedOwner.Balance, teamBalanceEpsilon)
	require.InDelta(t, 0, reloadedMember.TeamBalance, teamBalanceEpsilon)
	require.InDelta(t, 12, reloadedMember.Balance, teamBalanceEpsilon)
	require.InDelta(t, 3, reloadedMember.FrozenBalance, teamBalanceEpsilon)
	exists, queryErr := client.TeamMembership.Query().Where(teammembership.UserIDEQ(member.ID)).Exist(context.Background())
	require.NoError(t, queryErr)
	require.False(t, exists)
}

func TestTeamMembersAreNeverFinanciallyRestricted(t *testing.T) {
	svc, client := newTeamTestService(t)
	owner := createTeamTestUser(t, client, "owner-guard@example.com", 50, 0)
	member := createTeamTestUser(t, client, "member-guard@example.com", 0, 0)
	activateTeamMember(t, svc, owner, member)

	teamContext, err := svc.GetContext(context.Background(), member.ID)
	require.NoError(t, err)
	require.Equal(t, TeamRoleMember, teamContext.Role)
	require.False(t, teamContext.FinancialRestricted, "members may top up / redeem into their personal balance")
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

	reloadedOwner := reloadTeamTestUser(t, client, owner.ID)
	reloadedInvitee := reloadTeamTestUser(t, client, invitee.ID)
	require.InDelta(t, 50, reloadedOwner.Balance, teamBalanceEpsilon)
	require.InDelta(t, 17, reloadedInvitee.Balance, teamBalanceEpsilon)
	exists, err := client.TeamMembership.Query().Where(teammembership.UserIDEQ(invitee.ID)).Exist(context.Background())
	require.NoError(t, err)
	require.False(t, exists)
	transaction, err := client.TeamTransaction.Query().Where(teamtransaction.ActionEQ(TeamActionInviteCancelled)).Only(context.Background())
	require.NoError(t, err)
	require.Equal(t, invitee.ID, *transaction.MemberID)
}

func TestAdminTeamOverviewCountsOnlyTeamFunds(t *testing.T) {
	svc, client := newTeamTestService(t)
	ownerOne := createTeamTestUser(t, client, "overview-owner-1@example.com", 100, 0)
	member := createTeamTestUser(t, client, "overview-member@example.com", 9, 0)
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
	// 75 (owner one pool) + 25 (member team quota) + 40 (owner two pool); member personal 9 and invitee 17 excluded.
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

func TestTeamInvitationAcceptanceKeepsPersonalBalance(t *testing.T) {
	svc, client := newTeamTestService(t)
	owner := createTeamTestUser(t, client, "owner-merge@example.com", 50, 0)
	member := createTeamTestUser(t, client, "member-merge@example.com", 17, 1)
	_, err := svc.Upgrade(context.Background(), owner.ID, "Merge Team")
	require.NoError(t, err)
	_, err = svc.Invite(context.Background(), owner.ID, member.Email)
	require.NoError(t, err)

	memberContext, err := svc.RespondInvitation(context.Background(), member.ID, true)
	require.NoError(t, err)
	require.Equal(t, TeamRoleMember, memberContext.Role)
	require.InDelta(t, 17, memberContext.CurrentMembership.Balance, teamBalanceEpsilon)
	require.InDelta(t, 0, memberContext.CurrentMembership.TeamBalance, teamBalanceEpsilon)
	require.InDelta(t, 17, memberContext.CurrentMembership.TotalBalance, teamBalanceEpsilon)
	require.InDelta(t, 50, memberContext.Team.TotalBalance, teamBalanceEpsilon, "member personal balance is not team money")

	reloadedOwner := reloadTeamTestUser(t, client, owner.ID)
	reloadedMember := reloadTeamTestUser(t, client, member.ID)
	require.InDelta(t, 50, reloadedOwner.Balance, teamBalanceEpsilon)
	require.InDelta(t, 17, reloadedMember.Balance, teamBalanceEpsilon)
	require.InDelta(t, 1, reloadedMember.FrozenBalance, teamBalanceEpsilon)

	transaction, err := client.TeamTransaction.Query().Where(teamtransaction.ActionEQ(TeamActionInviteAccepted)).Only(context.Background())
	require.NoError(t, err)
	require.InDelta(t, 0, transaction.Amount, teamBalanceEpsilon)
	require.InDelta(t, 50, transaction.OwnerBalanceBefore, teamBalanceEpsilon)
	require.InDelta(t, 50, transaction.OwnerBalanceAfter, teamBalanceEpsilon)
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
	reloaded := reloadTeamTestUser(t, client, member.ID)
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
	require.InDelta(t, 7, view.Balance, teamBalanceEpsilon)
	require.InDelta(t, 0, view.TeamBalance, teamBalanceEpsilon)

	reloadedOwner := reloadTeamTestUser(t, client, owner.ID)
	reloadedMember := reloadTeamTestUser(t, client, member.ID)
	require.InDelta(t, 28, reloadedOwner.Balance, teamBalanceEpsilon)
	require.InDelta(t, 7, reloadedMember.Balance, teamBalanceEpsilon)
	transaction, err := client.TeamTransaction.Query().Where(teamtransaction.ActionEQ(TeamActionAdminMemberAdded)).Only(context.Background())
	require.NoError(t, err)
	require.InDelta(t, 0, transaction.Amount, teamBalanceEpsilon)
}

func TestAdminRemoveMemberReturnsTeamBalanceToOwner(t *testing.T) {
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
	reloadedOwner := reloadTeamTestUser(t, client, owner.ID)
	reloadedMember := reloadTeamTestUser(t, client, member.ID)
	require.InDelta(t, 28, reloadedOwner.Balance, teamBalanceEpsilon)
	require.InDelta(t, 0, reloadedMember.TeamBalance, teamBalanceEpsilon)
	require.InDelta(t, 7, reloadedMember.Balance, teamBalanceEpsilon)
	membershipCount, err := client.TeamMembership.Query().Count(context.Background())
	require.NoError(t, err)
	require.Zero(t, membershipCount)
}

func TestAdminDeleteSuspendedTeamReturnsMemberTeamBalances(t *testing.T) {
	svc, client := newTeamTestService(t)
	owner := createTeamTestUser(t, client, "admin-delete-owner@example.com", 28, 0)
	member := createTeamTestUser(t, client, "admin-delete-member@example.com", 7, 2)

	teamSummary, err := svc.AdminCreateTeam(context.Background(), 999, owner.Email, "Admin Delete Team")
	require.NoError(t, err)
	_, err = svc.AdminAddMember(context.Background(), 999, teamSummary.ID, member.Email, "")
	require.NoError(t, err)
	_, err = svc.AllocateBalance(context.Background(), owner.ID, member.ID, 5, "")
	require.NoError(t, err)
	_, err = svc.SetStatus(context.Background(), 999, teamSummary.ID, TeamStatusSuspended, "管理员删除前冻结")
	require.NoError(t, err)

	require.NoError(t, svc.AdminDeleteTeam(context.Background(), 999, teamSummary.ID))
	reloadedOwner := reloadTeamTestUser(t, client, owner.ID)
	reloadedMember := reloadTeamTestUser(t, client, member.ID)
	require.InDelta(t, 28, reloadedOwner.Balance, teamBalanceEpsilon)
	require.InDelta(t, 0, reloadedMember.TeamBalance, teamBalanceEpsilon)
	require.InDelta(t, 7, reloadedMember.Balance, teamBalanceEpsilon)
	require.InDelta(t, 2, reloadedMember.FrozenBalance, teamBalanceEpsilon)
	teamCount, err := client.Team.Query().Count(context.Background())
	require.NoError(t, err)
	require.Zero(t, teamCount)
	membershipCount, err := client.TeamMembership.Query().Count(context.Background())
	require.NoError(t, err)
	require.Zero(t, membershipCount)
}

func TestTeamDissolveReturnsMemberTeamBalancesAndLeavesInviteeUntouched(t *testing.T) {
	svc, client := newTeamTestService(t)
	owner := createTeamTestUser(t, client, "owner-dissolve@example.com", 100, 0)
	member := createTeamTestUser(t, client, "member-dissolve@example.com", 4, 0)
	invitee := createTeamTestUser(t, client, "invitee-dissolve@example.com", 7, 0)
	activateTeamMember(t, svc, owner, member)
	_, err := svc.AllocateBalance(context.Background(), owner.ID, member.ID, 25, "")
	require.NoError(t, err)
	_, err = svc.Invite(context.Background(), owner.ID, invitee.Email)
	require.NoError(t, err)
	createTeamTestAPIKey(t, client, member.ID, "sk-dissolve-team-only", BalanceModeTeamOnly)

	require.NoError(t, svc.Dissolve(context.Background(), owner.ID))
	reloadedOwner := reloadTeamTestUser(t, client, owner.ID)
	reloadedMember := reloadTeamTestUser(t, client, member.ID)
	reloadedInvitee := reloadTeamTestUser(t, client, invitee.ID)
	require.InDelta(t, 100, reloadedOwner.Balance, teamBalanceEpsilon)
	require.InDelta(t, 0, reloadedMember.TeamBalance, teamBalanceEpsilon)
	require.InDelta(t, 4, reloadedMember.Balance, teamBalanceEpsilon)
	require.InDelta(t, 7, reloadedInvitee.Balance, teamBalanceEpsilon)
	teamCount, err := client.Team.Query().Count(context.Background())
	require.NoError(t, err)
	require.Zero(t, teamCount)
	membershipCount, err := client.TeamMembership.Query().Count(context.Background())
	require.NoError(t, err)
	require.Zero(t, membershipCount)
	key, err := client.APIKey.Query().Where(dbapikey.KeyEQ("sk-dissolve-team-only")).Only(context.Background())
	require.NoError(t, err)
	require.Equal(t, BalanceModeTeamFirst, key.BalanceMode, "team_only keys fall back to the default once the user has no team quota")
}

func TestRemovedMemberTeamOnlyKeysFallBackToDefaultMode(t *testing.T) {
	svc, client := newTeamTestService(t)
	owner := createTeamTestUser(t, client, "owner-keys@example.com", 50, 0)
	member := createTeamTestUser(t, client, "member-keys@example.com", 0, 0)
	activateTeamMember(t, svc, owner, member)
	createTeamTestAPIKey(t, client, member.ID, "sk-keys-team-only", BalanceModeTeamOnly)
	createTeamTestAPIKey(t, client, member.ID, "sk-keys-personal-first", BalanceModePersonalFirst)

	require.NoError(t, svc.RemoveMember(context.Background(), owner.ID, member.ID))
	teamOnly, err := client.APIKey.Query().Where(dbapikey.KeyEQ("sk-keys-team-only")).Only(context.Background())
	require.NoError(t, err)
	require.Equal(t, BalanceModeTeamFirst, teamOnly.BalanceMode)
	personalFirst, err := client.APIKey.Query().Where(dbapikey.KeyEQ("sk-keys-personal-first")).Only(context.Background())
	require.NoError(t, err)
	require.Equal(t, BalanceModePersonalFirst, personalFirst.BalanceMode, "other modes are left untouched")
}

func TestOwnerCanTransferOwnershipToMember(t *testing.T) {
	svc, client := newTeamTestService(t)
	owner := createTeamTestUser(t, client, "owner-transfer@example.com", 100, 0)
	member := createTeamTestUser(t, client, "member-transfer@example.com", 8, 0)
	activateTeamMember(t, svc, owner, member)
	_, err := svc.AllocateBalance(context.Background(), owner.ID, member.ID, 30, "")
	require.NoError(t, err)
	createTeamTestAPIKey(t, client, member.ID, "sk-transfer-team-only", BalanceModeTeamOnly)

	oldOwnerContext, err := svc.TransferOwnership(context.Background(), owner.ID, member.ID)
	require.NoError(t, err)
	require.Equal(t, TeamRoleMember, oldOwnerContext.Role)
	require.Equal(t, TeamMembershipActive, oldOwnerContext.MembershipStatus)
	require.Equal(t, member.ID, oldOwnerContext.Team.Owner.UserID)

	newOwnerContext, err := svc.GetContext(context.Background(), member.ID)
	require.NoError(t, err)
	require.Equal(t, TeamRoleOwner, newOwnerContext.Role)
	require.Len(t, newOwnerContext.Team.Members, 1)
	require.Equal(t, owner.ID, newOwnerContext.Team.Members[0].UserID)

	// 资金池 70 + 新主账号个人余额 8 + 已获团队额度 30 = 108 全部成为新主账号余额。
	reloadedOld := reloadTeamTestUser(t, client, owner.ID)
	reloadedNew := reloadTeamTestUser(t, client, member.ID)
	require.InDelta(t, 0, reloadedOld.Balance, teamBalanceEpsilon)
	require.InDelta(t, 0, reloadedOld.TeamBalance, teamBalanceEpsilon)
	require.InDelta(t, 108, reloadedNew.Balance, teamBalanceEpsilon)
	require.InDelta(t, 0, reloadedNew.TeamBalance, teamBalanceEpsilon)
	require.InDelta(t, 108, newOwnerContext.Team.TotalBalance, teamBalanceEpsilon)

	teamEntity, err := client.Team.Query().Where(team.OwnerIDEQ(member.ID)).Only(context.Background())
	require.NoError(t, err)
	memberships, err := client.TeamMembership.Query().Where(teammembership.TeamIDEQ(teamEntity.ID)).All(context.Background())
	require.NoError(t, err)
	require.Len(t, memberships, 1)
	require.Equal(t, owner.ID, memberships[0].UserID)

	key, err := client.APIKey.Query().Where(dbapikey.KeyEQ("sk-transfer-team-only")).Only(context.Background())
	require.NoError(t, err)
	require.Equal(t, BalanceModeTeamFirst, key.BalanceMode)

	transaction, err := client.TeamTransaction.Query().Where(teamtransaction.ActionEQ(TeamActionOwnerTransferred)).Only(context.Background())
	require.NoError(t, err)
	require.InDelta(t, 70, transaction.Amount, teamBalanceEpsilon)
	require.Equal(t, member.ID, *transaction.MemberID)
	require.InDelta(t, 70, transaction.OwnerBalanceBefore, teamBalanceEpsilon)
	require.InDelta(t, 108, transaction.OwnerBalanceAfter, teamBalanceEpsilon)
}

func TestTransferOwnershipRejectsNonMembersAndSelf(t *testing.T) {
	svc, client := newTeamTestService(t)
	owner := createTeamTestUser(t, client, "owner-transfer-guard@example.com", 100, 0)
	member := createTeamTestUser(t, client, "member-transfer-guard@example.com", 0, 0)
	invitee := createTeamTestUser(t, client, "invitee-transfer-guard@example.com", 0, 0)
	outsider := createTeamTestUser(t, client, "outsider-transfer-guard@example.com", 0, 0)
	activateTeamMember(t, svc, owner, member)
	_, err := svc.Invite(context.Background(), owner.ID, invitee.Email)
	require.NoError(t, err)

	_, err = svc.TransferOwnership(context.Background(), owner.ID, owner.ID)
	require.ErrorIs(t, err, ErrTeamOwnerTransferTarget)
	_, err = svc.TransferOwnership(context.Background(), owner.ID, invitee.ID)
	require.ErrorIs(t, err, ErrTeamOwnerTransferTarget)
	_, err = svc.TransferOwnership(context.Background(), owner.ID, outsider.ID)
	require.ErrorIs(t, err, ErrTeamOwnerTransferTarget)
	_, err = svc.TransferOwnership(context.Background(), member.ID, owner.ID)
	require.ErrorIs(t, err, ErrTeamNotFound, "only the current owner can transfer")

	teamEntity, err := client.Team.Query().Only(context.Background())
	require.NoError(t, err)
	require.Equal(t, owner.ID, teamEntity.OwnerID)
	require.InDelta(t, 100, reloadTeamTestUser(t, client, owner.ID).Balance, teamBalanceEpsilon)
}

func TestAdminCanTransferOwnershipEvenWhenTeamSuspended(t *testing.T) {
	svc, client := newTeamTestService(t)
	owner := createTeamTestUser(t, client, "owner-admin-transfer@example.com", 60, 0)
	member := createTeamTestUser(t, client, "member-admin-transfer@example.com", 0, 0)
	activateTeamMember(t, svc, owner, member)
	teamEntity, err := client.Team.Query().Only(context.Background())
	require.NoError(t, err)
	_, err = svc.SetStatus(context.Background(), 999, teamEntity.ID, TeamStatusSuspended, "audit")
	require.NoError(t, err)

	_, err = svc.TransferOwnership(context.Background(), owner.ID, member.ID)
	require.ErrorIs(t, err, ErrTeamSuspended)

	summary, err := svc.AdminTransferOwnership(context.Background(), 999, teamEntity.ID, member.ID)
	require.NoError(t, err)
	require.Equal(t, member.ID, summary.Owner.UserID)
	require.InDelta(t, 60, summary.Owner.Balance, teamBalanceEpsilon)
	require.Len(t, summary.Members, 1)
	require.Equal(t, owner.ID, summary.Members[0].UserID)
	transaction, err := client.TeamTransaction.Query().Where(teamtransaction.ActionEQ(TeamActionAdminOwnerTransfer)).Only(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(999), transaction.OperatorID)
}

func TestTeamMemberUsageSummaryReportsBalancesAndScope(t *testing.T) {
	svc, client := newTeamTestService(t)
	owner := createTeamTestUser(t, client, "owner-summary@example.com", 100, 0)
	member := createTeamTestUser(t, client, "member-summary@example.com", 6, 0)
	outsider := createTeamTestUser(t, client, "outsider-summary@example.com", 0, 0)
	activateTeamMember(t, svc, owner, member)
	_, err := svc.AllocateBalance(context.Background(), owner.ID, member.ID, 14, "")
	require.NoError(t, err)
	_, err = svc.UpdateMemberRemark(context.Background(), owner.ID, member.ID, "设计组")
	require.NoError(t, err)

	summary, err := svc.GetUsageSummary(context.Background(), owner.ID, TeamUsageFilter{MemberID: member.ID})
	require.NoError(t, err)
	require.Equal(t, TeamRoleMember, summary.Role)
	require.Equal(t, "设计组", summary.Remark)
	require.InDelta(t, 6, summary.Balance, teamBalanceEpsilon)
	require.InDelta(t, 14, summary.TeamBalance, teamBalanceEpsilon)
	require.InDelta(t, 20, summary.TotalBalance, teamBalanceEpsilon)
	require.Zero(t, summary.Requests)
	require.Zero(t, summary.Tokens)

	ownerSummary, err := svc.GetUsageSummary(context.Background(), owner.ID, TeamUsageFilter{MemberID: owner.ID})
	require.NoError(t, err)
	require.Equal(t, TeamRoleOwner, ownerSummary.Role)
	require.InDelta(t, 86, ownerSummary.Balance, teamBalanceEpsilon)

	_, err = svc.GetUsageSummary(context.Background(), owner.ID, TeamUsageFilter{MemberID: outsider.ID})
	require.ErrorIs(t, err, ErrTeamMemberNotFound)
	_, err = svc.GetUsageSummary(context.Background(), owner.ID, TeamUsageFilter{})
	require.Error(t, err)
}

func TestBalanceModeHelpers(t *testing.T) {
	require.Equal(t, BalanceModeTeamFirst, NormalizeBalanceMode(""))
	require.Equal(t, BalanceModeTeamFirst, NormalizeBalanceMode("bogus"))
	require.Equal(t, BalanceModePersonalOnly, NormalizeBalanceMode(" personal_only "))
	require.True(t, IsValidBalanceMode(BalanceModeTeamOnly))
	require.False(t, IsValidBalanceMode("team"))
}
