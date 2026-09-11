//go:build unit

package service

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// teamBalanceUserRepoStub 返回带团队额度的用户，用于验证团队模式下的余额资格判定（fork）。
type teamBalanceUserRepoStub struct {
	mockUserRepo
	calls       atomic.Int64
	balance     float64
	teamBalance float64
}

func (s *teamBalanceUserRepoStub) GetByID(_ context.Context, id int64) (*User, error) {
	s.calls.Add(1)
	return &User{ID: id, Balance: s.balance, TeamBalance: s.teamBalance}, nil
}

func newTeamBalanceEligibilityService(t *testing.T, cachedBalance float64, repo UserRepository) *BillingCacheService {
	t.Helper()
	cache := &balanceEligibilityCacheStub{balance: cachedBalance}
	cfg := &config.Config{}
	cfg.Billing.MinimumBalanceReserve = 0.01
	svc := NewBillingCacheService(cache, repo, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(svc.Stop)
	return svc
}

func TestCheckBillingEligibility_TeamQuotaCoversEmptyPersonalBalance(t *testing.T) {
	repo := &teamBalanceUserRepoStub{balance: 0, teamBalance: 5}
	svc := newTeamBalanceEligibilityService(t, 0, repo)
	user := &User{ID: 1, TeamBalance: 5}

	err := svc.CheckBillingEligibility(context.Background(), user, &APIKey{BalanceMode: BalanceModeTeamFirst}, nil, nil, "")
	require.NoError(t, err)
	require.Equal(t, int64(1), repo.calls.Load(), "team quota is read from the database once")

	err = svc.CheckBillingEligibility(context.Background(), user, &APIKey{BalanceMode: BalanceModePersonalFirst}, nil, nil, "")
	require.NoError(t, err)
}

func TestCheckBillingEligibility_PersonalOnlyIgnoresTeamQuota(t *testing.T) {
	repo := &teamBalanceUserRepoStub{balance: 0, teamBalance: 5}
	svc := newTeamBalanceEligibilityService(t, 0, repo)

	err := svc.CheckBillingEligibility(context.Background(), &User{ID: 1, TeamBalance: 5}, &APIKey{BalanceMode: BalanceModePersonalOnly}, nil, nil, "")
	require.ErrorIs(t, err, ErrInsufficientBalance)
	require.Zero(t, repo.calls.Load(), "personal_only never touches the team quota")
}

func TestCheckBillingEligibility_TeamOnlyUsesFreshTeamQuota(t *testing.T) {
	repo := &teamBalanceUserRepoStub{balance: 50, teamBalance: 0}
	svc := newTeamBalanceEligibilityService(t, 50, repo)

	// 快照里还剩 3 的团队额度，但数据库里已经被消费为 0：以数据库为准。
	err := svc.CheckBillingEligibility(context.Background(), &User{ID: 1, TeamBalance: 3}, &APIKey{BalanceMode: BalanceModeTeamOnly}, nil, nil, "")
	require.ErrorIs(t, err, ErrInsufficientBalance)
	require.Equal(t, int64(1), repo.calls.Load())

	repo.teamBalance = 2
	err = svc.CheckBillingEligibility(context.Background(), &User{ID: 1, TeamBalance: 3}, &APIKey{BalanceMode: BalanceModeTeamOnly}, nil, nil, "")
	require.NoError(t, err)
}

func TestCheckBillingEligibility_SkipsTeamLookupWhenSnapshotHasNoQuota(t *testing.T) {
	repo := &teamBalanceUserRepoStub{balance: 0, teamBalance: 0}
	svc := newTeamBalanceEligibilityService(t, 0, repo)

	err := svc.CheckBillingEligibility(context.Background(), &User{ID: 1}, &APIKey{BalanceMode: BalanceModeTeamFirst}, nil, nil, "")
	require.ErrorIs(t, err, ErrInsufficientBalance)
	require.Zero(t, repo.calls.Load(), "snapshot without team quota means no team quota can exist")
}

func TestSyncBalanceCacheAfterDeduction_OnlyDeductsPersonalShareFromCache(t *testing.T) {
	cache := &balanceEligibilityCacheStub{balance: 10}
	cfg := &config.Config{}
	cfg.Billing.MinimumBalanceReserve = 0.01
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(svc.Stop)

	newBalance := 10.0
	// 全部从团队额度扣除：个人余额缓存不应被扣减。
	syncBalanceCacheAfterDeduction(context.Background(), &postUsageBillingParams{
		Cost: &CostBreakdown{ActualCost: 0.25},
		User: &User{ID: 1},
	}, &billingDeps{billingCacheService: svc}, &UsageBillingApplyResult{
		NewBalance:          &newBalance,
		PersonalBalanceCost: 0,
		TeamBalanceCost:     0.25,
	})
	require.Equal(t, int64(0), cache.invalidateCalls.Load())
	require.Equal(t, int64(0), cache.deductCalls.Load())
}
