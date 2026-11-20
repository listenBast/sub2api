package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/walletguard"
	"github.com/google/uuid"
)

type walletStoreProvider interface{ WalletStore() walletguard.Store }
type strictWalletQuoteKey struct{}
type strictWalletQuote struct {
	ID               string
	UserID, APIKeyID int64
	Units            walletguard.Amount
	Cost             CostBreakdown
	Kind             string
}

func walletQuote(ctx context.Context) (strictWalletQuote, bool) {
	if ctx == nil {
		return strictWalletQuote{}, false
	}
	q, ok := ctx.Value(strictWalletQuoteKey{}).(strictWalletQuote)
	return q, ok
}
func HasStrictWalletQuote(ctx context.Context) bool { _, ok := walletQuote(ctx); return ok }

// CopyStrictWalletQuote carries only immutable pricing/identity, never request
// bodies, API credentials, a handler or a database connection into a worker.
func CopyStrictWalletQuote(parent, base context.Context) context.Context {
	if q, ok := walletQuote(parent); ok {
		return context.WithValue(base, strictWalletQuoteKey{}, q)
	}
	return base
}

// FixedWalletRequest is one fixed-total-price invocation, including its bounded
// failover attempts. No automatic release is possible after a network send may
// have begun. Unknown outcomes require reconciliation, not a guessed refund.
type FixedWalletRequest struct {
	store      walletguard.Store
	id         string
	userID     int64
	dispatched atomic.Bool
	done       sync.Once
	invalidate func(context.Context)
}

func (r *FixedWalletRequest) BeforeDispatch(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if err := r.store.MarkDispatched(ctx, r.userID, r.id); err != nil {
		return ErrBillingServiceUnavailable.WithCause(err)
	}
	r.dispatched.Store(true)
	return nil
}
func (r *FixedWalletRequest) Done() {
	if r == nil {
		return
	}
	r.done.Do(func() {
		if r.dispatched.Load() {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := r.store.ReleaseBeforeNetwork(ctx, r.userID, r.id); err == nil {
			r.invalidate(ctx)
		}
	})
}

func beginFixedWallet(ctx context.Context, billing *BillingCacheService, key *APIKey, subscription *UserSubscription, cost *CostBreakdown, kind, payloadHash string, invalidator APIKeyQuotaUpdater) (context.Context, *FixedWalletRequest, error) {
	if key == nil || key.User == nil || key.Group == nil || billing == nil || cost == nil {
		return ctx, nil, ErrBillingServiceUnavailable
	}
	if billing.cfg != nil && billing.cfg.RunMode == config.RunModeSimple {
		return ctx, nil, nil
	}
	if key.Group.IsSubscriptionType() && subscription != nil {
		return ctx, nil, nil
	}
	if cost.ActualCost < 0 || math.IsNaN(cost.ActualCost) || math.IsInf(cost.ActualCost, 0) || cost.TotalCost < 0 || math.IsNaN(cost.TotalCost) || math.IsInf(cost.TotalCost, 0) {
		return ctx, nil, ErrBillingServiceUnavailable
	}
	pinned := *cost
	pinned.ActualCost = QuantizeUsageBillingAmount(cost.ActualCost)
	pinned.TotalCost = QuantizeUsageBillingAmount(cost.TotalCost)
	units, err := walletguard.ParseAmount(strconv.FormatFloat(pinned.ActualCost, 'f', 8, 64))
	if err != nil {
		return ctx, nil, ErrBillingServiceUnavailable.WithCause(err)
	}
	provider, ok := billing.userRepo.(walletStoreProvider)
	if !ok {
		return ctx, nil, ErrBillingServiceUnavailable
	}
	store := provider.WalletStore()
	q := strictWalletQuote{ID: uuid.NewString(), UserID: key.User.ID, APIKeyID: key.ID, Units: units, Cost: pinned, Kind: kind}
	if units == 0 {
		if err := store.CheckAvailable(ctx, key.User.ID, NormalizeBalanceMode(key.BalanceMode)); err != nil {
			if errors.Is(err, walletguard.ErrInsufficient) {
				return ctx, nil, ErrInsufficientBalance
			}
			return ctx, nil, ErrBillingServiceUnavailable.WithCause(err)
		}
		return context.WithValue(ctx, strictWalletQuoteKey{}, q), nil, nil
	}

	wire, err := json.Marshal(q)
	if err != nil {
		return ctx, nil, err
	}
	digest := sha256.Sum256(wire)
	_, err = store.Reserve(ctx, walletguard.Request{ID: q.ID, UserID: key.User.ID, APIKeyID: key.ID, Fingerprint: payloadHash, PricingDigest: hex.EncodeToString(digest[:]), Mode: NormalizeBalanceMode(key.BalanceMode), Ceiling: units})
	if errors.Is(err, walletguard.ErrInsufficient) {
		return ctx, nil, ErrInsufficientBalance
	}
	if err != nil {
		return ctx, nil, ErrBillingServiceUnavailable.WithCause(err)
	}
	invalidate := func(ctx context.Context) {
		_ = billing.InvalidateUserBalance(ctx, key.User.ID)
		if i, ok := invalidator.(teamUserAuthCacheInvalidator); ok {
			i.InvalidateAuthCacheByUserID(ctx, key.User.ID)
		}
	}
	// Cache invalidation is advisory, never the source of reservation permission.
	invalidate(ctx)
	return context.WithValue(ctx, strictWalletQuoteKey{}, q), &FixedWalletRequest{store: store, id: q.ID, userID: key.User.ID, invalidate: invalidate}, nil
}

// BeginAlphaSearchWallet pins exactly ONE per-call search charge. Existing alpha
// billing explicitly does not bill token/reasoning/image/service-tier supplements.
func (s *OpenAIGatewayService) BeginAlphaSearchWallet(ctx context.Context, key *APIKey, sub *UserSubscription, body []byte, invalidator APIKeyQuotaUpdater) (context.Context, *FixedWalletRequest, error) {
	if s == nil || s.billingService == nil || key == nil || key.User == nil || key.Group == nil {
		return ctx, nil, ErrBillingServiceUnavailable
	}
	multiplier := s.ResolveUserGroupRateMultiplier(ctx, key.User.ID, key.Group.ID, key.Group.RateMultiplier)
	if multiplier < 0 || math.IsNaN(multiplier) || math.IsInf(multiplier, 0) {
		return ctx, nil, ErrBillingServiceUnavailable
	}
	price := webSearchPricePerCallFromAPIKey(key)
	if price != nil && (*price < 0 || math.IsNaN(*price) || math.IsInf(*price, 0)) {
		return ctx, nil, ErrBillingServiceUnavailable
	}
	return beginFixedWallet(ctx, s.billingCacheService, key, sub, s.billingService.CalculateWebSearchCost(1, price, multiplier), "alpha-search", HashUsageRequestPayload(body), invalidator)
}

// BeginStandaloneSearchWallet snapshots the same zero-token SearchCount=1 result
// used by standalone Grok web/x search. It includes configured per-request and
// search surcharges, but cannot be used for tool search within token generation.
func (s *GatewayService) BeginStandaloneSearchWallet(ctx context.Context, key *APIKey, sub *UserSubscription, model, query string, invalidator APIKeyQuotaUpdater) (context.Context, *FixedWalletRequest, error) {
	if s == nil || s.billingService == nil || key == nil || key.User == nil || key.Group == nil {
		return ctx, nil, ErrBillingServiceUnavailable
	}
	multiplier, imageMultiplier, at := s.recordUsageMultipliers(ctx, key, key.User, time.Time{})
	if model != "grok-web-search" && model != "grok-x-search" {
		return ctx, nil, ErrBillingServiceUnavailable
	}
	if multiplier < 0 || math.IsNaN(multiplier) || math.IsInf(multiplier, 0) {
		return ctx, nil, ErrBillingServiceUnavailable
	}
	if p := key.Group.GetSearchPricePer1k(); p != nil && (*p < 0 || math.IsNaN(*p) || math.IsInf(*p, 0)) {
		return ctx, nil, ErrBillingServiceUnavailable
	}

	cost := s.calculateRecordUsageCost(ctx, &ForwardResult{Model: model, SearchCount: 1}, key, model, multiplier, imageMultiplier, at)
	return beginFixedWallet(ctx, s.billingCacheService, key, sub, cost, "grok-search", HashUsageRequestPayload([]byte(query)), invalidator)
}

// ConfirmHTTPFailure is for a fully read, explicit non-2xx response only. Errors
// with missing/incomplete responses MUST NOT release a possibly billable hold.
func (r *FixedWalletRequest) ConfirmHTTPFailure() error {
	if r == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := r.store.ReleaseConfirmedHTTPFailure(ctx, r.userID, r.id); err != nil {
		return err
	}
	r.invalidate(ctx)
	return nil
}

// ConfirmedSearchHTTPRejection identifies a fully read, explicit invalid-request
// response, not a generated timeout status or a partially read upstream response.
type ConfirmedSearchHTTPRejection struct{ Message string }

func (e *ConfirmedSearchHTTPRejection) Error() string { return e.Message }
