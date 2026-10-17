//go:build unit

package service

import (
	"context"
	"testing"

	dbteamtx "github.com/Wei-Shaw/sub2api/ent/teamtransaction"
	"github.com/stretchr/testify/require"
)

func setupAdminTeamBalanceTest(t *testing.T, ownerBalance, memberBalance float64) (*TeamService, int64, int64, int64, func(id int64) (float64, float64)) {
	t.Helper()
	svc, client := newTeamTestService(t)
	owner := createTeamTestUser(t, client, t.Name()+"-owner@example.com", ownerBalance, 0)
	member := createTeamTestUser(t, client, t.Name()+"-member@example.com", memberBalance, 0)
	summary, err := svc.AdminCreateTeam(context.Background(), 999, owner.Email, "Admin Balance Team")
	require.NoError(t, err)
	_, err = svc.AdminAddMember(context.Background(), 999, summary.ID, member.Email, "")
	require.NoError(t, err)
	reload := func(id int64) (float64, float64) {
		u := reloadTeamTestUser(t, client, id)
		return u.Balance, u.TeamBalance
	}
	return svc, summary.ID, owner.ID, member.ID, reload
}

func TestAdminAdjustMemberTeamBalance_OwnerPoolKeepsTeamTotal(t *testing.T) {
	svc, teamID, ownerID, memberID, reload := setupAdminTeamBalanceTest(t, 100, 0)
	ctx := context.Background()

	view, err := svc.AdminAdjustMemberTeamBalance(ctx, 999, teamID, memberID, TeamAdminBalanceAdjustment{Operation: "add", Amount: 15, Note: "补额度"})
	require.NoError(t, err)
	require.InDelta(t, 15, view.TeamBalance, teamBalanceEpsilon)
	ownerBal, _ := reload(ownerID)
	memberBal, memberTeam := reload(memberID)
	require.InDelta(t, 85, ownerBal, teamBalanceEpsilon)
	require.InDelta(t, 15, memberTeam, teamBalanceEpsilon)
	require.InDelta(t, 0, memberBal, teamBalanceEpsilon, "personal balance untouched")

	_, err = svc.AdminAdjustMemberTeamBalance(ctx, 999, teamID, memberID, TeamAdminBalanceAdjustment{Operation: "subtract", Amount: 5})
	require.NoError(t, err)
	ownerBal, _ = reload(ownerID)
	_, memberTeam = reload(memberID)
	require.InDelta(t, 90, ownerBal, teamBalanceEpsilon)
	require.InDelta(t, 10, memberTeam, teamBalanceEpsilon)

	_, err = svc.AdminAdjustMemberTeamBalance(ctx, 999, teamID, memberID, TeamAdminBalanceAdjustment{Operation: "set", Amount: 30, Source: TeamAdminBalanceSourceOwnerPool})
	require.NoError(t, err)
	ownerBal, _ = reload(ownerID)
	_, memberTeam = reload(memberID)
	require.InDelta(t, 70, ownerBal, teamBalanceEpsilon)
	require.InDelta(t, 30, memberTeam, teamBalanceEpsilon)

	txs, err := svc.client.TeamTransaction.Query().Where(dbteamtx.TeamIDEQ(teamID), dbteamtx.ActionIn(TeamActionAdminBalanceAllocated, TeamActionAdminBalanceRecovered)).All(ctx)
	require.NoError(t, err)
	require.Len(t, txs, 3)
	for _, tx := range txs {
		require.Equal(t, int64(999), tx.OperatorID)
		require.NotNil(t, tx.MemberID)
		require.Equal(t, memberID, *tx.MemberID)
	}
}

func TestAdminAdjustMemberTeamBalance_OwnerPoolRejectsInsufficientOwnerBalance(t *testing.T) {
	svc, teamID, ownerID, memberID, reload := setupAdminTeamBalanceTest(t, 10, 0)
	_, err := svc.AdminAdjustMemberTeamBalance(context.Background(), 999, teamID, memberID, TeamAdminBalanceAdjustment{Operation: "add", Amount: 15})
	require.ErrorIs(t, err, ErrTeamInsufficientBalance)
	ownerBal, _ := reload(ownerID)
	_, memberTeam := reload(memberID)
	require.InDelta(t, 10, ownerBal, teamBalanceEpsilon)
	require.InDelta(t, 0, memberTeam, teamBalanceEpsilon)
}

func TestAdminAdjustMemberTeamBalance_SystemSourceLeavesOwnerUntouched(t *testing.T) {
	svc, teamID, ownerID, memberID, reload := setupAdminTeamBalanceTest(t, 0, 0)
	ctx := context.Background()

	_, err := svc.AdminAdjustMemberTeamBalance(ctx, 999, teamID, memberID, TeamAdminBalanceAdjustment{Operation: "add", Amount: 15, Source: TeamAdminBalanceSourceSystem})
	require.NoError(t, err)
	ownerBal, _ := reload(ownerID)
	_, memberTeam := reload(memberID)
	require.InDelta(t, 0, ownerBal, teamBalanceEpsilon)
	require.InDelta(t, 15, memberTeam, teamBalanceEpsilon)

	_, err = svc.AdminAdjustMemberTeamBalance(ctx, 999, teamID, memberID, TeamAdminBalanceAdjustment{Operation: "set", Amount: 4, Source: TeamAdminBalanceSourceSystem})
	require.NoError(t, err)
	ownerBal, _ = reload(ownerID)
	_, memberTeam = reload(memberID)
	require.InDelta(t, 0, ownerBal, teamBalanceEpsilon)
	require.InDelta(t, 4, memberTeam, teamBalanceEpsilon)

	tx, err := svc.client.TeamTransaction.Query().Where(dbteamtx.TeamIDEQ(teamID), dbteamtx.ActionEQ(TeamActionAdminBalanceAdjusted)).Order(dbteamtx.ByID()).All(ctx)
	require.NoError(t, err)
	require.Len(t, tx, 2)
	require.InDelta(t, -11, tx[1].Amount, teamBalanceEpsilon)
	require.InDelta(t, 15, *tx[1].MemberBalanceBefore, teamBalanceEpsilon)
	require.InDelta(t, 4, *tx[1].MemberBalanceAfter, teamBalanceEpsilon)
}

func TestAdminAdjustMemberTeamBalance_Validation(t *testing.T) {
	svc, teamID, ownerID, memberID, reload := setupAdminTeamBalanceTest(t, 100, 0)
	ctx := context.Background()

	_, err := svc.AdminAdjustMemberTeamBalance(ctx, 999, teamID, memberID, TeamAdminBalanceAdjustment{Operation: "subtract", Amount: 1})
	require.ErrorIs(t, err, ErrTeamMemberInsufficientBalance, "team balance cannot go negative")
	_, err = svc.AdminAdjustMemberTeamBalance(ctx, 999, teamID, memberID, TeamAdminBalanceAdjustment{Operation: "add", Amount: 0})
	require.Error(t, err)
	_, err = svc.AdminAdjustMemberTeamBalance(ctx, 999, teamID, memberID, TeamAdminBalanceAdjustment{Operation: "set", Amount: -1})
	require.Error(t, err)
	_, err = svc.AdminAdjustMemberTeamBalance(ctx, 999, teamID, memberID, TeamAdminBalanceAdjustment{Operation: "multiply", Amount: 1})
	require.Error(t, err)
	_, err = svc.AdminAdjustMemberTeamBalance(ctx, 999, teamID, memberID, TeamAdminBalanceAdjustment{Operation: "add", Amount: 1, Source: "bank"})
	require.Error(t, err)

	// 主账号本人不是成员，不能调整团队额度。
	_, err = svc.AdminAdjustMemberTeamBalance(ctx, 999, teamID, ownerID, TeamAdminBalanceAdjustment{Operation: "add", Amount: 1})
	require.ErrorIs(t, err, ErrTeamMemberNotFound)
	// 其他团队 / 不存在的团队。
	_, err = svc.AdminAdjustMemberTeamBalance(ctx, 999, teamID+100, memberID, TeamAdminBalanceAdjustment{Operation: "add", Amount: 1})
	require.ErrorIs(t, err, ErrTeamNotFound)

	// set 为当前值：无变化、不写流水。
	view, err := svc.AdminAdjustMemberTeamBalance(ctx, 999, teamID, memberID, TeamAdminBalanceAdjustment{Operation: "set", Amount: 0})
	require.NoError(t, err)
	require.InDelta(t, 0, view.TeamBalance, teamBalanceEpsilon)
	count, err := svc.client.TeamTransaction.Query().Where(dbteamtx.ActionHasPrefix("admin_balance_")).Count(ctx)
	require.NoError(t, err)
	require.Zero(t, count)

	ownerBal, _ := reload(ownerID)
	require.InDelta(t, 100, ownerBal, teamBalanceEpsilon)
}

func TestAdminAdjustMemberTeamBalance_SetCorrectsOverdraftAndInvalidatesCaches(t *testing.T) {
	svc, teamID, _, memberID, reload := setupAdminTeamBalanceTest(t, 100, 0)
	ctx := context.Background()
	inv := &teamAuthInvalidatorSpy{}
	svc.authCacheInvalidator = inv
	// 计费允许透支：模拟团队额度已为负。
	_, err := svc.client.User.UpdateOneID(memberID).SetTeamBalance(-2).Save(ctx)
	require.NoError(t, err)

	_, err = svc.AdminAdjustMemberTeamBalance(ctx, 999, teamID, memberID, TeamAdminBalanceAdjustment{Operation: "set", Amount: 15, Source: TeamAdminBalanceSourceSystem})
	require.NoError(t, err)
	_, memberTeam := reload(memberID)
	require.InDelta(t, 15, memberTeam, teamBalanceEpsilon)
	require.Contains(t, inv.userIDs, memberID)
}

type teamAuthInvalidatorSpy struct{ userIDs []int64 }

func (s *teamAuthInvalidatorSpy) InvalidateAuthCacheByKey(context.Context, string)     {}
func (s *teamAuthInvalidatorSpy) InvalidateAuthCacheByGroupID(context.Context, int64) {}
func (s *teamAuthInvalidatorSpy) InvalidateAuthCacheByUserID(_ context.Context, id int64) {
	s.userIDs = append(s.userIDs, id)
}
