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

func newTeamInflightSvc(t *testing.T, personal float64, repo UserRepository) (*BillingCacheService, *memInflightCache) {
	t.Helper()
	cache := newMemInflightCache(personal)
	cfg := &config.Config{}
	cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, TTLSeconds: 60}
	svc := NewBillingCacheService(cache, repo, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(svc.Stop)
	return svc, cache
}

// 回归：个人余额 0、团队分配额度 15、Key 未设置扣费方式（默认 team_first）。
// 上游合入的在途预留只比较个人余额，导致第二个并发请求被判余额不足（403）。
func TestTeamFirstDefault_EmptyPersonalBalance_TeamQuotaAllowsConcurrentRequests(t *testing.T) {
	repo := &teamBalanceUserRepoStub{balance: 0, teamBalance: 15}
	svc, cache := newTeamInflightSvc(t, 0, repo)
	user := &User{ID: 7, Balance: 0, TeamBalance: 15}
	apiKey := &APIKey{ID: 70, User: user} // BalanceMode 为空 = team_first

	require.NoError(t, svc.CheckBillingEligibility(context.Background(), user, apiKey, nil, nil, ""))

	first, err := svc.ReserveInflightForAPIKey(context.Background(), apiKey, nil, 1.0)
	require.NoError(t, err)
	require.NotNil(t, first)
	second, err := svc.ReserveInflightForAPIKey(context.Background(), apiKey, nil, 1.0)
	require.NoError(t, err, "concurrent request must be covered by the team quota")
	require.NotNil(t, second)
	require.Equal(t, 2, cache.count())

	// 在途合计超过 个人 + 团队 时仍然拒绝。
	_, err = svc.ReserveInflightForAPIKey(context.Background(), apiKey, nil, 14.0)
	require.ErrorIs(t, err, ErrInsufficientBalance)

	first.HandlerDone()
	second.HandlerDone()
	require.Equal(t, 0, cache.count())
}

func TestInflightReservation_PersonalOnlyIgnoresTeamQuota(t *testing.T) {
	repo := &teamBalanceUserRepoStub{balance: 0, teamBalance: 15}
	svc, _ := newTeamInflightSvc(t, 0, repo)
	user := &User{ID: 8, TeamBalance: 15}
	apiKey := &APIKey{ID: 80, User: user, BalanceMode: BalanceModePersonalOnly}

	first, err := svc.ReserveInflightForAPIKey(context.Background(), apiKey, nil, 1.0)
	require.NoError(t, err) // 无在途预留时与旧行为一致直接放行（预检已拦截余额为 0 的情况）
	_, err = svc.ReserveInflightForAPIKey(context.Background(), apiKey, nil, 1.0)
	require.ErrorIs(t, err, ErrInsufficientBalance)
	require.Zero(t, repo.calls.Load(), "personal_only never touches the team quota")
	first.HandlerDone()
}

func TestReserveFixedCharge_TeamQuotaCoversEmptyPersonalBalance(t *testing.T) {
	repo := &teamBalanceUserRepoStub{balance: 0, teamBalance: 15}
	svc, _ := newTeamInflightSvc(t, 0, repo)
	user := &User{ID: 9, TeamBalance: 15}

	res, err := svc.ReserveFixedCharge(context.Background(), user, &APIKey{ID: 90, User: user}, nil, nil, 2.0)
	require.NoError(t, err)
	res.HandlerDone()

	_, err = svc.ReserveFixedCharge(context.Background(), user, &APIKey{ID: 91, User: user, BalanceMode: BalanceModePersonalOnly}, nil, nil, 2.0)
	require.ErrorIs(t, err, ErrInsufficientBalance)
}
