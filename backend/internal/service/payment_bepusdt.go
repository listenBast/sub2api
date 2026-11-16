package service

import (
	"fmt"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/shopspring/decimal"
)

func validateBEpusdtInstance(key, types string, config map[string]string, enabled, refunds bool) error {
	if key != payment.TypeBEpusdt {
		return nil
	}
	if refunds {
		return infraerrors.BadRequest("VALIDATION_ERROR", "BEpusdt does not support automatic refunds")
	}
	for method := range provider.BEpusdtChains {
		if address := config[method+"Address"]; address != "" && !provider.ValidateBEpusdtAddress(method, address) {
			return infraerrors.BadRequest("VALIDATION_ERROR", "Invalid USDT receiving address for "+method)
		}
	}
	selected := splitTypes(types)
	if enabled && len(selected) == 0 {
		return infraerrors.BadRequest("VALIDATION_ERROR", "Select at least one USDT chain with a receiving address")
	}
	for _, method := range selected {
		if _, ok := provider.BEpusdtChains[method]; !ok {
			return infraerrors.BadRequest("VALIDATION_ERROR", "Unsupported USDT chain")
		}
		if enabled && !provider.ValidateBEpusdtAddress(method, config[method+"Address"]) {
			return infraerrors.BadRequest("VALIDATION_ERROR", "Configure receiving address before enabling "+method)
		}
	}
	return nil
}

// Creation-time binding is mandatory for this new provider. Address changes
// apply only to new orders; no current address/config is used to authorize old
// payments. Query-only metadata can never satisfy this check.
func validateBEpusdtPayment(o *dbent.PaymentOrder, trade string, metadata map[string]string) error {
	if o == nil || len(o.ProviderSnapshot) == 0 {
		return fmt.Errorf("bepusdt missing immutable order binding")
	}
	for _, key := range []string{"bepusdt_instance", "bepusdt_api_base", "bepusdt_trade_id", "bepusdt_order_id", "bepusdt_method", "bepusdt_address", "bepusdt_amount", "bepusdt_actual_amount", "currency"} {
		expected, _ := o.ProviderSnapshot[key].(string)
		actual := metadata[key]
		equal := expected == actual
		if key == "bepusdt_address" && o.PaymentType != "usdt_trc20" {
			equal = strings.EqualFold(expected, actual)
		}
		if expected == "" || actual == "" || !equal {
			return fmt.Errorf("bepusdt payment binding mismatch: %s", key)
		}
	}
	if trade != psSnapshotStringValue(o.ProviderSnapshot["bepusdt_trade_id"]) || o.OutTradeNo != metadata["bepusdt_order_id"] || psStringValue(o.ProviderInstanceID) != metadata["bepusdt_instance"] {
		return fmt.Errorf("bepusdt order identity mismatch")
	}
	if o.Status == payment.OrderStatusCancelled || o.Status == payment.OrderStatusExpired || (o.Status == payment.OrderStatusFailed && o.PaidAt == nil) || (o.Status == payment.OrderStatusPending && !time.Now().Before(o.ExpiresAt)) {
		return fmt.Errorf("bepusdt payment after cancellation/expiry requires manual review")
	}
	return nil
}

// CryptoPaymentDetails is an allowlist of nonsecret, immutable payment instructions.
// Never expose the entire provider snapshot (instance identity/config) to clients.
// The QR payload is Address only; it does not encode a wallet URI, chain or amount.
type CryptoPaymentDetails struct {
	Method        string `json:"method"`
	Asset         string `json:"asset"`
	Address       string `json:"address"`
	Amount        string `json:"amount"`
	Rate          string `json:"rate,omitempty"`
	RateSource    string `json:"rate_source,omitempty"`
	RateQuotedAt  string `json:"rate_quoted_at,omitempty"`
	RateFetchedAt string `json:"rate_fetched_at,omitempty"`
}

// PaymentOrderCryptoDetails reads creation-time data, never today's configured wallet.
func PaymentOrderCryptoDetails(order *dbent.PaymentOrder) *CryptoPaymentDetails {
	if order == nil || psStringValue(order.ProviderKey) != payment.TypeBEpusdt {
		return nil
	}
	method := psSnapshotStringValue(order.ProviderSnapshot["bepusdt_method"])
	address := psSnapshotStringValue(order.ProviderSnapshot["bepusdt_address"])
	amount := psSnapshotStringValue(order.ProviderSnapshot["bepusdt_actual_amount"])
	if method != order.PaymentType || !provider.ValidateBEpusdtAddress(method, address) {
		return nil
	}
	value, err := decimal.NewFromString(amount)
	if err != nil || !value.IsPositive() {
		return nil
	}
	return &CryptoPaymentDetails{Method: method, Asset: "USDT", Address: address, Amount: value.String(), Rate: psSnapshotStringValue(order.ProviderSnapshot["bepusdt_rate"]), RateSource: psSnapshotStringValue(order.ProviderSnapshot["bepusdt_rate_source"]), RateQuotedAt: psSnapshotStringValue(order.ProviderSnapshot["bepusdt_rate_quoted_at"]), RateFetchedAt: psSnapshotStringValue(order.ProviderSnapshot["bepusdt_rate_fetched_at"])}
}
