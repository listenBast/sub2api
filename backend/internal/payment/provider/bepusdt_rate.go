package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"
)

// These are freshness/representation limits, not a spread or a market-price floor.
const BEpusdtRateCacheTTL = 30 * time.Second
const BEpusdtRateMaxAge = 120 * time.Second
const bepusdtRateURL = "https://api.coingecko.com/api/v3/simple/price?ids=tether&vs_currencies=cny&include_last_updated_at=true&precision=full"

var quoteDecimal = regexp.MustCompile(`^[0-9]{1,18}(?:\.[0-9]{1,18})?$`)

type BEpusdtRateQuote struct {
	Source    string    `json:"source"`
	Rate      string    `json:"rate"`
	QuotedAt  time.Time `json:"quoted_at"`
	FetchedAt time.Time `json:"fetched_at"`
}

type bepusdtRateSource struct {
	mu       sync.Mutex
	client   *http.Client
	now      func() time.Time
	quote    *BEpusdtRateQuote
	flight   chan struct{}
	failedAt time.Time
}

var liveBEpusdtRates = &bepusdtRateSource{client: &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, now: time.Now}

// GetBEpusdtRate never falls back to a fixed, stale, or alternate-source quote.
func GetBEpusdtRate(ctx context.Context) (*BEpusdtRateQuote, error) { return liveBEpusdtRates.get(ctx) }
func validQuoteTime(now, quoted time.Time) bool {
	return !quoted.IsZero() && !quoted.After(now.Add(5*time.Second)) && now.Sub(quoted) <= BEpusdtRateMaxAge
}
func (s *bepusdtRateSource) get(ctx context.Context) (*BEpusdtRateQuote, error) {
	for {
		s.mu.Lock()
		now := s.now()
		if s.quote != nil && now.Sub(s.quote.FetchedAt) < BEpusdtRateCacheTTL && validQuoteTime(now, s.quote.QuotedAt) {
			q := *s.quote
			s.mu.Unlock()
			return &q, nil
		}
		if s.flight != nil {
			ch := s.flight
			s.mu.Unlock()
			select {
			case <-ch:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		if !s.failedAt.IsZero() && now.Sub(s.failedAt) < 10*time.Second {
			s.mu.Unlock()
			return nil, fmt.Errorf("USDT_RATE_UNAVAILABLE: CoinGecko quote unavailable; new orders paused")
		}
		s.flight = make(chan struct{})
		s.mu.Unlock()
		q, err := s.fetch(ctx)
		s.mu.Lock()
		s.quote = q
		if err != nil {
			s.failedAt = s.now()
		} else {
			s.failedAt = time.Time{}
		}
		close(s.flight)
		s.flight = nil
		s.mu.Unlock()
		return q, err
	}
}
func (s *bepusdtRateSource) fetch(ctx context.Context) (*BEpusdtRateQuote, error) {
	failure := func() (*BEpusdtRateQuote, error) {
		return nil, fmt.Errorf("USDT_RATE_UNAVAILABLE: CoinGecko quote invalid/unavailable; new orders paused")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, bepusdtRateURL, nil)
	if err != nil {
		return failure()
	}
	req.Header.Set("Accept", "application/json")
	res, err := s.client.Do(req)
	if err != nil {
		return failure()
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return failure()
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8193))
	if err != nil || len(raw) > 8192 {
		return failure()
	}
	var data struct {
		Tether struct {
			CNY     json.RawMessage `json:"cny"`
			Updated json.RawMessage `json:"last_updated_at"`
		} `json:"tether"`
	}
	if json.Unmarshal(raw, &data) != nil || !quoteDecimal.MatchString(string(data.Tether.CNY)) {
		return failure()
	}
	value, err := bepusdtPositive(string(data.Tether.CNY))
	if err != nil {
		return failure()
	}
	ts, err := strconv.ParseInt(string(data.Tether.Updated), 10, 64)
	if err != nil || ts <= 0 {
		return failure()
	}
	now := s.now()
	quoted := time.Unix(ts, 0).UTC()
	if !validQuoteTime(now, quoted) {
		return failure()
	}
	return &BEpusdtRateQuote{Source: "coingecko", Rate: value.String(), QuotedAt: quoted, FetchedAt: now.UTC()}, nil
}

// PrepareBEpusdtRate runs before order persistence, outside the DB transaction.
// Its result is an ephemeral per-order config; never persist it into the instance.
func PrepareBEpusdtRate(ctx context.Context, config map[string]string) (map[string]string, error) {
	cfg := cloneStringMap(config)
	for _, k := range []string{"lockedRate", "rateSource", "rateQuotedAt", "rateFetchedAt"} {
		delete(cfg, k)
	}
	if cfg["rateMode"] != "coingecko" {
		return cfg, nil
	}
	q, err := GetBEpusdtRate(ctx)
	if err != nil {
		return nil, err
	}
	cfg["lockedRate"] = q.Rate
	cfg["rateSource"] = q.Source
	cfg["rateQuotedAt"] = q.QuotedAt.Format(time.RFC3339)
	cfg["rateFetchedAt"] = q.FetchedAt.Format(time.RFC3339Nano)
	return cfg, nil
}
