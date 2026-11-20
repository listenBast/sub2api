//go:build unit

package handler

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/walletguard"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/testutil/walletdb"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type strictWalletRouteUserRepo struct {
	service.UserRepository
	db *sql.DB
}

func (r strictWalletRouteUserRepo) WalletStore() walletguard.Store {
	return walletguard.Store{DB: r.db}
}
func (r strictWalletRouteUserRepo) GetByID(ctx context.Context, id int64) (*service.User, error) {
	u := &service.User{ID: id, Role: "user", Status: "active"}
	err := r.db.QueryRowContext(ctx, `SELECT balance,team_balance FROM users WHERE id=$1`, id).Scan(&u.Balance, &u.TeamBalance)
	return u, err
}

type strictWalletRouteKeyRepo struct{ service.APIKeyRepository }

func (strictWalletRouteKeyRepo) ListKeysByUserID(context.Context, int64) ([]string, error) {
	return nil, nil
}

type strictWalletRoute struct {
	failFirst            bool
	stream               bool
	db                   *sql.DB
	router               *gin.Engine
	handler              *OpenAIGatewayHandler
	key                  *service.APIKey
	calls                atomic.Int32
	block                chan struct{}
	started              chan struct{}
	status               int
	expectedReservations int
}

func newStrictWalletRoute(t *testing.T, personal, team, mode string, status int, withAccounts bool, caches ...service.BillingCache) *strictWalletRoute {
	t.Helper()
	db, _ := walletdb.Open(t)
	_, err := db.Exec(`INSERT INTO users(id,balance,team_balance) VALUES(1,$1,$2);`, personal, team)
	require.NoError(t, err)
	if team != "0" {
		_, err = db.Exec(`INSERT INTO team_memberships VALUES(1,1,'active')`)
		require.NoError(t, err)
	}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Default.RateMultiplier = 1
	var cache service.BillingCache
	if len(caches) > 0 {
		cache = caches[0]
	}
	users := strictWalletRouteUserRepo{db: db}
	billing := service.NewBillingCacheService(cache, users, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	price := 0.2
	gid := int64(1)
	user, err := users.GetByID(context.Background(), 1)
	require.NoError(t, err)
	key := &service.APIKey{ID: 1, UserID: 1, GroupID: &gid, Status: "active", BalanceMode: mode, User: user, Group: &service.Group{ID: 1, Platform: service.PlatformOpenAI, Status: "active", RateMultiplier: 1, WebSearchPricePerCall: &price}}
	fixture := &strictWalletRoute{db: db, key: key, status: status, expectedReservations: 1}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := fixture.calls.Add(1)
		var remaining string
		require.NoError(t, db.QueryRow(`SELECT (balance+team_balance)::text FROM users WHERE id=1`).Scan(&remaining))
		// Paid invocations observe their durable reservation before any HTTP send.
		// Explicitly free quotes still use the DB floor check, but hold no funds.
		var n int
		require.NoError(t, db.QueryRow(`SELECT count(*) FROM wallet_reservations WHERE state='reserved' AND dispatched_at IS NOT NULL`).Scan(&n))
		if fixture.expectedReservations == 0 {
			require.Zero(t, n)
		} else {
			require.GreaterOrEqual(t, n, fixture.expectedReservations)
		}
		if fixture.started != nil {
			select {
			case fixture.started <- struct{}{}:
			default:
			}
		}

		status := fixture.status
		if fixture.failFirst && call == 1 {
			status = 404
		}
		w.Header().Set("Content-Type", "application/json")
		if fixture.stream {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(status)
			_, _ = io.WriteString(w, "data: {\"type\":\"search.started\"}\n\n")
			w.(http.Flusher).Flush()
		} else {
			w.WriteHeader(status)
		}
		if fixture.block != nil {
			select {
			case <-fixture.block:
			case <-r.Context().Done():
				return
			}
		}
		if status == 200 {
			_, _ = io.WriteString(w, `{"results":[{"url":"https://fixture.invalid"}]}`)
		} else {
			_, _ = io.WriteString(w, `{"error":{"message":"fixture rejected"}}`)
		}
	}))
	t.Cleanup(upstream.Close)
	target, err := url.Parse(upstream.URL)
	require.NoError(t, err)
	accounts := []service.Account(nil)
	if withAccounts {
		accounts = []service.Account{{ID: 1, Name: "synthetic", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "synthetic-not-a-real-key", "base_url": "https://fixture.example/v1"}}}
	}
	if len(accounts) > 0 {
		second := accounts[0]
		second.ID = 2
		accounts = append(accounts, second)
	}
	accountRepo := openAIImagesFailoverAccountRepo{accounts: accounts}
	deferred := service.NewDeferredService(accountRepo, nil, time.Minute)
	raw := repository.NewUsageBillingRepository(nil, db)
	lanes := service.NewUsageBillingLaneCombiner(raw)
	t.Cleanup(lanes.Stop)
	gateway := service.NewOpenAIGatewayService(accountRepo, nil, lanes, users, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, billing, &nativeImageRouteUpstream{target: target, client: upstream.Client()}, deferred, nil, nil, nil, nil, nil, nil, nil)
	keys := service.NewAPIKeyService(strictWalletRouteKeyRepo{}, nil, nil, nil, nil, nil, cfg)
	h := NewOpenAIGatewayHandler(gateway, service.NewConcurrencyService(nil), billing, keys, nil, nil, nil, nil, cfg)
	h.maxAccountSwitches = 2
	fixture.handler = h
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyAPIKey), key)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
	})
	router.POST("/v1/alpha/search", h.AlphaSearch)
	fixture.router = router
	return fixture
}
func (f *strictWalletRoute) call() *httptest.ResponseRecorder {
	return f.callContext(context.Background())
}
func (f *strictWalletRoute) callContext(ctx context.Context) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/alpha/search", strings.NewReader(`{"model":"gpt-5","query":"synthetic"}`))
	r = r.WithContext(ctx)
	r.Header.Set("Content-Type", "application/json")
	f.router.ServeHTTP(w, r)
	return w
}
func (f *strictWalletRoute) balances(t *testing.T, p, tm string) {
	t.Helper()
	var a, b string
	require.NoError(t, f.db.QueryRow(`SELECT balance::text,team_balance::text FROM users WHERE id=1`).Scan(&a, &b))
	require.Equal(t, p, a)
	require.Equal(t, tm, b)
}

func TestStrictWalletAlphaRealRouteSettlement(t *testing.T) {
	for _, mode := range []string{"personal_only", "team_only", "personal_first", "team_first"} {
		t.Run(mode, func(t *testing.T) {
			f := newStrictWalletRoute(t, "1", "2", mode, 200, true)
			w := f.call()
			require.Equal(t, 200, w.Code, w.Body.String())
			require.EqualValues(t, 1, f.calls.Load())
			if mode == "personal_only" || mode == "personal_first" {
				f.balances(t, "0.80000000", "2.00000000")
			} else {
				f.balances(t, "1.00000000", "1.80000000")
			}
			var state string
			var n int
			require.NoError(t, f.db.QueryRow(`SELECT state FROM wallet_reservations`).Scan(&state))
			require.Equal(t, "settled", state)
			require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM wallet_billing_outbox`).Scan(&n))
			require.Zero(t, n)
		})
	}
}
func TestStrictWalletAlphaRealRouteFloorAndInsufficient(t *testing.T) {
	for _, balance := range []string{"0", "0.1", "0.10000001", "0.19999999"} {
		t.Run(balance, func(t *testing.T) {
			f := newStrictWalletRoute(t, balance, "0", "personal_only", 200, true)
			w := f.call()
			require.NotEqual(t, 200, w.Code, w.Body.String())
			require.Zero(t, f.calls.Load())
		})
	}
}
func TestStrictWalletAlphaRealRouteReleasesOnlyProvenFailure(t *testing.T) {
	t.Run("no scheduled account", func(t *testing.T) {
		f := newStrictWalletRoute(t, "1", "0", "personal_only", 200, false)
		w := f.call()
		require.NotEqual(t, 200, w.Code)
		require.Zero(t, f.calls.Load())
		f.balances(t, "1.00000000", "0.00000000")
		var state string
		require.NoError(t, f.db.QueryRow(`SELECT state FROM wallet_reservations`).Scan(&state))
		require.Equal(t, "released", state)
	})
	t.Run("explicit bad request", func(t *testing.T) {
		f := newStrictWalletRoute(t, "1", "0", "personal_only", 400, true)
		w := f.call()
		require.Equal(t, 400, w.Code, w.Body.String())
		f.balances(t, "1.00000000", "0.00000000")
		var reason string
		require.NoError(t, f.db.QueryRow(`SELECT no_charge_reason FROM wallet_reservations`).Scan(&reason))
		require.Equal(t, "confirmed_http_non_2xx", reason)
	})
}
func TestStrictWalletAlphaRealRouteConcurrentRequests(t *testing.T) {
	f := newStrictWalletRoute(t, "0.3", "0", "personal_only", 200, true)
	f.block = make(chan struct{})
	f.started = make(chan struct{}, 1)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- f.call() }()
	select {
	case <-f.started:
	case <-time.After(5 * time.Second):
		t.Fatal("first upstream never started")
	}
	second := f.call()
	require.NotEqual(t, 200, second.Code)
	require.EqualValues(t, 1, f.calls.Load())
	close(f.block)
	select {
	case w := <-done:
		require.Equal(t, 200, w.Code, w.Body.String())
	case <-time.After(5 * time.Second):
		t.Fatal("request did not settle")
	}
	f.balances(t, "0.10000000", "0.00000000")
}

type strictStaleWalletCache struct {
	service.BillingCache
	deltas atomic.Int32
}

func (*strictStaleWalletCache) GetUserBalance(context.Context, int64) (float64, error) {
	return 100, nil
}
func (*strictStaleWalletCache) InvalidateUserBalance(context.Context, int64) error {
	return errors.New("synthetic Redis outage")
}
func (c *strictStaleWalletCache) DeductUserBalance(context.Context, int64, float64) error {
	c.deltas.Add(1)
	return nil
}

func TestStrictWalletAlphaRealRouteIgnoresStaleCache(t *testing.T) {
	cache := &strictStaleWalletCache{}
	f := newStrictWalletRoute(t, "0.1", "0", "personal_only", 200, true, cache)
	w := f.call()
	require.NotEqual(t, 200, w.Code)
	require.Zero(t, f.calls.Load())
	f2 := newStrictWalletRoute(t, "1", "0", "personal_only", 200, true, cache)
	w = f2.call()
	require.Equal(t, 200, w.Code, w.Body.String())
	f2.balances(t, "0.80000000", "0.00000000")
	require.Zero(t, cache.deltas.Load())
}
func TestStrictWalletAlphaRealRouteRetriesUseOneReservation(t *testing.T) {
	f := newStrictWalletRoute(t, "1", "0", "personal_only", 200, true)
	f.failFirst = true
	w := f.call()
	require.Equal(t, 200, w.Code, w.Body.String())
	require.EqualValues(t, 2, f.calls.Load())
	f.balances(t, "0.80000000", "0.00000000")
	var n int
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM wallet_reservations`).Scan(&n))
	require.Equal(t, 1, n)
}
func TestStrictWalletAlphaRealRoutePinsPriceThroughPartialSSE(t *testing.T) {
	f := newStrictWalletRoute(t, "1", "0", "personal_only", 200, true)
	f.stream = true
	f.block = make(chan struct{})
	f.started = make(chan struct{}, 1)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- f.call() }()
	select {
	case <-f.started:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream never started")
	}
	f.balances(t, "0.80000000", "0.00000000")
	// Snapshot is immutable even when the runtime group price changes before the
	// upstream response and asynchronous billing boundary would normally finish.
	*f.key.Group.WebSearchPricePerCall = 0.9
	close(f.block)
	select {
	case w := <-done:
		require.Equal(t, 200, w.Code, w.Body.String())
	case <-time.After(5 * time.Second):
		t.Fatal("SSE request did not finish")
	}
	f.balances(t, "0.80000000", "0.00000000")
	var state string
	require.NoError(t, f.db.QueryRow(`SELECT state FROM wallet_reservations`).Scan(&state))
	require.Equal(t, "settled", state)
}
func TestStrictWalletAlphaRealRouteCancellationRetainsUnknownOutcome(t *testing.T) {
	f := newStrictWalletRoute(t, "1", "0", "personal_only", 200, true)
	f.stream = true
	f.block = make(chan struct{})
	f.started = make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{}, 1)
	go func() { f.callContext(ctx); done <- struct{}{} }()
	select {
	case <-f.started:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream never started")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		close(f.block)
		t.Fatal("cancelled request did not return")
	}
	close(f.block)
	f.balances(t, "0.80000000", "0.00000000")
	var state string
	require.NoError(t, f.db.QueryRow(`SELECT state FROM wallet_reservations`).Scan(&state))
	require.Equal(t, "reserved", state)
}

type strictWalletGroupRepo struct {
	service.GroupRepository
	group *service.Group
}

func (r strictWalletGroupRepo) GetByID(context.Context, int64) (*service.Group, error) {
	return r.group, nil
}

func TestStrictWalletStandaloneGrokRealRoutes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		xSearch bool
		status  int
	}{
		{"web-success", false, 200}, {"x-success", true, 200}, {"web-rejected", false, 400}, {"x-rejected", true, 400},
	} {
		xSearch := tc.xSearch
		t.Run(tc.name, func(t *testing.T) {
			db, _ := walletdb.Open(t)
			_, err := db.Exec(`INSERT INTO users(id,balance,team_balance) VALUES(1,1,2);INSERT INTO team_memberships VALUES(1,1,'active')`)
			require.NoError(t, err)
			cfg := &config.Config{RunMode: config.RunModeStandard}
			cfg.Default.RateMultiplier = 1
			users := strictWalletRouteUserRepo{db: db}
			billing := service.NewBillingCacheService(nil, users, nil, nil, nil, nil, cfg, nil)
			t.Cleanup(billing.Stop)
			price := 200.0
			gid := int64(1)
			user, err := users.GetByID(context.Background(), 1)
			require.NoError(t, err)
			group := &service.Group{ID: 1, Platform: service.PlatformGrok, Status: "active", RateMultiplier: 1, SearchPricePer1k: &price}
			key := &service.APIKey{ID: 1, UserID: 1, GroupID: &gid, Group: group, User: user, BalanceMode: "team_only"}
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var team string
				require.NoError(t, db.QueryRow(`SELECT team_balance::text FROM users WHERE id=1`).Scan(&team))
				require.Equal(t, "1.80000000", team)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, `{"output":[{"type":"web_search_call","action":{"sources":[{"url":"https://fixture.invalid","title":"Synthetic"}]}}]}`)
			}))
			t.Cleanup(upstream.Close)
			target, err := url.Parse(upstream.URL)
			require.NoError(t, err)
			accounts := openAIImagesFailoverAccountRepo{accounts: []service.Account{{ID: 1, Name: "synthetic-grok", Platform: service.PlatformGrok, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "synthetic-not-real", "base_url": "https://fixture.example/v1"}}}}
			raw := repository.NewUsageBillingRepository(nil, db)
			lanes := service.NewUsageBillingLaneCombiner(raw)
			t.Cleanup(lanes.Stop)
			deferred := service.NewDeferredService(accounts, nil, time.Minute)
			concurrency := service.NewConcurrencyService(nil)
			gateway := service.NewGatewayService(accounts, strictWalletGroupRepo{group: group}, nil, lanes, users, nil, nil, nil, cfg, nil, concurrency, service.NewBillingService(cfg, nil), nil, billing, nil, &nativeImageRouteUpstream{target: target, client: upstream.Client()}, deferred, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			keys := service.NewAPIKeyService(strictWalletRouteKeyRepo{}, nil, nil, nil, nil, nil, cfg)
			h := NewGatewayHandler(gateway, nil, nil, nil, nil, concurrency, billing, nil, keys, nil, nil, nil, nil, cfg, nil)
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set(string(middleware.ContextKeyAPIKey), key)
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
				c.Set("grok_x_search_endpoint", xSearch)
			})
			router.POST("/search", h.WebSearch)
			w := httptest.NewRecorder()
			r := httptest.NewRequest("POST", "/search", strings.NewReader(`{"query":"synthetic"}`))
			r.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(w, r)
			expectedStatus := 200
			expectedTeam := "1.80000000"
			expectedState := "settled"
			if tc.status != 200 {
				expectedStatus = 502
				expectedTeam = "2.00000000"
				expectedState = "released"
			}
			require.Equal(t, expectedStatus, w.Code, w.Body.String())
			require.EqualValues(t, 1, calls.Load())
			var p, tm, state string
			require.NoError(t, db.QueryRow(`SELECT balance::text,team_balance::text FROM users WHERE id=1`).Scan(&p, &tm))
			require.Equal(t, "1.00000000", p)
			require.Equal(t, expectedTeam, tm)
			require.NoError(t, db.QueryRow(`SELECT state FROM wallet_reservations`).Scan(&state))
			require.Equal(t, expectedState, state)
		})
	}
}

func (r strictWalletGroupRepo) GetByIDLite(context.Context, int64) (*service.Group, error) {
	return r.group, nil
}

func TestStrictWalletAlphaFreeQuotePinnedAndUnique(t *testing.T) {
	f := newStrictWalletRoute(t, "1", "0", "personal_only", 200, true)
	f.expectedReservations = 0
	*f.key.Group.WebSearchPricePerCall = 0
	f.started = make(chan struct{}, 1)
	f.block = make(chan struct{})
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- f.call() }()
	<-f.started
	// A later admin price change must not bill the in-flight free quote.
	*f.key.Group.WebSearchPricePerCall = 9
	close(f.block)
	require.Equal(t, 200, (<-done).Code)
	f.balances(t, "1.00000000", "0.00000000")
	*f.key.Group.WebSearchPricePerCall = 0
	require.Equal(t, 200, f.call().Code)
	f.balances(t, "1.00000000", "0.00000000")
	var n int
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM usage_billing_dedup`).Scan(&n))
	require.Equal(t, 2, n)
}
func TestStrictWalletAlphaFreeCannotUseStaleAboveFloorCache(t *testing.T) {
	f := newStrictWalletRoute(t, "0.1", "0", "personal_only", 200, true, &strictStaleWalletCache{})
	f.expectedReservations = 0
	*f.key.Group.WebSearchPricePerCall = 0
	require.NotEqual(t, 200, f.call().Code)
	require.Zero(t, f.calls.Load())
	f.balances(t, "0.10000000", "0.00000000")
}
