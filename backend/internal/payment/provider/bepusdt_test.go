package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

const testUSDTAddress = "0x1111111111111111111111111111111111111111"

func testBEpusdt(t *testing.T, handler http.HandlerFunc) *BEpusdt {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	cfg := map[string]string{"apiBase": server.URL, "apiToken": "test-only-token", "notifyUrl": "https://merchant.example/api/v1/payment/webhook/usdt", "returnUrl": "https://merchant.example/payment/result", "rate": "7", "minimumAmount": "1", "exactMatchConfirmed": "yes", "usdt_bep20Address": testUSDTAddress}
	b, err := NewBEpusdt("1", cfg)
	require.NoError(t, err)
	b.httpClient = server.Client()
	b.httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return b
}
func paidBEpusdtRaw(t *testing.T, b *BEpusdt, change func(map[string]string)) string {
	p := map[string]string{"trade_id": "trade-1", "order_id": "order-1", "amount": "14", "actual_amount": "2", "token": testUSDTAddress, "block_transaction_id": strings.Repeat("a", 64), "status": "2"}
	if change != nil {
		change(p)
	}
	p["signature"] = bepusdtSign(p, b.config["apiToken"])
	raw, err := json.Marshal(p)
	require.NoError(t, err)
	return string(raw)
}
func TestBEpusdtOfficialSignatureVector(t *testing.T) {
	require.Equal(t, "1cd4b52df5587cfb1968b0c0c6e156cd", bepusdtSign(map[string]string{"order_id": "20220201030210321", "amount": "42", "notify_url": "http://example.com/notify", "redirect_url": "http://example.com/redirect"}, "epusdt_password_xasddawqe"))
}
func TestBEpusdtCreateUsesConfiguredAddressAndChain(t *testing.T) {
	var b *BEpusdt
	b = testBEpusdt(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/order/create-transaction", r.URL.Path)
		var p map[string]any
		dec := json.NewDecoder(r.Body)
		dec.UseNumber()
		require.NoError(t, dec.Decode(&p))
		require.Equal(t, testUSDTAddress, p["address"])
		require.Equal(t, "usdt.bep20", p["trade_type"])
		require.Equal(t, "CNY", p["fiat"])
		require.Equal(t, "7", p["rate"])
		ps := map[string]string{}
		for k, v := range p {
			ps[k] = fmt.Sprint(v)
		}
		require.Equal(t, bepusdtSign(ps, b.config["apiToken"]), p["signature"])
		_, _ = fmt.Fprintf(w, `{"status_code":200,"data":{"trade_id":"trade-1","order_id":"order-1","amount":"14.00","actual_amount":"2.00","token":%q,"trade_type":"usdt.bep20","fiat":"CNY","status":1,"expiration_time":500,"payment_url":%q}}`, testUSDTAddress, b.config["apiBase"]+"/pay/checkout-counter/trade-1")
	})
	result, err := b.CreatePayment(context.Background(), payment.CreatePaymentRequest{OrderID: "order-1", PaymentType: "usdt_bep20", Amount: "14.00", ExpiresAt: time.Now().Add(10 * time.Minute)})
	require.NoError(t, err)
	require.Equal(t, testUSDTAddress, result.QRCode)
	require.NotEqual(t, result.PayURL, result.QRCode)
	require.WithinDuration(t, time.Now().Add(500*time.Second), result.ExpiresAt, 2*time.Second)
	require.Equal(t, "2", result.VerificationMetadata["bepusdt_actual_amount"])
	require.Equal(t, testUSDTAddress, result.VerificationMetadata["bepusdt_address"])
	for _, method := range []string{"usdt_trc20", "usdt_polygon", "bogus"} {
		_, err = b.CreatePayment(context.Background(), payment.CreatePaymentRequest{PaymentType: method, Amount: "14.00", ExpiresAt: time.Now().Add(time.Hour)})
		require.Error(t, err)
	}
}
func TestBEpusdtVerifyNotification(t *testing.T) {
	b := testBEpusdt(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected query during verification") })
	raw := paidBEpusdtRaw(t, b, nil)
	n, err := b.VerifyNotification(context.Background(), raw, nil)
	require.NoError(t, err)
	require.Equal(t, "14", n.Metadata["bepusdt_amount"])
	numeric := strings.ReplaceAll(strings.ReplaceAll(raw, `"amount":"14"`, `"amount":14`), `"status":"2"`, `"status":2`)
	_, err = b.VerifyNotification(context.Background(), numeric, nil)
	require.NoError(t, err)
	tests := map[string]string{
		"bad signature":  strings.Replace(raw, `"amount":"14"`, `"amount":"15"`, 1),
		"duplicate":      strings.Replace(raw, `{`, `{"amount":"14",`, 1),
		"trailing":       raw + `{}`,
		"unknown signed": paidBEpusdtRaw(t, b, func(p map[string]string) { p["notify_url"] = "x" }),
		"delimiters":     paidBEpusdtRaw(t, b, func(p map[string]string) { p["order_id"] = "order-1&status=2" }),
		"zero":           paidBEpusdtRaw(t, b, func(p map[string]string) { p["amount"] = "0" }),
		"nan":            paidBEpusdtRaw(t, b, func(p map[string]string) { p["amount"] = "NaN" }),
		"missing hash":   paidBEpusdtRaw(t, b, func(p map[string]string) { p["block_transaction_id"] = "" }),
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := b.VerifyNotification(context.Background(), raw, nil)
			require.Error(t, err)
		})
	}
	for _, status := range []string{"1", "3", "4"} {
		n, err := b.VerifyNotification(context.Background(), paidBEpusdtRaw(t, b, func(p map[string]string) { p["status"] = status }), nil)
		require.NoError(t, err)
		require.Nil(t, n)
	}
}
func TestBEpusdtConfirmMustMatchSignedPayment(t *testing.T) {
	for _, variant := range []string{"ok", "pending", "wrong-order", "wrong-trade", "wrong-money", "wrong-actual", "wrong-token", "wrong-currency", "wrong-chain", "query-failure", "fingerprint-denied"} {
		t.Run(variant, func(t *testing.T) {
			b := testBEpusdt(t, func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/api/v1/pay/info", r.URL.Path)
				var request map[string]string
				require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
				require.Equal(t, "trade-1", request["trade_id"])
				if variant == "query-failure" {
					w.WriteHeader(503)
					return
				}
				if variant == "fingerprint-denied" {
					_, _ = w.Write([]byte(`{"status_code":400,"message":"order not found"}`))
					return
				}
				d := map[string]any{"trade_id": "trade-1", "order_id": "order-1", "money": "14.00", "actual_amount": "2", "token": testUSDTAddress, "fiat": "CNY", "trade_type": "usdt.bep20", "status": 2}
				switch variant {
				case "pending":
					d["status"] = 1
				case "wrong-order":
					d["order_id"] = "other"
				case "wrong-trade":
					d["trade_id"] = "other"
				case "wrong-money":
					d["money"] = "14.01"
				case "wrong-actual":
					d["actual_amount"] = "1.99"
				case "wrong-token":
					d["token"] = "0x2222222222222222222222222222222222222222"
				case "wrong-currency":
					d["fiat"] = "USD"
				case "wrong-chain":
					d["trade_type"] = "usdc.bep20"
				}
				require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"status_code": 200, "data": d}))
			})
			n, err := b.VerifyNotification(context.Background(), paidBEpusdtRaw(t, b, nil), nil)
			require.NoError(t, err)
			err = b.ConfirmNotification(context.Background(), n)
			if variant == "ok" {
				require.NoError(t, err)
				require.Equal(t, "usdt_bep20", n.Metadata["bepusdt_method"])
				_, err = b.QueryOrder(context.Background(), "trade-1")
				require.ErrorContains(t, err, "signed notification required")
			} else {
				require.Error(t, err)
			}
		})
	}
}
func TestBEpusdtConfigurationFailClosed(t *testing.T) {
	b := testBEpusdt(t, func(http.ResponseWriter, *http.Request) {})
	for _, key := range []string{"apiToken", "apiBase", "rate", "minimumAmount", "exactMatchConfirmed"} {
		cfg := cloneStringMap(b.config)
		delete(cfg, key)
		_, err := NewBEpusdt("1", cfg)
		require.Error(t, err)
	}
	for _, raw := range []string{"0", "-1", "NaN", "~1.02", "+1"} {
		cfg := cloneStringMap(b.config)
		cfg["rate"] = raw
		_, err := NewBEpusdt("1", cfg)
		require.Error(t, err)
	}
	require.False(t, ValidateBEpusdtAddress("usdt_trc20", testUSDTAddress))
	require.False(t, ValidateBEpusdtAddress("usdt_bep20", "T"+strings.Repeat("1", 33)))
	require.True(t, ValidateBEpusdtAddress("usdt_trc20", "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"))
}

func TestBEpusdtEveryNetworkUsesWebsiteAddress(t *testing.T) {
	for method, chain := range BEpusdtChains {
		t.Run(method, func(t *testing.T) {
			address := testUSDTAddress
			if method == "usdt_trc20" {
				address = "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"
			}
			var b *BEpusdt
			b = testBEpusdt(t, func(w http.ResponseWriter, r *http.Request) {
				var p map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&p))
				require.Equal(t, address, p["address"])
				require.Equal(t, chain, p["trade_type"])
				require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"status_code": 200, "data": map[string]any{"trade_id": "trade-1", "order_id": "order-1", "amount": "14", "actual_amount": "2", "token": address, "trade_type": chain, "fiat": "CNY", "status": 1, "expiration_time": 500, "payment_url": b.config["apiBase"] + "/pay/checkout-counter/trade-1"}}))
			})
			b.config[method+"Address"] = address
			result, err := b.CreatePayment(context.Background(), payment.CreatePaymentRequest{OrderID: "order-1", PaymentType: method, Amount: "14", ExpiresAt: time.Now().Add(10 * time.Minute)})
			require.NoError(t, err)
			require.Equal(t, address, result.VerificationMetadata["bepusdt_address"])
			require.Equal(t, method, result.VerificationMetadata["bepusdt_method"])
			require.Equal(t, address, result.QRCode)
		})
	}
}
func TestBEpusdtCreateRejectsMismatchedGatewayResponse(t *testing.T) {
	for _, variant := range []string{"wrong-address", "wrong-chain", "wrong-order", "wrong-fiat", "wrong-amount", "zero-actual", "foreign-cashier", "unsafe-cashier", "long-expiry", "HTTP-error", "business-error"} {
		t.Run(variant, func(t *testing.T) {
			var b *BEpusdt
			b = testBEpusdt(t, func(w http.ResponseWriter, r *http.Request) {
				if variant == "HTTP-error" {
					w.WriteHeader(500)
					return
				}
				if variant == "business-error" {
					_, _ = w.Write([]byte(`{"status_code":400}`))
					return
				}
				d := map[string]any{"trade_id": "trade-1", "order_id": "order-1", "amount": "14", "actual_amount": "2", "token": testUSDTAddress, "trade_type": "usdt.bep20", "fiat": "CNY", "status": 1, "expiration_time": 500, "payment_url": b.config["apiBase"] + "/pay/checkout-counter/trade-1"}
				switch variant {
				case "wrong-address":
					d["token"] = "0x2222222222222222222222222222222222222222"
				case "wrong-chain":
					d["trade_type"] = "usdt.erc20"
				case "wrong-order":
					d["order_id"] = "other"
				case "wrong-fiat":
					d["fiat"] = "USD"
				case "wrong-amount":
					d["amount"] = "13.99"
				case "zero-actual":
					d["actual_amount"] = "0"
				case "foreign-cashier":
					d["payment_url"] = "https://different.example/pay"
				case "unsafe-cashier":
					d["payment_url"] = "javascript:alert(1)"
				case "long-expiry":
					d["expiration_time"] = 10000
				}
				require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"status_code": 200, "data": d}))
			})
			_, err := b.CreatePayment(context.Background(), payment.CreatePaymentRequest{OrderID: "order-1", PaymentType: "usdt_bep20", Amount: "14", ExpiresAt: time.Now().Add(10 * time.Minute)})
			require.Error(t, err)
		})
	}
}
func TestBEpusdtCancelUsesSignedNativeAPI(t *testing.T) {
	var b *BEpusdt
	b = testBEpusdt(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/order/cancel-transaction", r.URL.Path)
		var p map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&p))
		require.Equal(t, "trade-1", p["trade_id"])
		require.Equal(t, bepusdtSign(p, b.config["apiToken"]), p["signature"])
		_, _ = w.Write([]byte(`{"status_code":200,"data":{"trade_id":"trade-1"}}`))
	})
	require.NoError(t, b.CancelPayment(context.Background(), "trade-1"))
	_, err := b.Refund(context.Background(), payment.RefundRequest{TradeNo: "trade-1", Amount: "1"})
	require.ErrorContains(t, err, "not supported")
}

// TRON base58 addresses are case-sensitive; EVM hex addresses are not.
// The callback intentionally has no method until the same-instance query binds it.
func TestBEpusdtConfirmationUsesNetworkAddressCaseRules(t *testing.T) {
	const tronAddress = "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"
	const evmAddress = "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd"
	cases := []struct {
		name, method, address, queryAddress string
		reject                              bool
	}{
		{"tron-exact", "usdt_trc20", tronAddress, tronAddress, false},
		{"tron-case-change", "usdt_trc20", tronAddress, strings.ToLower(tronAddress), true},
		{"bsc-hex-case", "usdt_bep20", evmAddress, "0x" + strings.ToUpper(evmAddress[2:]), false},
		{"polygon-hex-case", "usdt_polygon", evmAddress, "0x" + strings.ToUpper(evmAddress[2:]), false},
		{"ethereum-hex-case", "usdt_erc20", evmAddress, "0x" + strings.ToUpper(evmAddress[2:]), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := testBEpusdt(t, func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/api/v1/pay/info", r.URL.Path)
				require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"status_code": 200, "data": map[string]any{
					"trade_id": "trade-1", "order_id": "order-1", "money": "14.00", "actual_amount": "2",
					"token": tc.queryAddress, "fiat": "CNY", "trade_type": BEpusdtChains[tc.method], "status": 2,
				}}))
			})
			raw := paidBEpusdtRaw(t, b, func(p map[string]string) { p["token"] = tc.address })
			n, err := b.VerifyNotification(context.Background(), raw, nil)
			require.NoError(t, err)
			require.NotContains(t, n.Metadata, "bepusdt_method")
			err = b.ConfirmNotification(context.Background(), n)
			if tc.reject {
				require.ErrorContains(t, err, "confirmation mismatch")
				require.NotContains(t, n.Metadata, "bepusdt_method")
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.method, n.Metadata["bepusdt_method"])
			}
		})
	}
}

func TestBEpusdtRejectsUnboundedCallbackNumbers(t *testing.T) {
	b := testBEpusdt(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected query") })
	for _, key := range []string{"amount", "actual_amount", "status", "order_id"} {
		for _, value := range []string{"1e2147483647", "1e-2147483647", strings.Repeat("1", 41)} {
			t.Run(key+"/"+value, func(t *testing.T) {
				_, err := b.VerifyNotification(context.Background(), fmt.Sprintf(`{"%s":%s}`, key, value), nil)
				require.ErrorContains(t, err, "invalid numeric field")
			})
		}
	}
}

func TestBEpusdtScientificCallbackNumberRetainsExactAmount(t *testing.T) {
	b := testBEpusdt(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected query") })
	raw := paidBEpusdtRaw(t, b, func(p map[string]string) { p["actual_amount"] = "2e-18" })
	raw = strings.Replace(raw, `"actual_amount":"2e-18"`, `"actual_amount":2e-18`, 1)
	n, err := b.VerifyNotification(context.Background(), raw, nil)
	require.NoError(t, err)
	require.Equal(t, "0.000000000000000002", n.Metadata["bepusdt_actual_amount"])
}
