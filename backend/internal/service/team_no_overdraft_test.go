//go:build unit

package service

// 不透支守护测试（sub2api-custom: team billing）：用量扣费永远不会把个人余额或团队额度扣成负数，
// 不够的部分不扣（由站点承担），并记一条 billing.uncovered_balance_charge 日志。
// SQL 本身的逐模式规则见 repository/team_no_overdraft_*_test.go（含真实 Postgres 集成测试）。

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// captureSlog 把默认 slog 输出接到内存里，返回按 msg 过滤后的 JSON 记录。
func captureSlog(t *testing.T) func(msg string) []map[string]any {
	t.Helper()
	buf := &syncBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return func(msg string) []map[string]any {
		var out []map[string]any
		for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
			if line == "" {
				continue
			}
			var rec map[string]any
			require.NoError(t, json.Unmarshal([]byte(line), &rec))
			if rec["msg"] == msg {
				out = append(out, rec)
			}
		}
		return out
	}
}

const uncoveredLogMsg = "billing.uncovered_balance_charge"

func TestTeamNoOverdraft_TeamFirstShortClampsAndLogs(t *testing.T) {
	logs := captureSlog(t)
	f := newTeamGuardFixture(t, "")
	f.ledger.team = 2
	f.user.TeamBalance = 2

	applied, err := applyUsageBilling(context.Background(), "guard-no-od-1", nil, f.billingParams(3), f.deps(), f.ledger)
	require.NoError(t, err)
	require.True(t, applied)
	personal, team := f.ledger.balances()
	require.InDelta(t, 0, personal, 1e-9, "personal balance must not go negative")
	require.InDelta(t, 0, team, 1e-9, "team quota must not go negative")
	require.Equal(t, int32(0), f.cache.deducts.Load())

	recs := logs(uncoveredLogMsg)
	require.Len(t, recs, 1)
	rec := recs[0]
	require.Equal(t, "usage_billing", rec["source"])
	require.Equal(t, float64(7), rec["user_id"])
	require.Equal(t, float64(70), rec["api_key_id"])
	require.Equal(t, "guard-no-od-1", rec["request_id"])
	require.Equal(t, BalanceModeTeamFirst, rec["balance_mode"])
	require.InDelta(t, 3, rec["actual_cost"], 1e-9)
	require.InDelta(t, 0, rec["personal_charged"], 1e-9)
	require.InDelta(t, 2, rec["team_charged"], 1e-9)
	require.InDelta(t, 1, rec["uncovered"], 1e-9)

	// 资金耗尽后预检拦截后续请求。
	f.user.TeamBalance = 0
	require.ErrorIs(t, f.svc.CheckBillingEligibility(context.Background(), f.user, f.apiKey, nil, nil, ""), ErrInsufficientBalance)
}

func TestTeamNoOverdraft_FullyCoveredChargeDoesNotLog(t *testing.T) {
	logs := captureSlog(t)
	f := newTeamGuardFixture(t, "")
	applied, err := applyUsageBilling(context.Background(), "guard-no-od-2", nil, f.billingParams(3), f.deps(), f.ledger)
	require.NoError(t, err)
	require.True(t, applied)
	require.Empty(t, logs(uncoveredLogMsg))
}

// team_only 且团队额度为 0：个人余额 5 完全不动，余额缓存也不能被扣（修复前会按全额扣缓存）。
func TestTeamNoOverdraft_TeamOnlyUncoveredDoesNotDeductPersonalCache(t *testing.T) {
	logs := captureSlog(t)
	f := newTeamGuardFixture(t, BalanceModeTeamOnly)
	f.ledger.personal = 5
	f.ledger.team = 0
	f.user.Balance = 5
	f.user.TeamBalance = 0

	applied, err := applyUsageBilling(context.Background(), "guard-no-od-3", nil, f.billingParams(3), f.deps(), f.ledger)
	require.NoError(t, err)
	require.True(t, applied)
	personal, team := f.ledger.balances()
	require.InDelta(t, 5, personal, 1e-9)
	require.InDelta(t, 0, team, 1e-9)
	time.Sleep(50 * time.Millisecond) // 给异步缓存队列一点时间，确认没有扣减
	require.Equal(t, int32(0), f.cache.deducts.Load(), "nothing was charged to the personal wallet, so the cache must not move")
	require.Len(t, logs(uncoveredLogMsg), 1)
}

func TestTeamNoOverdraft_CachePersonalCostUsesSplitEvenWhenNothingCharged(t *testing.T) {
	p := &postUsageBillingParams{Cost: &CostBreakdown{ActualCost: 3}}
	zero := 0.0
	require.Zero(t, teamCachePersonalCost(p, &UsageBillingApplyResult{NewTeamBalance: &zero, UncoveredBalanceCost: 3}))
	require.InDelta(t, 1.5, teamCachePersonalCost(p, &UsageBillingApplyResult{NewTeamBalance: &zero, PersonalBalanceCost: 1.5, UncoveredBalanceCost: 1.5}), 1e-9)
	// 没有拆分结果（legacy / 第三方仓储）时仍按全额。
	require.InDelta(t, 3, teamCachePersonalCost(p, nil), 1e-9)
	require.InDelta(t, 3, teamCachePersonalCost(p, &UsageBillingApplyResult{}), 1e-9)
}

func TestTeamNoOverdraft_LegacyPathClampsAndLogs(t *testing.T) {
	logs := captureSlog(t)
	f := newTeamGuardFixture(t, BalanceModePersonalOnly)
	f.ledger.personal = 1

	applied, err := applyUsageBilling(context.Background(), "", nil, f.billingParams(3), f.deps(), nil)
	require.NoError(t, err)
	require.True(t, applied)
	personal, team := f.ledger.balances()
	require.InDelta(t, 0, personal, 1e-9)
	require.InDelta(t, 15, team, 1e-9, "personal_only never spills into the team quota")

	recs := logs(uncoveredLogMsg)
	require.Len(t, recs, 1)
	require.Equal(t, "legacy", recs[0]["source"])
	require.Equal(t, float64(70), recs[0]["api_key_id"])
	require.InDelta(t, 2, recs[0]["uncovered"], 1e-9)
}

func TestTeamNoOverdraft_UsageLogDeductionLogsUncovered(t *testing.T) {
	logs := captureSlog(t)
	ledger := &teamGuardLedger{personal: 0, team: 1}
	err := teamDeductBalanceForUsageLog(context.Background(), ledger, CreateUsageLogRequest{
		UserID: 7, APIKeyID: 70, RequestID: "usage-log-1", ActualCost: 4, BalanceMode: BalanceModePersonalFirst,
	})
	require.NoError(t, err)
	personal, team := ledger.balances()
	require.InDelta(t, 0, personal, 1e-9)
	require.InDelta(t, 0, team, 1e-9)
	recs := logs(uncoveredLogMsg)
	require.Len(t, recs, 1)
	require.Equal(t, "usage_log", recs[0]["source"])
	require.Equal(t, "usage-log-1", recs[0]["request_id"])
	require.InDelta(t, 3, recs[0]["uncovered"], 1e-9)
}

// 历史透支（已有的负余额）不做迁移：team_first 下可用资金仍按“个人 + 团队”判定，
// 欠款会占用等额团队额度（与修复前一致），可用资金 <= 0 时继续拦截。
func TestTeamNoOverdraft_EligibilityWithExistingNegativePersonal(t *testing.T) {
	f := newTeamGuardFixture(t, "")
	f.ledger.personal = -1.73
	f.ledger.team = 38.94
	f.user.Balance = -1.73
	f.user.TeamBalance = 38.94
	require.NoError(t, f.svc.CheckBillingEligibility(context.Background(), f.user, f.apiKey, nil, nil, ""))

	// 扣费只动团队额度，旧欠款保持不变。
	applied, err := applyUsageBilling(context.Background(), "guard-no-od-neg", nil, f.billingParams(1), f.deps(), f.ledger)
	require.NoError(t, err)
	require.True(t, applied)
	personal, team := f.ledger.balances()
	require.InDelta(t, -1.73, personal, 1e-9)
	require.InDelta(t, 37.94, team, 1e-9)

	f.ledger.team = 1.5
	f.user.TeamBalance = 1.5
	require.ErrorIs(t, f.svc.CheckBillingEligibility(context.Background(), f.user, f.apiKey, nil, nil, ""), ErrInsufficientBalance)

	// team_only 只看团队额度：个人欠款不影响。
	f.apiKey.BalanceMode = BalanceModeTeamOnly
	require.NoError(t, f.svc.CheckBillingEligibility(context.Background(), f.user, f.apiKey, nil, nil, ""))
	// personal_only 只看个人余额：负数拦截。
	f.apiKey.BalanceMode = BalanceModePersonalOnly
	require.ErrorIs(t, f.svc.CheckBillingEligibility(context.Background(), f.user, f.apiKey, nil, nil, ""), ErrInsufficientBalance)
}
