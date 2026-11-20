// Package walletdb supplies LOCAL-ONLY PostgreSQL fixtures to financial tests.
package walletdb

import (
	"database/sql"
	"fmt"
	_ "github.com/lib/pq"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func Open(t testing.TB) (*sql.DB, string)           { return open(t, true) }
func OpenHistorical(t testing.TB) (*sql.DB, string) { return open(t, false) }
func open(t testing.TB, guards bool) (*sql.DB, string) {
	t.Helper()
	dsn := os.Getenv("WALLETGUARD_TEST_DSN")
	if dsn == "" {
		t.Skip("WALLETGUARD_TEST_DSN required for local-only PostgreSQL verification")
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if u.Hostname() != "127.0.0.1" || u.Path != "/walletguard_test" {
		t.Fatal("refusing non-loopback/non-test PostgreSQL")
	}
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("wallet_lifecycle_%d", time.Now().UnixNano())
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
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
		if _, err := admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); err != nil {
			t.Error(err)
		}
		if err := admin.Close(); err != nil {
			t.Error(err)
		}
	})
	_, err = db.Exec(`CREATE TABLE users(id bigint PRIMARY KEY,balance numeric(20,8) NOT NULL,team_balance numeric(20,8) NOT NULL DEFAULT 0,deleted_at timestamptz,updated_at timestamptz);
 CREATE TABLE teams(id bigint PRIMARY KEY,status text NOT NULL);
 CREATE TABLE team_memberships(user_id bigint PRIMARY KEY REFERENCES users(id),team_id bigint REFERENCES teams(id),status text NOT NULL);
 CREATE TABLE api_keys(id bigint PRIMARY KEY,quota_used numeric(20,8) NOT NULL DEFAULT 0,quota numeric(20,8) NOT NULL DEFAULT 0,status text NOT NULL DEFAULT 'active',updated_at timestamptz,deleted_at timestamptz);
 INSERT INTO api_keys(id) VALUES(1);
 CREATE TABLE usage_billing_dedup(id bigserial PRIMARY KEY,request_id text NOT NULL,api_key_id bigint NOT NULL,request_fingerprint text NOT NULL,UNIQUE(request_id,api_key_id));
 CREATE TABLE usage_billing_dedup_archive(LIKE usage_billing_dedup INCLUDING ALL);
 INSERT INTO teams VALUES(1,'active');`)
	if err != nil {
		t.Fatal(err)
	}
	_, f, _, _ := runtime.Caller(0)
	wire, err := os.ReadFile(filepath.Join(filepath.Dir(f), "../../../migrations/253_wallet_reservations.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(wire)); err != nil {
		t.Fatal(err)
	}
	if guards {
		wire, err = os.ReadFile(filepath.Join(filepath.Dir(f), "../../../migrations/254_wallet_nonnegative_writes.sql"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(string(wire)); err != nil {
			t.Fatal(err)
		}
	}
	return db, u.String()
}
