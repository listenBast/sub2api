package provider

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/shopspring/decimal"
)

// BEpusdt uses fixed positive-amount native transactions, never Epay's query
// API or zero-amount/address-exclusive mode. See docs/BEpusdt.md.
type BEpusdt struct {
	instanceID string
	config     map[string]string
	httpClient *http.Client
}

var BEpusdtChains = map[string]string{
	"usdt_trc20": "usdt.trc20", "usdt_bep20": "usdt.bep20",
	"usdt_polygon": "usdt.polygon", "usdt_erc20": "usdt.erc20",
}
var evmReceiveAddress = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)
var tronReceiveAddress = regexp.MustCompile(`^T[1-9A-HJ-NP-Za-km-z]{33}$`)
var bepusdtTransactionHash = regexp.MustCompile(`^(?:0x)?[0-9a-fA-F]{64}$`)
var bepusdtDecimal = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)

// ValidateBEpusdtAddress is a format check, not proof of ownership or chain.
// Never request a private key or seed phrase.
func ValidateBEpusdtAddress(method, address string) bool {
	if _, ok := BEpusdtChains[method]; !ok {
		return false
	}
	if method == "usdt_trc20" {
		if !tronReceiveAddress.MatchString(address) {
			return false
		}
		// TRON addresses are base58check: network prefix 0x41 + 20-byte address
		// + first four bytes of double SHA256. Reject plausible-looking typos.
		alphabet := "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
		n := new(big.Int)
		for _, c := range address {
			i := strings.IndexRune(alphabet, c)
			if i < 0 {
				return false
			}
			n.Mul(n, big.NewInt(58))
			n.Add(n, big.NewInt(int64(i)))
		}
		data := n.Bytes()
		if len(data) != 25 || data[0] != 0x41 || bytes.Equal(data[1:21], make([]byte, 20)) {
			return false
		}
		first := sha256.Sum256(data[:21])
		second := sha256.Sum256(first[:])
		return hmac.Equal(data[21:], second[:4])
	}
	return evmReceiveAddress.MatchString(address) && !strings.EqualFold(address, "0x"+strings.Repeat("0", 40))
}
func bepusdtPositive(raw string) (decimal.Decimal, error) {
	if len(raw) > 40 || !bepusdtDecimal.MatchString(raw) {
		return decimal.Zero, fmt.Errorf("invalid positive decimal")
	}
	d, err := decimal.NewFromString(raw)
	if err != nil || !d.IsPositive() {
		return decimal.Zero, fmt.Errorf("invalid positive decimal")
	}
	return d, nil
}
func NewBEpusdt(id string, config map[string]string) (*BEpusdt, error) {
	for _, key := range []string{"apiBase", "apiToken", "notifyUrl", "returnUrl", "minimumAmount"} {
		if strings.TrimSpace(config[key]) == "" {
			return nil, fmt.Errorf("bepusdt config missing required key: %s", key)
		}
	}
	if config["exactMatchConfirmed"] != "yes" {
		return nil, fmt.Errorf("bepusdt exact gateway matching must be explicitly confirmed before enabling")
	}
	cfg := cloneStringMap(config)
	for _, key := range []string{"apiBase", "notifyUrl", "returnUrl"} {
		u, err := url.Parse(cfg[key])
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return nil, fmt.Errorf("bepusdt %s must be an HTTPS URL without credentials/query/fragment", key)
		}
		cfg[key] = strings.TrimRight(cfg[key], "/")
	}
	if cfg["rateMode"] != "" && cfg["rateMode"] != "fixed" && cfg["rateMode"] != "coingecko" {
		return nil, fmt.Errorf("bepusdt invalid rate mode")
	}
	keys := []string{"minimumAmount"}
	if cfg["rateMode"] != "coingecko" {
		keys = append(keys, "rate")
	}
	for _, key := range keys {
		if _, err := bepusdtPositive(cfg[key]); err != nil {
			return nil, fmt.Errorf("bepusdt %s must be an explicit positive decimal", key)
		}
	}
	minimum, _ := bepusdtPositive(cfg["minimumAmount"])
	if !minimum.Equal(minimum.Round(2)) {
		return nil, fmt.Errorf("bepusdt minimumAmount must have at most 2 decimal places (CNY)")
	}
	for method := range BEpusdtChains {
		if address := cfg[method+"Address"]; address != "" && !ValidateBEpusdtAddress(method, address) {
			return nil, fmt.Errorf("bepusdt invalid %s receiving address", method)
		}
	}
	return &BEpusdt{instanceID: id, config: cfg, httpClient: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (b *BEpusdt) Name() string        { return "BEpusdt USDT" }
func (b *BEpusdt) ProviderKey() string { return payment.TypeBEpusdt }
func (b *BEpusdt) SupportedTypes() []payment.PaymentType {
	return []payment.PaymentType{"usdt_trc20", "usdt_bep20", "usdt_polygon", "usdt_erc20"}
}
func (b *BEpusdt) MerchantIdentityMetadata() map[string]string {
	return map[string]string{"bepusdt_instance": b.instanceID, "bepusdt_api_base": b.config["apiBase"], "currency": "CNY"}
}

// Native signing differs from Epay only in the excluded field name. Scalar
// values retain their protocol representation (not float64 money arithmetic).
func bepusdtSign(params map[string]string, token string) string {
	keys := make([]string, 0, len(params))
	for key, value := range params {
		if key != "signature" && value != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	values := make([]string, 0, len(keys))
	for _, key := range keys {
		values = append(values, key+"="+params[key])
	}
	sum := md5.Sum([]byte(strings.Join(values, "&") + token))
	return hex.EncodeToString(sum[:])
}

type bepusdtOrder struct {
	TradeID        string          `json:"trade_id"`
	OrderID        string          `json:"order_id"`
	Amount         json.RawMessage `json:"amount"`
	Money          json.RawMessage `json:"money"`
	Actual         json.RawMessage `json:"actual_amount"`
	Token          string          `json:"token"`
	Fiat           string          `json:"fiat"`
	TradeType      string          `json:"trade_type"`
	Status         int             `json:"status"`
	PaymentURL     string          `json:"payment_url"`
	ExpirationTime int             `json:"expiration_time"`
}

func (b *BEpusdt) post(ctx context.Context, path string, params map[string]any, signed bool) (*bepusdtOrder, error) {
	if signed {
		p := map[string]string{}
		for k, v := range params {
			p[k] = fmt.Sprint(v)
			// Upstream SignVerify decodes JSON numeric scalars as float64 and
			// formats them with %v. This conversion is protocol canonicalization
			// only: amounts are never authorized or compared using these floats.
			if n, ok := v.(json.Number); ok {
				f, e := n.Float64()
				if e != nil {
					return nil, fmt.Errorf("bepusdt invalid signing number")
				}
				p[k] = strconv.FormatFloat(f, 'g', -1, 64)
			}
		}
		params["signature"] = bepusdtSign(p, b.config["apiToken"])
	}
	body, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("bepusdt encode request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.config["apiBase"]+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("bepusdt invalid request")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bepusdt request failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bepusdt HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return nil, fmt.Errorf("bepusdt response unreadable or too large")
	}
	var envelope struct {
		Code int           `json:"status_code"`
		Data *bepusdtOrder `json:"data"`
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.Code != 200 || envelope.Data == nil {
		return nil, fmt.Errorf("bepusdt upstream rejected request or returned an invalid response")
	}
	return envelope.Data, nil
}
func (b *BEpusdt) CreatePayment(ctx context.Context, req payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
	chain, ok := BEpusdtChains[req.PaymentType]
	if !ok {
		return nil, fmt.Errorf("bepusdt unsupported USDT chain")
	}
	address := b.config[req.PaymentType+"Address"]
	if !ValidateBEpusdtAddress(req.PaymentType, address) {
		return nil, fmt.Errorf("bepusdt receiving address for selected chain is not configured")
	}
	amount, err := bepusdtPositive(req.Amount)
	if err != nil || !amount.Equal(amount.Round(2)) {
		return nil, fmt.Errorf("bepusdt invalid CNY amount")
	}
	minimum, _ := bepusdtPositive(b.config["minimumAmount"])
	if amount.LessThan(minimum) {
		return nil, fmt.Errorf("bepusdt amount is below configured minimum")
	}
	seconds := int(time.Until(req.ExpiresAt).Seconds())
	if seconds < 120 {
		return nil, fmt.Errorf("bepusdt order requires at least 120 seconds remaining")
	}
	redirect := req.ReturnURL
	if redirect == "" {
		redirect = b.config["returnUrl"]
	}
	requestedAt := time.Now()
	rate := b.config["rate"]
	if b.config["rateMode"] == "coingecko" {
		rate = b.config["lockedRate"]
		quoted, e := time.Parse(time.RFC3339, b.config["rateQuotedAt"])
		if _, err := bepusdtPositive(rate); err != nil || e != nil || !validQuoteTime(time.Now(), quoted) || b.config["rateSource"] != "coingecko" {
			return nil, fmt.Errorf("USDT_RATE_UNAVAILABLE: missing or expired locked CoinGecko quote")
		}
	}
	data, err := b.post(ctx, "/api/v1/order/create-transaction", map[string]any{
		"order_id": req.OrderID, "amount": json.Number(amount.String()), "fiat": "CNY", "trade_type": chain,
		"address": address, "rate": rate, "notify_url": b.config["notifyUrl"], "redirect_url": redirect, "timeout": seconds,
	}, true)
	if err != nil {
		return nil, err
	}
	returned, e := bepusdtPositive(easyPayJSONScalar(data.Amount))
	actual, ae := bepusdtPositive(easyPayJSONScalar(data.Actual))
	if e != nil || !returned.Equal(amount) || ae != nil || !actual.IsPositive() || data.OrderID != req.OrderID || data.TradeID == "" || data.Status != 1 || data.ExpirationTime <= 0 || data.ExpirationTime > seconds || data.Fiat != "CNY" || data.TradeType != chain || !bepusdtSameAddress(req.PaymentType, data.Token, address) {
		return nil, fmt.Errorf("bepusdt created order binding mismatch")
	}
	// Never accept an arbitrary upstream checkout host, protocol-relative URL,
	// userinfo or executable scheme. The address-only QR never encodes a
	// network, token or amount; immutable checkout details are returned separately.
	payURL, err := url.Parse(data.PaymentURL)
	base, _ := url.Parse(b.config["apiBase"])
	if err != nil || payURL.Scheme != base.Scheme || payURL.Host != base.Host || payURL.User != nil {
		return nil, fmt.Errorf("bepusdt invalid cashier URL")
	}
	metadata := b.MerchantIdentityMetadata()
	metadata["bepusdt_rate"] = rate
	metadata["bepusdt_rate_source"] = "fixed"
	if b.config["rateMode"] == "coingecko" {
		metadata["bepusdt_rate_source"] = "coingecko"
		metadata["bepusdt_rate_quoted_at"] = b.config["rateQuotedAt"]
		metadata["bepusdt_rate_fetched_at"] = b.config["rateFetchedAt"]
	}
	metadata["bepusdt_trade_id"] = data.TradeID
	metadata["bepusdt_order_id"] = req.OrderID
	metadata["bepusdt_method"] = req.PaymentType
	metadata["bepusdt_address"] = data.Token
	metadata["bepusdt_amount"] = amount.String()
	metadata["bepusdt_actual_amount"] = actual.String()
	return &payment.CreatePaymentResponse{ExpiresAt: requestedAt.Add(time.Duration(data.ExpirationTime) * time.Second), TradeNo: data.TradeID, PayURL: data.PaymentURL, QRCode: data.Token, VerificationMetadata: metadata, Currency: "CNY"}, nil
}
func bepusdtSameAddress(method, a, b string) bool {
	if method == "usdt_trc20" {
		return a == b
	}
	return strings.EqualFold(a, b)
}
func (b *BEpusdt) query(ctx context.Context, trade string) (*bepusdtOrder, error) {
	d, err := b.post(ctx, "/api/v1/pay/info", map[string]any{"trade_id": trade}, false)
	if err != nil {
		return nil, err
	}
	if d.TradeID != trade || d.OrderID == "" {
		return nil, fmt.Errorf("bepusdt query order identity mismatch")
	}
	return d, nil
}

// Info is an unsigned payer-facing endpoint, NOT payment proof. Generic
// reconciliation must never credit from it; only the signed webhook can do so.
func (b *BEpusdt) QueryOrder(ctx context.Context, trade string) (*payment.QueryOrderResponse, error) {
	d, err := b.query(ctx, trade)
	if err != nil {
		return nil, err
	}
	if d.Status == 2 {
		return nil, fmt.Errorf("bepusdt signed notification required for paid order")
	}
	return &payment.QueryOrderResponse{TradeNo: trade, Status: payment.ProviderStatusPending}, nil
}
func (b *BEpusdt) ConfirmNotification(ctx context.Context, n *payment.PaymentNotification) error {
	if n == nil || n.Status != payment.NotificationStatusSuccess {
		return nil
	}
	d, err := b.query(ctx, n.TradeNo)
	if err != nil {
		return err
	}
	method := ""
	for k, v := range BEpusdtChains {
		if v == d.TradeType {
			method = k
		}
	}
	if method == "" {
		return fmt.Errorf("bepusdt confirmation currency/chain mismatch")
	}
	// The signed native callback has no chain field. Bind the queried chain
	// before address comparison so TRON never inherits EVM case folding.
	amount, e := bepusdtPositive(easyPayJSONScalar(d.Money))
	actual, ae := bepusdtPositive(easyPayJSONScalar(d.Actual))
	if d.OrderID != n.OrderID || d.Status != 2 || d.Fiat != "CNY" || e != nil || ae != nil || amount.String() != n.Metadata["bepusdt_amount"] || actual.String() != n.Metadata["bepusdt_actual_amount"] || !bepusdtSameAddress(method, d.Token, n.Metadata["bepusdt_address"]) {
		return fmt.Errorf("bepusdt confirmation mismatch")
	}
	n.Metadata["bepusdt_method"] = method
	return nil
}
func (b *BEpusdt) VerifyNotification(_ context.Context, raw string, _ map[string]string) (*payment.PaymentNotification, error) {
	// Reject duplicate keys, nested values, unknown fields and signed delimiter
	// smuggling. The only accepted schema is the official native callback.
	allowed := map[string]bool{"trade_id": true, "order_id": true, "amount": true, "actual_amount": true, "token": true, "block_transaction_id": true, "status": true, "signature": true}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	t, err := dec.Token()
	if err != nil || t != json.Delim('{') {
		return nil, fmt.Errorf("bepusdt invalid notification")
	}
	p := map[string]string{}
	exact := map[string]string{}
	for dec.More() {
		k, e := dec.Token()
		if e != nil {
			return nil, fmt.Errorf("bepusdt invalid notification")
		}
		key, ok := k.(string)
		if !ok || !allowed[key] {
			return nil, fmt.Errorf("bepusdt unexpected notification field")
		}
		if _, ok = p[key]; ok {
			return nil, fmt.Errorf("bepusdt duplicate notification field")
		}
		var v any
		if dec.Decode(&v) != nil {
			return nil, fmt.Errorf("bepusdt invalid notification")
		}
		switch value := v.(type) {
		case string:
			p[key] = value
		case json.Number:
			// Bound work before expanding an unauthenticated exponent. A tiny
			// JSON number such as 1e2147483647 must not allocate gigabytes in
			// Decimal.String. Native USDT precision fits within the existing
			// 40-character decimal limit, including scientific JSON notation.
			if len(value.String()) > 40 {
				return nil, fmt.Errorf("bepusdt invalid numeric field")
			}
			d, e := decimal.NewFromString(value.String())
			if e != nil || d.Exponent() < -38 || d.Exponent() > 38 {
				return nil, fmt.Errorf("bepusdt invalid numeric field")
			}
			exact[key] = d.String()
			f, e := value.Float64()
			if e != nil {
				return nil, fmt.Errorf("bepusdt invalid signing number")
			}
			p[key] = strconv.FormatFloat(f, 'g', -1, 64)
		default:
			return nil, fmt.Errorf("bepusdt invalid notification scalar")
		}
		if strings.ContainsAny(p[key], "&=\x00") {
			return nil, fmt.Errorf("bepusdt ambiguous notification scalar")
		}
	}
	if _, err = dec.Token(); err != nil {
		return nil, fmt.Errorf("bepusdt invalid notification")
	}
	if _, err = dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("bepusdt trailing notification data")
	}
	if p["signature"] == "" || !hmac.Equal([]byte(bepusdtSign(p, b.config["apiToken"])), []byte(p["signature"])) {
		return nil, fmt.Errorf("bepusdt invalid signature")
	}
	if p["trade_id"] == "" || p["order_id"] == "" {
		return nil, fmt.Errorf("bepusdt missing order identity")
	}
	if p["status"] != "2" {
		return nil, nil
	}
	amountText, actualText := p["amount"], p["actual_amount"]
	if v, ok := exact["amount"]; ok {
		amountText = v
	}
	if v, ok := exact["actual_amount"]; ok {
		actualText = v
	}
	amount, e := bepusdtPositive(amountText)
	actual, ae := bepusdtPositive(actualText)
	if e != nil || ae != nil || !amount.Equal(amount.Round(2)) || !bepusdtTransactionHash.MatchString(p["block_transaction_id"]) || p["token"] == "" {
		return nil, fmt.Errorf("bepusdt invalid paid notification")
	}
	metadata := b.MerchantIdentityMetadata()
	metadata["bepusdt_trade_id"] = p["trade_id"]
	metadata["bepusdt_order_id"] = p["order_id"]
	metadata["bepusdt_amount"] = amount.String()
	metadata["bepusdt_actual_amount"] = actual.String()
	metadata["bepusdt_address"] = p["token"]
	// This float is solely the legacy interface adapter. All USDT authorization
	// comparisons use the exact decimal metadata against the creation snapshot.
	value, _ := amount.Float64()
	return &payment.PaymentNotification{TradeNo: p["trade_id"], OrderID: p["order_id"], Amount: value, Status: payment.NotificationStatusSuccess, Metadata: metadata}, nil
}
func (b *BEpusdt) Refund(context.Context, payment.RefundRequest) (*payment.RefundResponse, error) {
	return nil, fmt.Errorf("bepusdt automatic refunds are not supported; manual review required")
}
func (b *BEpusdt) CancelPayment(ctx context.Context, trade string) error {
	_, err := b.post(ctx, "/api/v1/order/cancel-transaction", map[string]any{"trade_id": trade}, true)
	return err
}
