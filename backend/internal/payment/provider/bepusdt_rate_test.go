package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type quoteTransport func(*http.Request) (*http.Response, error)

func (f quoteTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func testRateSource(now *time.Time, response func() (int, string)) *bepusdtRateSource {
	return &bepusdtRateSource{now: func() time.Time { return *now }, client: &http.Client{Transport: quoteTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != bepusdtRateURL || r.Method != "GET" {
			return nil, fmt.Errorf("unexpected origin")
		}
		code, body := response()
		return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}}
}
func TestBEpusdtRateCacheFailureAndConcurrency(t *testing.T) {
	now := time.Now().UTC()
	var calls atomic.Int32
	fail := false
	s := testRateSource(&now, func() (int, string) {
		calls.Add(1)
		if fail {
			return 429, "{}"
		}
		return 200, fmt.Sprintf(`{"tether":{"cny":6.6623951423103795,"last_updated_at":%d}}`, now.Unix())
	})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			q, e := s.get(context.Background())
			require.NoError(t, e)
			if q != nil {
				require.Equal(t, "6.6623951423103795", q.Rate)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, calls.Load())
	now = now.Add(31 * time.Second)
	fail = true
	_, err := s.get(context.Background())
	require.Error(t, err)
	require.Nil(t, s.quote)
	_, err = s.get(context.Background())
	require.Error(t, err)
	require.EqualValues(t, 2, calls.Load())
	now = now.Add(11 * time.Second)
	fail = false
	_, err = s.get(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 3, calls.Load())
}
func TestBEpusdtRateRejectsInvalidQuotes(t *testing.T) {
	now := time.Now().UTC()
	for _, body := range []string{`{}`, `{"tether":{"cny":0,"last_updated_at":1}}`, `{"tether":{"cny":-1,"last_updated_at":1}}`, `{"tether":{"cny":1e999999,"last_updated_at":1}}`, `{"tether":{"cny":"6.67","last_updated_at":1}}`, strings.Repeat("x", 8193), `{"tether":{"cny":6.67}}`, fmt.Sprintf(`{"tether":{"cny":6.67,"last_updated_at":%d}}`, now.Add(-121*time.Second).Unix()), fmt.Sprintf(`{"tether":{"cny":6.67,"last_updated_at":%d}}`, now.Add(time.Minute).Unix()), `{"tether":{"cny":6.67,"last_updated_at":1e100}}`, `{"tether":{"cny":6.67,"last_updated_at":"1794850000"}}`, `{"tether":{"cny":6.67,"last_updated_at":1}} {}`} {
		t.Run(body[:min(len(body), 50)], func(t *testing.T) {
			s := testRateSource(&now, func() (int, string) { return 200, body })
			q, err := s.get(context.Background())
			require.Error(t, err)
			require.Nil(t, q)
		})
	}
	for _, code := range []int{301, 401, 403, 429, 500, 503} {
		s := testRateSource(&now, func() (int, string) { return code, "{}" })
		_, err := s.get(context.Background())
		require.Error(t, err)
	}
}
func TestBEpusdtRatePreparationNoFixedFallback(t *testing.T) {
	original := liveBEpusdtRates
	defer func() { liveBEpusdtRates = original }()
	now := time.Now().UTC()
	liveBEpusdtRates = testRateSource(&now, func() (int, string) { return 429, "{}" })
	_, err := PrepareBEpusdtRate(context.Background(), map[string]string{"rateMode": "coingecko", "rate": "6.67", "lockedRate": "9"})
	require.Error(t, err)
	liveBEpusdtRates = testRateSource(&now, func() (int, string) {
		return 200, fmt.Sprintf(`{"tether":{"cny":6.6623951423103795,"last_updated_at":%d}}`, now.Unix())
	})
	config := map[string]string{"rateMode": "coingecko"}
	locked, err := PrepareBEpusdtRate(context.Background(), config)
	require.NoError(t, err)
	require.Equal(t, "6.6623951423103795", locked["lockedRate"])
	require.Empty(t, config["lockedRate"])
	first := locked["lockedRate"]
	now = now.Add(time.Minute)
	liveBEpusdtRates = testRateSource(&now, func() (int, string) {
		return 200, fmt.Sprintf(`{"tether":{"cny":6.7,"last_updated_at":%d}}`, now.Unix())
	})
	_, err = PrepareBEpusdtRate(context.Background(), config)
	require.NoError(t, err)
	require.Equal(t, first, locked["lockedRate"])
	fixed, err := PrepareBEpusdtRate(context.Background(), map[string]string{"rate": "6.67"})
	require.NoError(t, err)
	require.Equal(t, "6.67", fixed["rate"])
}

func TestBEpusdtAutomaticCreateUsesLockedDecimalOnly(t *testing.T) {
	var b *BEpusdt
	b = testBEpusdt(t, func(w http.ResponseWriter, r *http.Request) {
		var p map[string]json.RawMessage
		require.NoError(t, json.NewDecoder(r.Body).Decode(&p))
		require.Equal(t, `"6.6623951423103795"`, string(p["rate"]))
		_, _ = fmt.Fprintf(w, `{"status_code":200,"data":{"trade_id":"trade-1","order_id":"order-1","amount":"14","actual_amount":"2.10","token":%q,"trade_type":"usdt.bep20","fiat":"CNY","status":1,"expiration_time":500,"payment_url":%q}}`, testUSDTAddress, b.config["apiBase"]+"/pay/checkout-counter/trade-1")
	})
	b.config["rateMode"] = "coingecko"
	delete(b.config, "rate")
	_, err := NewBEpusdt("1", b.config)
	require.NoError(t, err)
	req := payment.CreatePaymentRequest{OrderID: "order-1", PaymentType: "usdt_bep20", Amount: "14", ExpiresAt: time.Now().Add(10 * time.Minute)}
	_, err = b.CreatePayment(context.Background(), req)
	require.ErrorContains(t, err, "locked CoinGecko quote")
	b.config["lockedRate"] = "6.6623951423103795"
	b.config["rateSource"] = "coingecko"
	b.config["rateQuotedAt"] = time.Now().UTC().Format(time.RFC3339)
	b.config["rateFetchedAt"] = time.Now().UTC().Format(time.RFC3339)
	result, err := b.CreatePayment(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "6.6623951423103795", result.VerificationMetadata["bepusdt_rate"])
	require.Equal(t, "2.1", result.VerificationMetadata["bepusdt_actual_amount"])
	b.config["rateQuotedAt"] = time.Now().Add(-3 * time.Minute).Format(time.RFC3339)
	_, err = b.CreatePayment(context.Background(), req)
	require.Error(t, err)
}
