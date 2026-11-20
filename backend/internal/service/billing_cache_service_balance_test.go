//go:build unit

package service

import (
	"context"
	"errors"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type balanceEligibilityCacheStub struct {
	billingCacheWorkerStub

	balance                  float64
	cacheMissAfterInvalidate bool
	invalidated              atomic.Bool
	deductCalls              atomic.Int64
	invalidateCalls          atomic.Int64
}

func (s *balanceEligibilityCacheStub) GetUserBalance(context.Context, int64) (float64, error) {
	if s.cacheMissAfterInvalidate && s.invalidated.Load() {
		return 0, errors.New("cache miss")
	}
	return s.balance, nil
}

func (s *balanceEligibilityCacheStub) DeductUserBalance(context.Context, int64, float64) error {
	s.deductCalls.Add(1)
	return nil
}

func (s *balanceEligibilityCacheStub) InvalidateUserBalance(context.Context, int64) error {
	s.invalidateCalls.Add(1)
	s.invalidated.Store(true)
	return nil
}

func TestCheckBillingEligibility_RejectsBalanceBelowMinimumReserve(t *testing.T) {
	cache := &balanceEligibilityCacheStub{balance: 0.005}
	cfg := &config.Config{}
	cfg.Billing.MinimumBalanceReserve = 0.01
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(svc.Stop)

	err := svc.CheckBillingEligibility(context.Background(), &User{ID: 1}, nil, nil, nil, "")
	require.ErrorIs(t, err, ErrInsufficientBalance)
}

func TestCheckBillingEligibility_AllowsBalanceAtHigherMinimumReserve(t *testing.T) {
	cache := &balanceEligibilityCacheStub{balance: 0.2}
	cfg := &config.Config{}
	cfg.Billing.MinimumBalanceReserve = 0.2
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(svc.Stop)

	err := svc.CheckBillingEligibility(context.Background(), &User{ID: 1}, nil, nil, nil, "")
	require.NoError(t, err)
}

func TestSyncBalanceCacheAfterDeduction_InvalidatesExhaustedBalance(t *testing.T) {
	cache := &balanceEligibilityCacheStub{
		balance:                  0.50,
		cacheMissAfterInvalidate: true,
	}
	userRepo := &balanceLoadUserRepoStub{balance: -0.25}
	cfg := &config.Config{}
	cfg.Billing.MinimumBalanceReserve = 0.01
	svc := NewBillingCacheService(cache, userRepo, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(svc.Stop)

	newBalance := -0.25
	syncBalanceCacheAfterDeduction(context.Background(), &postUsageBillingParams{
		Cost: &CostBreakdown{ActualCost: 0.75},
		User: &User{ID: 1},
	}, &billingDeps{billingCacheService: svc}, &UsageBillingApplyResult{
		NewBalance:         &newBalance,
		BalanceOverdrafted: true,
	})

	require.Equal(t, int64(1), cache.invalidateCalls.Load())
	require.Equal(t, int64(0), cache.deductCalls.Load())

	err := svc.CheckBillingEligibility(context.Background(), &User{ID: 1}, nil, nil, nil, "")
	require.ErrorIs(t, err, ErrInsufficientBalance)
	require.Equal(t, int64(1), userRepo.calls.Load())
}

func TestSyncBalanceCacheAfterDeduction_InvalidatesWhenBalanceFallsBelowReserve(t *testing.T) {
	cache := &balanceEligibilityCacheStub{balance: 0.50}
	cfg := &config.Config{}
	cfg.Billing.MinimumBalanceReserve = 0.01
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(svc.Stop)

	newBalance := 0.005
	syncBalanceCacheAfterDeduction(context.Background(), &postUsageBillingParams{
		Cost: &CostBreakdown{ActualCost: 0.495},
		User: &User{ID: 1},
	}, &billingDeps{billingCacheService: svc}, &UsageBillingApplyResult{NewBalance: &newBalance})

	require.Equal(t, int64(1), cache.invalidateCalls.Load())
	require.Equal(t, int64(0), cache.deductCalls.Load())
}

func TestSyncBalanceCacheAfterDeduction_QueuesDeductWhenBalanceStillEligible(t *testing.T) {
	cache := &balanceEligibilityCacheStub{balance: 1}
	cfg := &config.Config{}
	cfg.Billing.MinimumBalanceReserve = 0.01
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(svc.Stop)

	newBalance := 0.75
	syncBalanceCacheAfterDeduction(context.Background(), &postUsageBillingParams{
		Cost: &CostBreakdown{ActualCost: 0.25},
		User: &User{ID: 1},
	}, &billingDeps{billingCacheService: svc}, &UsageBillingApplyResult{NewBalance: &newBalance})

	require.Equal(t, int64(0), cache.invalidateCalls.Load())
	require.Eventually(t, func() bool {
		return cache.deductCalls.Load() == 1
	}, 2*time.Second, 10*time.Millisecond)
}

func TestBalanceFloorBoundaryAndNonfinite(t *testing.T) {
	for _, tc := range []struct {
		name    string
		balance float64
		allow   bool
	}{
		{"zero", 0, false}, {"floor", .1, false}, {"just_above", .10000001, true}, {"two", 2, true}, {"negative", -.1, false}, {"nan", math.NaN(), false}, {"pos_inf", math.Inf(1), false}, {"neg_inf", math.Inf(-1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewBillingCacheService(&balanceEligibilityCacheStub{balance: tc.balance}, nil, nil, nil, nil, nil, &config.Config{}, nil)
			t.Cleanup(svc.Stop)
			err := svc.CheckBillingEligibility(context.Background(), &User{ID: 1}, nil, nil, nil, "")
			if tc.allow {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrInsufficientBalance)
			}
		})
	}
}
