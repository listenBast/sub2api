package service

// 团队计费（sub2api 二开，fork-only）。
//
// 本文件集中放置团队模式在“计费核心”里的全部逻辑，上游文件里只保留带
// `// sub2api-custom: team billing` 标记的单行调用：
//
//   - billing_cache_service.go   CheckBillingEligibility → checkTeamBalanceEligibility
//                                ReserveFixedCharge → teamSpendableUserBalance / withTeamBillingAPIKey
//   - billing_inflight_reservation.go  reserveInflight → teamInflightSpendableBalance
//   - gateway_usage_billing.go   legacy 扣费 → teamDeductLegacyBalance；
//                                buildUsageBillingCommand 写 BalanceMode；
//                                finalizePostUsageBilling → teamInvalidateAuthAfterTeamCharge；
//                                余额缓存同步 / 余额提醒 → teamCachePersonalCost / teamNotifyPersonalCost
//   - usage_billing.go           UsageBillingCommand.BalanceMode 与 UsageBillingApplyResult 的团队字段
//
// 对应的仓储层逻辑在 repository/team_usage_billing.go，鉴权中间件在
// server/middleware/team_balance_auth.go。
//
// ⚠️ 每次合并上游（upstream-sync）后都必须运行团队计费守护测试：
//
//	cd backend && go test -tags=unit ./internal/service/ -run 'TestTeamBillingGuard_' -count=1
//	cd backend && go test -tags=unit ./internal/repository/ ./internal/server/middleware/ -run 'Team' -count=1
//
// 合并冲突时，先接受上游版本，再把带 sub2api-custom 标记的行补回去即可。

import (
	"context"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

// BalanceDeductionResult reports how a compatibility billing path split one
// charge between the user's personal wallet and allocated team quota.
type BalanceDeductionResult struct {
	Personal float64
	Team     float64
}

// BalanceModeDeductor is intentionally optional on UserRepository. Existing
// repository fakes and third-party adapters continue to use the legacy
// personal-only DeductBalance method until they implement this interface.
type BalanceModeDeductor interface {
	DeductBalanceByMode(ctx context.Context, id int64, amount float64, mode string) (BalanceDeductionResult, error)
}

// teamBillingAPIKeyCtxKey 让上游 reserveInflight 的签名保持不变：
// 调用方把 API Key 放进 ctx，teamInflightSpendableBalance 再取出来按扣费方式计算可用资金。
type teamBillingAPIKeyCtxKey struct{}

func withTeamBillingAPIKey(ctx context.Context, apiKey *APIKey) context.Context {
	if apiKey == nil {
		return ctx
	}
	return context.WithValue(ctx, teamBillingAPIKeyCtxKey{}, apiKey)
}

func teamBillingAPIKeyFromContext(ctx context.Context) *APIKey {
	if ctx == nil {
		return nil
	}
	apiKey, _ := ctx.Value(teamBillingAPIKeyCtxKey{}).(*APIKey)
	return apiKey
}

func teamBalanceModeOf(apiKey *APIKey) string {
	if apiKey == nil {
		return DefaultBalanceMode
	}
	return NormalizeBalanceMode(apiKey.BalanceMode)
}

// checkTeamBalanceEligibility 检查余额模式资格（团队模式版 checkBalanceEligibility）。
//
// 按 API Key 的扣费方式决定参与判定的资金池：
//   - personal_only：只看缓存中的个人余额（直接走上游 checkBalanceEligibility）。
//   - team_only：只看团队额度。
//   - team_first / personal_first：个人余额达标即放行；否则再加上团队额度一起判定。
//
// 团队额度不走 Redis 缓存：认证快照里的 TeamBalance 只会因划拨而增加（划拨会失效认证快照），
// 因消费而减少。所以快照为 0 时可以直接判定没有团队额度；快照大于 0 时再读一次库拿精确值。
// 这样只有“个人余额不足且确实持有团队额度”的成员才会触发额外的主键查询。
func (s *BillingCacheService) checkTeamBalanceEligibility(ctx context.Context, user *User, apiKey *APIKey) error {
	mode := teamBalanceModeOf(apiKey)
	userID := user.ID

	if mode == BalanceModePersonalOnly {
		return s.checkBalanceEligibility(ctx, userID)
	}

	if mode == BalanceModeTeamOnly {
		teamBalance, err := s.resolveTeamBalance(ctx, user)
		if err != nil {
			return s.balanceCheckFailure(userID, err)
		}
		if s.circuitBreaker != nil {
			s.circuitBreaker.OnSuccess()
		}
		if s.balanceBelowEligibilityThreshold(teamBalance) {
			return ErrInsufficientBalance
		}
		return nil
	}

	balance, err := s.GetUserBalance(ctx, userID)
	if err != nil {
		return s.balanceCheckFailure(userID, err)
	}
	if s.circuitBreaker != nil {
		s.circuitBreaker.OnSuccess()
	}
	if !s.balanceBelowEligibilityThreshold(balance) {
		return nil
	}

	// 个人余额不足：团队成员还可以用团队额度兜底。
	teamBalance, err := s.resolveTeamBalance(ctx, user)
	if err != nil {
		return s.balanceCheckFailure(userID, err)
	}
	if s.balanceBelowEligibilityThreshold(balance + teamBalance) {
		return ErrInsufficientBalance
	}
	return nil
}

// balanceCheckFailure 与上游 checkBalanceEligibility 的失败分支一致（熔断计数 + 告警日志）。
func (s *BillingCacheService) balanceCheckFailure(userID int64, err error) error {
	if s.circuitBreaker != nil {
		s.circuitBreaker.OnFailure(err)
	}
	logger.LegacyPrintf("service.billing_cache", "ALERT: billing balance check failed for user %d: %v", userID, err)
	return ErrBillingServiceUnavailable.WithCause(err)
}

// teamSpendableUserBalance = GetUserBalance + spendableBalance，用于 ReserveFixedCharge 的余额预检。
func (s *BillingCacheService) teamSpendableUserBalance(ctx context.Context, user *User, apiKey *APIKey) (float64, error) {
	personal, err := s.GetUserBalance(ctx, user.ID)
	if err != nil {
		return 0, err
	}
	return s.spendableBalance(ctx, user, apiKey, personal)
}

// teamInflightSpendableBalance 是 reserveInflight 的余额来源：预留比较的是本 Key 实际可扣的资金，
// 而不只是个人余额，否则个人余额为 0、靠团队额度消费的成员在并发请求时会被误判余额不足（403）。
// API Key 由 withTeamBillingAPIKey 放入 ctx；没有 Key 时按默认扣费方式（team_first）处理。
func (s *BillingCacheService) teamInflightSpendableBalance(ctx context.Context, user *User) (float64, error) {
	return s.teamSpendableUserBalance(ctx, user, teamBillingAPIKeyFromContext(ctx))
}

// ReserveInflightForAPIKey 同 ReserveInflight，但按 API Key 的扣费方式计算可用资金：
// team_first / personal_first 为个人余额 + 团队额度，team_only 只算团队额度，
// personal_only 只算个人余额，与 checkTeamBalanceEligibility 和实际扣费保持一致。
func (s *BillingCacheService) ReserveInflightForAPIKey(ctx context.Context, apiKey *APIKey, subscription *UserSubscription, estimate float64) (*InflightReservation, error) {
	if apiKey == nil {
		return nil, nil
	}
	return s.reserveInflight(withTeamBillingAPIKey(ctx, apiKey), apiKey.User, apiKey.Group, subscription, estimate, true, true)
}

// spendableBalance 按 API Key 的扣费方式返回本 Key 实际可扣的资金：
//   - personal_only：个人余额
//   - team_only：团队额度
//   - team_first / personal_first（含未设置 = team_first）：个人余额 + 团队额度
//
// 与 checkTeamBalanceEligibility 及仓储层拆分扣费规则一致。apiKey 为 nil 时按默认方式处理。
// 团队额度只在认证快照显示持有额度时才读库（见 resolveTeamBalance）。
func (s *BillingCacheService) spendableBalance(ctx context.Context, user *User, apiKey *APIKey, personal float64) (float64, error) {
	mode := teamBalanceModeOf(apiKey)
	if mode == BalanceModePersonalOnly {
		return personal, nil
	}
	teamBalance, err := s.resolveTeamBalance(ctx, user)
	if err != nil {
		return 0, err
	}
	if mode == BalanceModeTeamOnly {
		return teamBalance, nil
	}
	return personal + teamBalance, nil
}

// resolveTeamBalance 返回用户当前的团队额度。快照为 0 时无需读库，见 checkTeamBalanceEligibility。
func (s *BillingCacheService) resolveTeamBalance(ctx context.Context, user *User) (float64, error) {
	if user == nil || user.TeamBalance <= 0 || s.userRepo == nil {
		return 0, nil
	}
	fresh, err := s.userRepo.GetByID(ctx, user.ID)
	if err != nil {
		return 0, fmt.Errorf("get user team balance: %w", err)
	}
	return fresh.TeamBalance, nil
}

// teamUserAuthCacheInvalidator 是 APIKeyService 按用户失效认证快照的能力（可选接口）。
type teamUserAuthCacheInvalidator interface {
	InvalidateAuthCacheByUserID(context.Context, int64)
}

// teamDeductLegacyBalance 替换 legacy（非事务）路径的 userRepo.DeductBalance：
// 仓储支持 BalanceModeDeductor 时按 Key 的扣费方式拆分扣费，否则退回个人余额扣费。
// 扣费成功后按用户失效认证快照，让下次预检看到最新的团队额度。
func teamDeductLegacyBalance(ctx context.Context, p *postUsageBillingParams, deps *billingDeps, amount float64) error {
	var err error
	mode := teamBalanceModeOf(p.APIKey)
	if deductor, ok := deps.userRepo.(BalanceModeDeductor); ok {
		_, err = deductor.DeductBalanceByMode(ctx, p.User.ID, amount, mode)
	} else {
		err = deps.userRepo.DeductBalance(ctx, p.User.ID, amount)
	}
	if err != nil {
		return err
	}
	if invalidator, ok := p.APIKeyService.(teamUserAuthCacheInvalidator); ok {
		invalidator.InvalidateAuthCacheByUserID(ctx, p.User.ID)
	}
	return nil
}

// teamInvalidateAuthAfterTeamCharge：团队额度不进普通余额缓存。团队额度被扣过之后重建认证快照，
// 让下一次预检立刻看到最新团队额度（团队额度归零后也不会残留正数快照）。
func teamInvalidateAuthAfterTeamCharge(ctx context.Context, p *postUsageBillingParams, result *UsageBillingApplyResult) {
	if result == nil || result.TeamBalanceCost <= 0 {
		return
	}
	if invalidator, ok := p.APIKeyService.(teamUserAuthCacheInvalidator); ok && p.User != nil {
		// 团队额度由该成员的所有 Key 共享，按用户失效，避免其他 Key 保留过期快照。
		invalidator.InvalidateAuthCacheByUserID(ctx, p.User.ID)
	} else if invalidator, ok := p.APIKeyService.(apiKeyAuthCacheInvalidator); ok && p.APIKey != nil && p.APIKey.Key != "" {
		invalidator.InvalidateAuthCacheByKey(ctx, p.APIKey.Key)
	}
}

// teamCachePersonalCost：余额缓存只跟踪个人余额，团队额度部分不入缓存。
// 有事务拆分结果时以实际扣到个人余额的金额为准；无结果（legacy 路径）时退回全额。
func teamCachePersonalCost(p *postUsageBillingParams, result *UsageBillingApplyResult) float64 {
	if result != nil && (result.PersonalBalanceCost > 0 || result.TeamBalanceCost > 0) {
		return result.PersonalBalanceCost
	}
	return p.Cost.ActualCost
}

// teamNotifyPersonalCost：余额提醒只按个人余额部分计算；纯团队额度扣费不能让成员
// 看起来像是个人钱包被扣了。
func teamNotifyPersonalCost(p *postUsageBillingParams, result *UsageBillingApplyResult) float64 {
	if result != nil && result.NewTeamBalance != nil {
		return result.PersonalBalanceCost
	}
	return p.Cost.ActualCost
}
