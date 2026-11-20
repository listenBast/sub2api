package walletguard

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	_ "github.com/lib/pq"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func fixture(t *testing.T) (Store, string) {
	t.Helper()
	dsn := os.Getenv("WALLETGUARD_TEST_DSN")
	if dsn == "" {
		t.Skip("set WALLETGUARD_TEST_DSN to an isolated local walletguard_test PostgreSQL")
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if u.Hostname() != "127.0.0.1" || u.Path != "/walletguard_test" {
		t.Fatal("tests require 127.0.0.1/walletguard_test, never production")
	}
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("wg_%d", time.Now().UnixNano())
	if _, err = admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(32)
	t.Cleanup(func() { db.Close(); admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); admin.Close() })
	_, err = db.Exec(`CREATE TABLE users(id bigint PRIMARY KEY,balance numeric(20,8) NOT NULL,team_balance numeric(20,8) NOT NULL DEFAULT 0,deleted_at timestamptz,updated_at timestamptz);
 CREATE TABLE teams(id bigint PRIMARY KEY,status text NOT NULL);
 CREATE TABLE team_memberships(user_id bigint PRIMARY KEY REFERENCES users(id),team_id bigint REFERENCES teams(id),status text NOT NULL);
 INSERT INTO teams VALUES(1,'active');`)
	if err != nil {
		t.Fatal(err)
	}
	_, f, _, _ := runtime.Caller(0)
	migration, err := os.ReadFile(filepath.Join(filepath.Dir(f), "../../../migrations/253_wallet_reservations.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(migration)); err != nil {
		t.Fatal(err)
	}
	return Store{DB: db}, u.String()
}
func put(t *testing.T, s Store, id int64, p, tm string, member bool) {
	t.Helper()
	if _, err := s.DB.Exec(`INSERT INTO users(id,balance,team_balance) VALUES($1,$2,$3)`, id, p, tm); err != nil {
		t.Fatal(err)
	}
	if member {
		if _, err := s.DB.Exec(`INSERT INTO team_memberships VALUES($1,1,'active')`, id); err != nil {
			t.Fatal(err)
		}
	}
}
func amount(s string) Amount {
	a, err := ParseAmount(s)
	if err != nil {
		panic(err)
	}
	return a
}
func request(id string, user int64, cost, mode string) Request {
	return Request{ID: id, UserID: user, Fingerprint: "mock-payload-v1", PricingDigest: "mock-price-limits-v1", Ceiling: amount(cost), Mode: mode}
}
func balances(t *testing.T, s Store, id int64, p, tm string) {
	t.Helper()
	var a, b string
	if err := s.DB.QueryRow(`SELECT balance::text,team_balance::text FROM users WHERE id=$1`, id).Scan(&a, &b); err != nil {
		t.Fatal(err)
	}
	if a != amount(p).String() || b != amount(tm).String() {
		t.Fatalf("wallet %d got %s/%s want %s/%s", id, a, b, p, tm)
	}
}

func TestPostgresConcurrentMultiInstanceAndConservation(t *testing.T) {
	s, dsn := fixture(t)
	put(t, s, 1, "0.5", "0.5", true)
	db2, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	db2.SetMaxOpenConns(16)
	stores := []Store{s, {DB: db2}}
	var granted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := stores[i%2].Reserve(context.Background(), request(fmt.Sprint(i), 1, "0.3", "team_first"))
			if err == nil {
				granted.Add(1)
			} else if !errors.Is(err, ErrInsufficient) {
				t.Errorf("reserve: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if granted.Load() != 3 {
		t.Fatalf("granted %d", granted.Load())
	}
	balances(t, s, 1, "0.1", "0")
	var held int64
	if err = s.DB.QueryRow(`SELECT SUM(personal_units+team_units) FROM wallet_reservations WHERE state='reserved'`).Scan(&held); err != nil {
		t.Fatal(err)
	}
	if Amount(held)+amount("0.1") != amount("1") {
		t.Fatal("funds lost")
	}
}
func TestPostgresSettleReleaseReplayAndOverflow(t *testing.T) {
	s, _ := fixture(t)
	put(t, s, 1, "1", "2", true)
	ctx := context.Background()
	r := request("settle", 1, "2.5", "team_first")
	if _, err := s.Reserve(ctx, r); err != nil {
		t.Fatal(err)
	}
	balances(t, s, 1, "0.5", "0")
	if _, err := s.Reserve(ctx, r); !errors.Is(err, ErrReplay) {
		t.Fatalf("retry %v", err)
	}
	if _, err := s.Finalize(ctx, 1, r.ID, amount("2.50000001"), false); !errors.Is(err, ErrExceeded) {
		t.Fatalf("over ceiling %v", err)
	}
	balances(t, s, 1, "0.5", "0")
	var state string
	s.DB.QueryRow(`SELECT state FROM wallet_reservations WHERE request_id=$1`, r.ID).Scan(&state)
	if state != "reserved" {
		t.Fatal(state)
	}
	got, err := s.Finalize(ctx, 1, r.ID, amount("1.25"), false)
	if err != nil || !got.Changed || got.TeamSpent != amount("1.25") || got.PersonalSpent != 0 {
		t.Fatalf("settle %+v %v", got, err)
	}
	balances(t, s, 1, "1", "0.75")
	got, err = s.Finalize(ctx, 1, r.ID, amount("1.25"), false)
	if err != nil || got.Changed {
		t.Fatalf("duplicate %+v %v", got, err)
	}
	if _, err = s.Finalize(ctx, 1, r.ID, 0, true); !errors.Is(err, ErrConflict) {
		t.Fatalf("release settled %v", err)
	}
	if _, err = s.Finalize(ctx, 1, r.ID, amount("1.2"), false); !errors.Is(err, ErrConflict) {
		t.Fatalf("alter charge %v", err)
	}
	r = request("release", 1, "0.5", "personal_first")
	if _, err = s.Reserve(ctx, r); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		_, err = s.Finalize(ctx, 1, r.ID, 0, true)
		if err != nil {
			t.Fatal(err)
		}
	}
	balances(t, s, 1, "1", "0.75")
	if _, err = s.Reserve(ctx, r); !errors.Is(err, ErrReplay) {
		t.Fatal("released request replay accepted")
	}
}
func TestPostgresAtomicWithUsageAndCancellation(t *testing.T) {
	s, _ := fixture(t)
	put(t, s, 1, "2", "0", false)
	ctx := context.Background()
	r := request("stream", 1, "1.5", "personal_only")
	if _, err := s.Reserve(ctx, r); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.Finalize(cancelled, 1, r.ID, 0, true); err == nil {
		t.Fatal("cancelled transaction succeeded")
	}
	balances(t, s, 1, "0.5", "0")
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.FinalizeTx(ctx, tx, 1, r.ID, amount("1"), false); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`SELECT 1/0`); err == nil {
		t.Fatal("expected failure")
	}
	tx.Rollback()
	balances(t, s, 1, "0.5", "0")
	tx, err = s.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.FinalizeTx(ctx, tx, 1, r.ID, amount("1"), false); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`CREATE TABLE mock_usage(request_id text PRIMARY KEY,cost numeric); INSERT INTO mock_usage VALUES('stream',1)`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	balances(t, s, 1, "1", "0")
}
func TestPostgresConcurrentFinalizationAndNoTTL(t *testing.T) {
	s, dsn := fixture(t)
	put(t, s, 1, "3", "0", false)
	ctx := context.Background()
	r := request("long-stream", 1, "2", "personal_only")
	if _, err := s.Reserve(ctx, r); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`UPDATE wallet_reservations SET created_at=now()-interval '7 days'`); err != nil {
		t.Fatal(err)
	}
	db2, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	s2 := Store{DB: db2}
	if _, err := s2.Reserve(ctx, request("another", 1, "1.00000001", "personal_only")); !errors.Is(err, ErrInsufficient) {
		t.Fatalf("lost hold: %v", err)
	}
	var changed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			st := s
			if i%2 == 1 {
				st = s2
			}
			out, err := st.Finalize(ctx, 1, r.ID, amount("0.5"), false)
			if err != nil {
				t.Error(err)
			}
			if out.Changed {
				changed.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if changed.Load() != 1 {
		t.Fatalf("changed %d", changed.Load())
	}
	balances(t, s, 1, "2.5", "0")
}
func TestPostgresHistoricalDebtAndMissingBackendFailClosed(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	put(t, s, 1, "-0.01", "5", true)
	put(t, s, 2, "1", "-0.01", true)
	for _, id := range []int64{1, 2} {
		if _, err := s.Reserve(ctx, request(fmt.Sprint(id), id, "0.1", "team_first")); !errors.Is(err, ErrInsufficient) {
			t.Fatalf("debt user %d %v", id, err)
		}
	}
	for i, p := range []string{"0", "0.1", "0.10000001", "1.99999999", "2"} {
		id := int64(i + 10)
		put(t, s, id, p, "0", false)
		_, err := s.Reserve(ctx, request(p, id, "0.00000001", "personal_only"))
		if (err == nil) != (i >= 2) {
			t.Fatalf("boundary %s: %v", p, err)
		}
	}
	s.DB.Close()
	if _, err := s.Reserve(ctx, request("offline", 10, "0.01", "personal_only")); err == nil {
		t.Fatal("database unavailable granted admission")
	}
}
func TestPostgresCrossUserIDAndModeChanges(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	put(t, s, 1, "1", "2", true)
	put(t, s, 2, "10", "0", false)
	r := request("global-id", 1, "2.5", "personal_first")
	if _, err := s.Reserve(ctx, r); err != nil {
		t.Fatal(err)
	}
	r.UserID = 2
	r.Mode = "personal_only"
	if _, err := s.Reserve(ctx, r); !errors.Is(err, ErrReplay) {
		t.Fatalf("cross-user replay %v", err)
	}
	balances(t, s, 2, "10", "0")
	if _, err := s.Finalize(ctx, 2, r.ID, 0, true); !errors.Is(err, ErrMissing) {
		t.Fatalf("cross user finalize %v", err)
	}
	if _, err := s.DB.Exec(`DELETE FROM team_memberships WHERE user_id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Finalize(ctx, 1, r.ID, amount("1.25"), false); err != nil {
		t.Fatal(err)
	}
	balances(t, s, 1, "0", "1.75")
}
func TestPostgresLedgerConstraintsRejectNullSettlement(t *testing.T) {
	s, _ := fixture(t)
	put(t, s, 1, "2", "0", false)
	if _, err := s.Reserve(context.Background(), request("null", 1, "1", "personal_only")); err != nil {
		t.Fatal(err)
	}
	_, err := s.DB.Exec(`UPDATE wallet_reservations SET state='settled',finalized_at=now() WHERE request_id='null'`)
	if err == nil || !strings.Contains(err.Error(), "check constraint") {
		t.Fatalf("incomplete settlement accepted: %v", err)
	}
}

func TestPostgresReserveRollbackAndFinalizeRollback(t *testing.T) {
	s, _ := fixture(t)
	put(t, s, 1, "2", "0", false)
	ctx := context.Background()
	if _, err := s.DB.Exec(`ALTER TABLE users ADD CONSTRAINT mock_reserve_failure CHECK(balance<>1.5)`); err != nil {
		t.Fatal(err)
	}
	r := request("rollback-reserve", 1, "0.5", "personal_only")
	if _, err := s.Reserve(ctx, r); err == nil {
		t.Fatal("expected wallet update failure")
	}
	balances(t, s, 1, "2", "0")
	var n int
	if err := s.DB.QueryRow(`SELECT count(*) FROM wallet_reservations WHERE request_id=$1`, r.ID).Scan(&n); err != nil || n != 0 {
		t.Fatal("failed reserve persisted a hold")
	}
	if _, err := s.DB.Exec(`ALTER TABLE users DROP CONSTRAINT mock_reserve_failure`); err != nil {
		t.Fatal(err)
	}
	r = request("rollback-finalize", 1, "1", "personal_only")
	if _, err := s.Reserve(ctx, r); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`ALTER TABLE users ADD CONSTRAINT mock_refund_failure CHECK(balance<>1.75)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Finalize(ctx, 1, r.ID, amount("0.25"), false); err == nil {
		t.Fatal("expected refund failure")
	}
	balances(t, s, 1, "1", "0")
	var state string
	if err := s.DB.QueryRow(`SELECT state FROM wallet_reservations WHERE request_id=$1`, r.ID).Scan(&state); err != nil || state != "reserved" {
		t.Fatal("failed refund finalized ledger")
	}
	if _, err := s.DB.Exec(`ALTER TABLE users DROP CONSTRAINT mock_refund_failure`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Finalize(ctx, 1, r.ID, amount("0.25"), false); err != nil {
		t.Fatal(err)
	}
	balances(t, s, 1, "1.75", "0")
}

func TestPostgresConcurrentDuplicateReserveAndConflictingFinalization(t *testing.T) {
	s, dsn := fixture(t)
	put(t, s, 1, "2", "0", false)
	ctx := context.Background()
	db2, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	stores := []Store{s, {DB: db2}}
	var accepted atomic.Int32
	var wg sync.WaitGroup
	r := request("same-id", 1, "1", "personal_only")
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := stores[i%2].Reserve(ctx, r)
			if err == nil {
				accepted.Add(1)
			} else if !errors.Is(err, ErrReplay) {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("accepted duplicate %d times", accepted.Load())
	}
	balances(t, s, 1, "1", "0")
	var finalized atomic.Int32
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			a := amount("0.5")
			release := false
			if i == 1 {
				a = 0
				release = true
			}
			out, err := stores[i].Finalize(ctx, 1, r.ID, a, release)
			if err == nil && out.Changed {
				finalized.Add(1)
			} else if !errors.Is(err, ErrConflict) {
				t.Errorf("race finalization: %v %+v", err, out)
			}
		}(i)
	}
	wg.Wait()
	if finalized.Load() != 1 {
		t.Fatalf("conflicting outcomes changed %d times", finalized.Load())
	}
	var wallet, state string
	var actual int64
	if err = s.DB.QueryRow(`SELECT balance::text FROM users WHERE id=1`).Scan(&wallet); err != nil {
		t.Fatal(err)
	}
	if err = s.DB.QueryRow(`SELECT state,actual_units FROM wallet_reservations WHERE request_id=$1`, r.ID).Scan(&state, &actual); err != nil {
		t.Fatal(err)
	}
	if state != "settled" && state != "released" {
		t.Fatal(state)
	}
	if amount(wallet)+Amount(actual) != amount("2") {
		t.Fatal("release/settle race lost or duplicated money")
	}
}
