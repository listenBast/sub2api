//go:build unit

package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	dbteamtx "github.com/Wei-Shaw/sub2api/ent/teamtransaction"
	"github.com/stretchr/testify/require"
)

// setupTeamAdminTest 创建主账号 + 一个管理员候选成员 + 一个普通成员，两人均已加入。
func setupTeamAdminTest(t *testing.T) (svc *TeamService, owner, adm, member, outsider *teamUser) {
	t.Helper()
	s, client := newTeamTestService(t)
	o := createTeamTestUser(t, client, t.Name()+"-owner@example.com", 100, 0)
	a := createTeamTestUser(t, client, t.Name()+"-admin@example.com", 0, 0)
	m := createTeamTestUser(t, client, t.Name()+"-member@example.com", 0, 0)
	x := createTeamTestUser(t, client, t.Name()+"-outsider@example.com", 0, 0)
	activateTeamMember(t, s, o, a)
	_, err := s.Invite(context.Background(), o.ID, m.Email)
	require.NoError(t, err)
	_, err = s.RespondInvitation(context.Background(), m.ID, true)
	require.NoError(t, err)
	return s, &teamUser{o.ID, o.Email}, &teamUser{a.ID, a.Email}, &teamUser{m.ID, m.Email}, &teamUser{x.ID, x.Email}
}

type teamUser struct {
	ID    int64
	Email string
}

// asManager 模拟 handler：每次请求解析当前身份，成功则返回 owner ID 与带操作人的 ctx。
func asManager(t *testing.T, svc *TeamService, userID int64) (int64, context.Context, error) {
	t.Helper()
	manager, err := svc.ResolveManager(context.Background(), userID)
	if err != nil {
		return 0, nil, err
	}
	return manager.OwnerID, WithTeamOperator(context.Background(), userID), nil
}

func TestTeamAdmin_OwnerPromotesAndDemotesTakeEffectImmediately(t *testing.T) {
	svc, owner, adm, member, _ := setupTeamAdminTest(t)
	ctx := context.Background()

	// 普通成员没有管理权限。
	_, _, err := asManager(t, svc, adm.ID)
	require.ErrorIs(t, err, ErrTeamManagerRequired)

	view, err := svc.SetMemberAdmin(ctx, owner.ID, adm.ID, true)
	require.NoError(t, err)
	require.True(t, view.IsAdmin)

	// 下一次请求立即具备管理权限，且 GetContext 返回 admin 角色与完整成员列表。
	ownerID, mctx, err := asManager(t, svc, adm.ID)
	require.NoError(t, err)
	require.Equal(t, owner.ID, ownerID)
	teamCtx, err := svc.GetContext(ctx, adm.ID)
	require.NoError(t, err)
	require.Equal(t, TeamRoleAdmin, teamCtx.Role)
	require.NotNil(t, teamCtx.Team)
	require.Len(t, teamCtx.Team.Members, 2)
	_, err = svc.Invite(mctx, ownerID, "new-member@example.com")
	require.Error(t, err) // 邮箱不存在是业务错误，但不是权限错误
	require.NotErrorIs(t, err, ErrTeamManagerRequired)

	// 撤销后下一次请求立即失效，且不需要重新登录。
	view, err = svc.SetMemberAdmin(ctx, owner.ID, adm.ID, false)
	require.NoError(t, err)
	require.False(t, view.IsAdmin)
	_, _, err = asManager(t, svc, adm.ID)
	require.ErrorIs(t, err, ErrTeamManagerRequired)
	teamCtx, err = svc.GetContext(ctx, adm.ID)
	require.NoError(t, err)
	require.Equal(t, TeamRoleMember, teamCtx.Role)

	// 幂等：重复取消不报错，也不重复写流水。
	_, err = svc.SetMemberAdmin(ctx, owner.ID, adm.ID, false)
	require.NoError(t, err)
	count, err := svc.client.TeamTransaction.Query().Where(dbteamtx.ActionIn(TeamActionAdminGranted, TeamActionAdminRevoked)).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, count)
	_ = member
}

func TestTeamAdmin_OnlyOwnerOrSiteAdminCanSetAdmin(t *testing.T) {
	svc, owner, adm, member, outsider := setupTeamAdminTest(t)
	ctx := context.Background()
	_, err := svc.SetMemberAdmin(ctx, owner.ID, adm.ID, true)
	require.NoError(t, err)

	// 管理员不能任命 / 撤销管理员（SetMemberAdmin 只接受主账号）。
	_, err = svc.SetMemberAdmin(ctx, adm.ID, member.ID, true)
	require.ErrorIs(t, err, ErrTeamNotFound)
	// 非成员、主账号自己、被邀请但未加入的人都不能被设为管理员。
	_, err = svc.SetMemberAdmin(ctx, owner.ID, outsider.ID, true)
	require.ErrorIs(t, err, ErrTeamMemberNotFound)
	_, err = svc.SetMemberAdmin(ctx, owner.ID, owner.ID, true)
	require.ErrorIs(t, err, ErrTeamAdminTarget)
	_, err = svc.Invite(ctx, owner.ID, outsider.Email)
	require.NoError(t, err)
	_, err = svc.SetMemberAdmin(ctx, owner.ID, outsider.ID, true)
	require.ErrorIs(t, err, ErrTeamMemberNotFound)

	// 站点管理员可设置 / 取消任意团队的管理员，流水里记录站点管理员。
	teamCtx, err := svc.GetContext(ctx, owner.ID)
	require.NoError(t, err)
	view, err := svc.AdminSetMemberAdmin(ctx, 999, teamCtx.Team.ID, member.ID, true)
	require.NoError(t, err)
	require.True(t, view.IsAdmin)
	_, err = svc.AdminSetMemberAdmin(ctx, 999, teamCtx.Team.ID, member.ID, false)
	require.NoError(t, err)
	tx, err := svc.client.TeamTransaction.Query().Where(dbteamtx.ActionEQ(TeamActionSiteAdminGranted)).Only(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(999), tx.OperatorID)

	// 站点管理员视角的成员列表带角色。
	detail, err := svc.GetAdminTeam(ctx, teamCtx.Team.ID)
	require.NoError(t, err)
	admins := 0
	for _, m := range detail.Members {
		if m.IsAdmin {
			admins++
			require.Equal(t, adm.ID, m.UserID)
		}
	}
	require.Equal(t, 1, admins)
}

func TestTeamAdmin_CannotTransferOwnershipButCanDoEverythingElse(t *testing.T) {
	svc, owner, adm, member, _ := setupTeamAdminTest(t)
	ctx := context.Background()
	_, err := svc.SetMemberAdmin(ctx, owner.ID, adm.ID, true)
	require.NoError(t, err)

	// 转让主账号只接受真正的主账号：管理员即使通过 ResolveManager 也不能转让。
	_, err = svc.TransferOwnership(ctx, adm.ID, member.ID)
	require.Error(t, err)
	teamCtx, err := svc.GetContext(ctx, owner.ID)
	require.NoError(t, err)
	require.Equal(t, TeamRoleOwner, teamCtx.Role)
	// 管理员也不能把自己转让为主账号。
	_, err = svc.TransferOwnership(ctx, adm.ID, adm.ID)
	require.Error(t, err)

	ownerID, mctx, err := asManager(t, svc, adm.ID)
	require.NoError(t, err)
	require.Equal(t, owner.ID, ownerID)

	// 改名
	renamed, err := svc.Rename(mctx, ownerID, "Renamed By Admin")
	require.NoError(t, err)
	require.Equal(t, "Renamed By Admin", renamed.Team.Name)
	// 划拨额度（来自主账号资金池）
	allocated, err := svc.AllocateBalance(mctx, ownerID, member.ID, 20, "by admin")
	require.NoError(t, err)
	require.InDelta(t, 20, allocated.TeamBalance, teamBalanceEpsilon)
	// 备注与限额
	_, err = svc.UpdateMemberRemark(mctx, ownerID, member.ID, "运营")
	require.NoError(t, err)
	conc := 3
	limited, err := svc.UpdateMemberLimits(mctx, ownerID, member.ID, &conc, nil)
	require.NoError(t, err)
	require.NotNil(t, limited)
	// 视图：成员、流水、汇总
	_, err = svc.GetUsageSummary(mctx, ownerID, TeamUsageFilter{MemberID: member.ID})
	require.NoError(t, err)
	txs, _, err := svc.ListTransactions(mctx, ownerID, 1, 50)
	require.NoError(t, err)
	require.NotEmpty(t, txs)
	userIDs, err := svc.ResolveUsageUserIDs(ctx, adm.ID, 0)
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{owner.ID, adm.ID, member.ID}, userIDs)
	// 成员发起退出，管理员审批通过
	_, err = svc.RequestExit(ctx, member.ID)
	require.NoError(t, err)
	require.NoError(t, svc.ReviewExit(mctx, ownerID, member.ID, true))
	// 流水里的操作人是管理员本人，不是主账号。
	rename, err := svc.client.TeamTransaction.Query().Where(dbteamtx.ActionEQ(TeamActionRenamed)).Only(ctx)
	require.NoError(t, err)
	require.Equal(t, adm.ID, rename.OperatorID)
	alloc, err := svc.client.TeamTransaction.Query().Where(dbteamtx.ActionEQ(TeamActionBalanceAdded)).Only(ctx)
	require.NoError(t, err)
	require.Equal(t, adm.ID, alloc.OperatorID)

	// 邀请 + 移除 + 解散
	extra := createTeamTestUser(t, svc.client, "extra-admin-flow@example.com", 0, 0)
	_, err = svc.Invite(mctx, ownerID, extra.Email)
	require.NoError(t, err)
	_, err = svc.RespondInvitation(ctx, extra.ID, true)
	require.NoError(t, err)
	require.NoError(t, svc.RemoveMember(mctx, ownerID, extra.ID))
	require.NoError(t, svc.Dissolve(mctx, ownerID))
}

func TestTeamAdmin_OwnershipTransferResetsPreviousOwnerAndNewOwnerAdminFlag(t *testing.T) {
	svc, owner, adm, member, _ := setupTeamAdminTest(t)
	ctx := context.Background()
	_, err := svc.SetMemberAdmin(ctx, owner.ID, adm.ID, true)
	require.NoError(t, err)

	// 转让给一个管理员：新主账号不再是“成员管理员”，前主账号成为普通成员。
	_, err = svc.TransferOwnership(ctx, owner.ID, adm.ID)
	require.NoError(t, err)
	_, _, err = asManager(t, svc, owner.ID)
	require.ErrorIs(t, err, ErrTeamManagerRequired)
	manager, err := svc.ResolveManager(ctx, adm.ID)
	require.NoError(t, err)
	require.True(t, manager.IsOwner)
	require.Equal(t, adm.ID, manager.OwnerID)
	teamCtx, err := svc.GetContext(ctx, owner.ID)
	require.NoError(t, err)
	require.Equal(t, TeamRoleMember, teamCtx.Role)
	_ = member
}

func TestTeamAdmin_ResolveManagerRejectsNonManagers(t *testing.T) {
	svc, owner, adm, member, outsider := setupTeamAdminTest(t)
	ctx := context.Background()
	m, err := svc.ResolveManager(ctx, owner.ID)
	require.NoError(t, err)
	require.True(t, m.IsOwner)
	_, err = svc.ResolveManager(ctx, member.ID)
	require.ErrorIs(t, err, ErrTeamManagerRequired)
	_, err = svc.ResolveManager(ctx, outsider.ID)
	require.ErrorIs(t, err, ErrTeamNotFound)

	// 管理员处于“退出待审批”后不再具备管理权限。
	_, err = svc.SetMemberAdmin(ctx, owner.ID, adm.ID, true)
	require.NoError(t, err)
	_, err = svc.RequestExit(ctx, adm.ID)
	require.NoError(t, err)
	_, err = svc.ResolveManager(ctx, adm.ID)
	require.ErrorIs(t, err, ErrTeamManagerRequired)
	// 移除成员后管理员标记随成员记录一并消失；重新邀请加入是普通成员。
	require.NoError(t, svc.ReviewExit(ctx, owner.ID, adm.ID, true))
	_, err = svc.Invite(ctx, owner.ID, adm.Email)
	require.NoError(t, err)
	_, err = svc.RespondInvitation(ctx, adm.ID, true)
	require.NoError(t, err)
	_, err = svc.ResolveManager(ctx, adm.ID)
	require.ErrorIs(t, err, ErrTeamManagerRequired)
}

func TestTeamMemberSpendStats_AggregatesPerMemberOverDateRange(t *testing.T) {
	svc, owner, adm, member, outsider := setupTeamAdminTest(t)
	ctx := context.Background()
	_, err := svc.SetMemberAdmin(ctx, owner.ID, adm.ID, true)
	require.NoError(t, err)
	teamCtx, err := svc.GetContext(ctx, owner.ID)
	require.NoError(t, err)

	base := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	apiKey := createTeamTestAPIKey(t, svc.client, member.ID, "sk-stats-member", BalanceModeTeamFirst)
	adminKey := createTeamTestAPIKey(t, svc.client, adm.ID, "sk-stats-admin", BalanceModeTeamFirst)
	outKey := createTeamTestAPIKey(t, svc.client, outsider.ID, "sk-stats-out", BalanceModeTeamFirst)
	ownerKey := createTeamTestAPIKey(t, svc.client, owner.ID, "sk-stats-owner", BalanceModeTeamFirst)
	account, err := svc.client.Account.Create().SetName("stats-acct").SetPlatform("openai").SetType("api_key").Save(ctx)
	require.NoError(t, err)
	seq := 0
	addLog := func(userID, keyID int64, at time.Time, total, actual float64, in, out int) {
		seq++
		_, err := svc.client.UsageLog.Create().
			SetUserID(userID).SetAPIKeyID(keyID).SetAccountID(account.ID).SetRequestID(fmt.Sprintf("req-%d-%d", userID, seq)).
			SetModel("gpt-test").SetInputTokens(in).SetOutputTokens(out).
			SetTotalCost(total).SetActualCost(actual).SetCreatedAt(at).Save(ctx)
		require.NoError(t, err)
	}
	addLog(member.ID, apiKey.ID, base, 1.0, 0.8, 100, 50)
	addLog(member.ID, apiKey.ID, base.Add(time.Hour), 2.0, 1.6, 10, 5)
	addLog(member.ID, apiKey.ID, base.AddDate(0, 0, 5), 4.0, 3.2, 1, 1) // 区间外
	addLog(adm.ID, adminKey.ID, base, 3.0, 2.5, 20, 20)
	addLog(owner.ID, ownerKey.ID, base, 0.5, 0.5, 1, 1)
	addLog(outsider.ID, outKey.ID, base, 99, 99, 1, 1) // 非团队成员不计入

	start := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	stats, err := svc.GetMemberSpendStats(WithTeamOperator(ctx, adm.ID), owner.ID, &start, &end)
	require.NoError(t, err)
	require.Equal(t, teamCtx.Team.ID, stats.TeamID)
	require.Len(t, stats.Members, 3)
	// 按实际扣费降序：成员 2.4、管理员 2.5 → 管理员第一。
	require.Equal(t, adm.ID, stats.Members[0].UserID)
	require.Equal(t, TeamRoleAdmin, stats.Members[0].Role)
	require.InDelta(t, 2.5, stats.Members[0].ActualCost, teamBalanceEpsilon)
	require.Equal(t, member.ID, stats.Members[1].UserID)
	require.Equal(t, TeamRoleMember, stats.Members[1].Role)
	require.Equal(t, int64(2), stats.Members[1].Requests)
	require.Equal(t, int64(165), stats.Members[1].Tokens)
	require.InDelta(t, 3.0, stats.Members[1].TotalCost, teamBalanceEpsilon)
	require.InDelta(t, 2.4, stats.Members[1].ActualCost, teamBalanceEpsilon)
	require.Equal(t, owner.ID, stats.Members[2].UserID)
	require.Equal(t, TeamRoleOwner, stats.Members[2].Role)
	require.Equal(t, int64(4), stats.Totals.Requests)
	require.InDelta(t, 5.4, stats.Totals.ActualCost, teamBalanceEpsilon)
	require.InDelta(t, 6.5, stats.Totals.TotalCost, teamBalanceEpsilon)

	// 不限区间：包含区间外那一条；站点管理员版本一致。
	all, err := svc.GetAdminMemberSpendStats(ctx, teamCtx.Team.ID, nil, nil)
	require.NoError(t, err)
	require.Equal(t, int64(5), all.Totals.Requests)
	require.InDelta(t, 8.6, all.Totals.ActualCost, teamBalanceEpsilon)

	// 无花费的区间返回全 0 的成员行，而不是缺行。
	emptyStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	emptyEnd := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	empty, err := svc.GetAdminMemberSpendStats(ctx, teamCtx.Team.ID, &emptyStart, &emptyEnd)
	require.NoError(t, err)
	require.Len(t, empty.Members, 3)
	require.Zero(t, empty.Totals.Requests)

	// 非法区间与不存在的团队。
	_, err = svc.GetMemberSpendStats(ctx, owner.ID, &end, &start)
	require.Error(t, err)
	_, err = svc.GetAdminMemberSpendStats(ctx, 424242, nil, nil)
	require.ErrorIs(t, err, ErrTeamNotFound)
}

func TestTeamAdmin_MemberUsageSummaryRoleReflectsAdmin(t *testing.T) {
	svc, owner, adm, _, _ := setupTeamAdminTest(t)
	ctx := context.Background()
	_, err := svc.SetMemberAdmin(ctx, owner.ID, adm.ID, true)
	require.NoError(t, err)
	summary, err := svc.GetUsageSummary(ctx, owner.ID, TeamUsageFilter{MemberID: adm.ID})
	require.NoError(t, err)
	require.Equal(t, TeamRoleAdmin, summary.Role)
}
