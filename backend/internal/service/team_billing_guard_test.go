//go:build unit

package service

// 团队计费守护测试（sub2api-custom）。每次合并上游后都要跑：
//
//	cd backend && go test -tags=unit ./internal/service/ -run 'TestTeamBillingGuard_|TestTeamNoOverdraft_' -count=1
//
// 场景固定为线上出过 403 的组合：团队成员个人余额 0、团队额度 15、Key 未设置扣费方式（默认 team_first）。
// 覆盖 预检 → 并发在途预留 → 事务扣费 / legacy 扣费 → 缓存与认证快照失效 这条完整链路，
// 并验证 personal_only 完全不碰团队额度。

import (
	"context"
	"math"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// teamGuardLedger 是一个按扣费方式拆分扣费的内存账本，同时充当 UserRepository（含 BalanceModeDeductor）
// 与 UsageBillingRepository。拆分规则与 repository.teamUsageBillingDeductSQL 一致（团队成员，不透支）。
type teamGuardLedger struct {
	mockUserRepo
	UsageBillingRepository // 只实现 Apply
	mu                     sync.Mutex
	personal               float64
	team                   float64
	getByID                atomic.Int64
	cmds                   []*UsageBillingCommand
	modeCalls              []string
}

func (l *teamGuardLedger) GetByID(_ context.Context, id int64) (*User, error) {
	l.getByID.Add(1)
	l.mu.Lock()
	defer l.mu.Unlock()
	return &User{ID: id, Balance: l.personal, TeamBalance: l.team}, nil
}

func (l *teamGuardLedger) split(amount float64, mode string) (personalPart, teamPart, uncovered float64) {
	// 不透支：两个资金池都只扣到 0，已为负数的旧余额按 0 处理。
	personalAvail, teamAvail := math.Max(l.personal, 0), math.Max(l.team, 0)
	switch NormalizeBalanceMode(mode) {
	case BalanceModePersonalOnly:
		personalPart = math.Min(personalAvail, amount)
	case BalanceModeTeamOnly:
		teamPart = math.Min(teamAvail, amount)
	case BalanceModePersonalFirst:
		personalPart = math.Min(personalAvail, amount)
		teamPart = math.Min(teamAvail, amount-personalPart)
	default: // team_first
		teamPart = math.Min(teamAvail, amount)
		personalPart = math.Min(personalAvail, amount-teamPart)
	}
	uncovered = amount - personalPart - teamPart
	l.personal -= personalPart
	l.team -= teamPart
	return personalPart, teamPart, uncovered
}

func (l *teamGuardLedger) DeductBalanceByMode(_ context.Context, _ int64, amount float64, mode string) (BalanceDeductionResult, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.modeCalls = append(l.modeCalls, mode)
	personal, team, uncovered := l.split(amount, mode)
	return BalanceDeductionResult{Personal: personal, Team: team, Uncovered: uncovered}, nil
}

func (l *teamGuardLedger) Apply(_ context.Context, cmd *UsageBillingCommand) (*UsageBillingApplyResult, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cmds = append(l.cmds, cmd)
	result := &UsageBillingApplyResult{Applied: true}
	if cmd.BalanceCost > 0 {
		personal, team, uncovered := l.split(cmd.BalanceCost, cmd.BalanceMode)
		newBalance, newTeam := l.personal, l.team
		result.NewBalance = &newBalance
		result.NewTeamBalance = &newTeam
		result.PersonalBalanceCost = personal
		result.TeamBalanceCost = team
		result.UncoveredBalanceCost = uncovered
		result.BalanceOverdrafted = uncovered > 0
	}
	return result, nil
}

func (l *teamGuardLedger) balances() (float64, float64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.personal, l.team
}

// teamGuardCache：在途预留走 memInflightCache，个人余额缓存读账本，记录缓存扣减 / 失效次数。
type teamGuardCache struct {
	*memInflightCache
	ledger      *teamGuardLedger
	invalidated atomic.Int64
}

func (c *teamGuardCache) GetUserBalance(context.Context, int64) (float64, error) {
	personal, _ := c.ledger.balances()
	return personal, nil
}

func (c *teamGuardCache) InvalidateUserBalance(context.Context, int64) error {
	c.invalidated.Add(1)
	return nil
}

// teamGuardAPIKeyService 记录按用户失效认证快照的调用。
type teamGuardAPIKeyService struct {
	apiKeyQuotaUpdaterStub
	byUser atomic.Int64
}

func (s *teamGuardAPIKeyService) InvalidateAuthCacheByUserID(context.Context, int64) { s.byUser.Add(1) }

type teamGuardFixture struct {
	svc    *BillingCacheService
	cache  *teamGuardCache
	ledger *teamGuardLedger
	keys   *teamGuardAPIKeyService
	user   *User
	apiKey *APIKey
}

func newTeamGuardFixture(t *testing.T, mode string) *teamGuardFixture {
	t.Helper()
	ledger := &teamGuardLedger{personal: 0, team: 15}
	cache := &teamGuardCache{memInflightCache: newMemInflightCache(0), ledger: ledger}
	cfg := &config.Config{}
	cfg.Billing.MinimumBalanceReserve = 0.01
	cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, TTLSeconds: 60}
	svc := NewBillingCacheService(cache, ledger, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(svc.Stop)
	user := &User{ID: 7, Balance: 0, TeamBalance: 15}
	return &teamGuardFixture{
		svc: svc, cache: cache, ledger: ledger, keys: &teamGuardAPIKeyService{}, user: user,
		apiKey: &APIKey{ID: 70, Key: "sk-team-guard", User: user, BalanceMode: mode},
	}
}

func (f *teamGuardFixture) billingParams(cost float64) *postUsageBillingParams {
	return &postUsageBillingParams{
		Cost:          &CostBreakdown{ActualCost: cost, TotalCost: cost},
		User:          f.user,
		APIKey:        f.apiKey,
		Account:       &Account{ID: 9, Type: AccountTypeOAuth},
		APIKeyService: f.keys,
	}
}

func (f *teamGuardFixture) deps() *billingDeps {
	return &billingDeps{userRepo: f.ledger, billingCacheService: f.svc, deferredService: &DeferredService{}}
}

func TestTeamBillingGuard_TeamFirstDefault_EligibilityPasses(t *testing.T) {
	f := newTeamGuardFixture(t, "") // 未设置 = team_first
	require.NoError(t, f.svc.CheckBillingEligibility(context.Background(), f.user, f.apiKey, nil, nil, ""))
	require.NoError(t, f.svc.CheckBillingEligibility(context.Background(), f.user, &APIKey{ID: 71, User: f.user, BalanceMode: BalanceModePersonalFirst}, nil, nil, ""))
	require.NoError(t, f.svc.CheckBillingEligibility(context.Background(), f.user, &APIKey{ID: 72, User: f.user, BalanceMode: BalanceModeTeamOnly}, nil, nil, ""))
}

func TestTeamBillingGuard_TeamFirstDefault_ConcurrentInflightReservationsPass(t *testing.T) {
	f := newTeamGuardFixture(t, "")
	const workers = 10 // 10 × 1.0 ≤ 个人 0 + 团队 15
	var wg sync.WaitGroup
	reservations := make([]*InflightReservation, workers)
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			reservations[i], errs[i] = f.svc.ReserveInflightForAPIKey(context.Background(), f.apiKey, nil, 1.0)
		}(i)
	}
	wg.Wait()
	for i := 0; i < workers; i++ {
		require.NoError(t, errs[i], "concurrent request %d must be covered by the team quota", i)
		require.NotNil(t, reservations[i])
	}
	require.Equal(t, workers, f.cache.count())

	// 超过 个人 + 团队 的在途合计仍然拒绝。
	_, err := f.svc.ReserveInflightForAPIKey(context.Background(), f.apiKey, nil, 6.0)
	require.ErrorIs(t, err, ErrInsufficientBalance)

	// 固定计费（图片等）同样按团队额度预检。
	fixed, err := f.svc.ReserveFixedCharge(context.Background(), f.user, f.apiKey, nil, nil, 2.0)
	require.NoError(t, err)

	for _, r := range reservations {
		r.HandlerDone()
	}
	if fixed != nil {
		fixed.HandlerDone()
	}
	require.Equal(t, 0, f.cache.count())
}

func TestTeamBillingGuard_TeamFirstDefault_DeductionTakesTeamQuota(t *testing.T) {
	f := newTeamGuardFixture(t, "")

	// 事务扣费路径。
	applied, err := applyUsageBilling(context.Background(), "guard-req-1", nil, f.billingParams(3), f.deps(), f.ledger)
	require.NoError(t, err)
	require.True(t, applied)
	require.Len(t, f.ledger.cmds, 1)
	require.Equal(t, BalanceModeTeamFirst, f.ledger.cmds[0].BalanceMode)
	personal, team := f.ledger.balances()
	require.InDelta(t, 0, personal, 1e-9)
	require.InDelta(t, 12, team, 1e-9)
	require.Equal(t, int32(0), f.cache.deducts.Load(), "team-funded charge must not touch the personal balance cache")
	require.Equal(t, int64(1), f.keys.byUser.Load(), "team-funded charge rebuilds the member's auth snapshots")

	// legacy 扣费路径（无 request id / 无事务仓储）同样按扣费方式扣团队额度。
	applied, err = applyUsageBilling(context.Background(), "", nil, f.billingParams(2), f.deps(), nil)
	require.NoError(t, err)
	require.True(t, applied)
	require.Equal(t, []string{BalanceModeTeamFirst}, f.ledger.modeCalls)
	personal, team = f.ledger.balances()
	require.InDelta(t, 0, personal, 1e-9)
	require.InDelta(t, 10, team, 1e-9)
	require.Equal(t, int64(2), f.keys.byUser.Load())

	// 预检读到最新团队额度后仍放行。
	f.user.TeamBalance = team
	require.NoError(t, f.svc.CheckBillingEligibility(context.Background(), f.user, f.apiKey, nil, nil, ""))
}

func TestTeamBillingGuard_PersonalOnlyIgnoresTeamQuota(t *testing.T) {
	f := newTeamGuardFixture(t, BalanceModePersonalOnly)

	err := f.svc.CheckBillingEligibility(context.Background(), f.user, f.apiKey, nil, nil, "")
	require.ErrorIs(t, err, ErrInsufficientBalance)

	first, err := f.svc.ReserveInflightForAPIKey(context.Background(), f.apiKey, nil, 1.0)
	require.NoError(t, err) // 无在途时放行（与上游一致，预检已拦截）
	_, err = f.svc.ReserveInflightForAPIKey(context.Background(), f.apiKey, nil, 1.0)
	require.ErrorIs(t, err, ErrInsufficientBalance)
	first.HandlerDone()

	_, err = f.svc.ReserveFixedCharge(context.Background(), f.user, f.apiKey, nil, nil, 1.0)
	require.ErrorIs(t, err, ErrInsufficientBalance)
	require.Zero(t, f.ledger.getByID.Load(), "personal_only never reads the team quota")

	applied, err := applyUsageBilling(context.Background(), "guard-req-po", nil, f.billingParams(3), f.deps(), f.ledger)
	require.NoError(t, err)
	require.True(t, applied)
	require.Equal(t, BalanceModePersonalOnly, f.ledger.cmds[0].BalanceMode)
	personal, team := f.ledger.balances()
	require.InDelta(t, 0, personal, 1e-9, "no overdraft: personal balance never goes negative")
	require.InDelta(t, 15, team, 1e-9, "personal_only must not spend the team quota")
	require.Zero(t, f.keys.byUser.Load())
}

// legacy 扣费路径的失效顺序必须与拆分前一致：先失效个人余额缓存，再按用户失效认证快照。
type teamGuardOrderCache struct {
	*teamGuardCache
	order *[]string
}

func (c *teamGuardOrderCache) InvalidateUserBalance(context.Context, int64) error {
	*c.order = append(*c.order, "balance")
	return nil
}

type teamGuardOrderKeys struct {
	apiKeyQuotaUpdaterStub
	order *[]string
}

func (k *teamGuardOrderKeys) InvalidateAuthCacheByUserID(context.Context, int64) {
	*k.order = append(*k.order, "auth")
}

func TestTeamBillingGuard_LegacyDeductionInvalidationOrder(t *testing.T) {
	f := newTeamGuardFixture(t, "")
	var order []string
	cache := &teamGuardOrderCache{teamGuardCache: f.cache, order: &order}
	cfg := &config.Config{}
	svc := NewBillingCacheService(cache, f.ledger, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(svc.Stop)
	p := f.billingParams(2)
	p.APIKeyService = &teamGuardOrderKeys{order: &order}
	postUsageBilling(context.Background(), p, &billingDeps{userRepo: f.ledger, billingCacheService: svc, deferredService: &DeferredService{}})
	require.Equal(t, []string{"balance", "auth"}, order)
	_, team := f.ledger.balances()
	require.InDelta(t, 13, team, 1e-9)
}
