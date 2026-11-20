package walletguard

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func runRepair(t *testing.T, dsn string, apply, maintenance bool) (string, error) {
	t.Helper()
	if _, err := exec.LookPath("psql"); err != nil {
		t.Fatal("psql required for opted-in database tests")
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	schema := q.Get("search_path")
	q.Del("search_path")
	q.Set("options", "-csearch_path="+schema)
	u.RawQuery = q.Encode()
	_, f, _, _ := runtime.Caller(0)
	script := filepath.Join(filepath.Dir(f), "../../../../scripts/operations/repair_team_personal_debt_20261120.sql")
	if _, err := os.Stat(script); err != nil {
		t.Fatal(err)
	}
	// This is exclusively the fixture's loopback-only walletguard_test database.
	cmd := exec.Command("psql", "-X", u.String(), "-v", fmt.Sprintf("apply=%t", apply), "-v", fmt.Sprintf("maintenance_confirmed=%t", maintenance), "-v", "reviewed_ids={11,12,13,14}", "-v", "reviewed_team_id=1", "-v", "batch_id=synthetic-test-batch-v1", "-f", script)
	cmd.Env = append(os.Environ(), "PGCONNECT_TIMEOUT=3")
	out, err := cmd.CombinedOutput()
	return string(out), err
}
func repairFixture(t *testing.T) (Store, string) {
	s, dsn := fixture(t)
	for _, u := range []struct {
		id    int64
		p, tm string
	}{
		{11, "-1.2", "10"}, {12, "-0.3", "20"},
		{13, "-0.04", "30"}, {14, "-0.005", "40"},
	} {
		put(t, s, u.id, u.p, u.tm, true)
	}
	// Unrelated historical debts and owner-like account must remain untouched.
	put(t, s, 21, "-0.01", "0", false)
	put(t, s, 22, "0", "-0.02", true)
	put(t, s, 23, "50", "0", false)
	return s, dsn
}
func TestRepairAuditApplyIdempotenceConservation(t *testing.T) {
	s, dsn := repairFixture(t)
	if out, err := runRepair(t, dsn, false, false); err != nil {
		t.Fatalf("audit %v %s", err, out)
	}
	var exists bool
	if err := s.DB.QueryRow(`SELECT to_regclass('wallet_balance_repairs') IS NOT NULL`).Scan(&exists); err != nil || exists {
		t.Fatal("read-only audit wrote a table")
	}
	if out, err := runRepair(t, dsn, true, false); err == nil || !strings.Contains(out, "REFUSED") {
		t.Fatalf("missing gate %v %s", err, out)
	}
	var before, after string
	s.DB.QueryRow(`SELECT sum(balance+team_balance)::text FROM users WHERE id IN (11,12,13,14)`).Scan(&before)
	for i := 0; i < 2; i++ {
		if out, err := runRepair(t, dsn, true, true); err != nil {
			t.Fatalf("apply %d %v %s", i, err, out)
		}
	}
	s.DB.QueryRow(`SELECT sum(balance+team_balance)::text FROM users WHERE id IN (11,12,13,14)`).Scan(&after)
	if before != after {
		t.Fatalf("assets changed %s -> %s", before, after)
	}
	balances(t, s, 11, "0", "8.8")
	balances(t, s, 12, "0", "19.7")
	balances(t, s, 13, "0", "29.96")
	balances(t, s, 14, "0", "39.995")
	balances(t, s, 23, "50", "0")
	var debt string
	var count int
	s.DB.QueryRow(`SELECT count(*),sum(debt)::text FROM wallet_balance_repairs`).Scan(&count, &debt)
	if count != 4 || debt != "1.54500000" {
		t.Fatalf("audit %d %s", count, debt)
	}
	var p, tm string
	s.DB.QueryRow(`SELECT balance::text,team_balance::text FROM users WHERE id=21`).Scan(&p, &tm)
	if p != "-0.01000000" || tm != "0.00000000" {
		t.Fatal("nonmember debt changed")
	}
	s.DB.QueryRow(`SELECT balance::text,team_balance::text FROM users WHERE id=22`).Scan(&p, &tm)
	if p != "0.00000000" || tm != "-0.02000000" {
		t.Fatal("team debt changed")
	}
}
func TestRepairRecomputesCurrentAmounts(t *testing.T) {
	s, dsn := repairFixture(t)
	if _, err := s.DB.Exec(`UPDATE users SET balance=-1.8 WHERE id=11`); err != nil {
		t.Fatal(err)
	}
	if out, err := runRepair(t, dsn, true, true); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	balances(t, s, 11, "0", "8.2")
}
func TestRepairInsufficiencyOrAffiliationDriftRollsBack(t *testing.T) {
	for _, change := range []string{
		`UPDATE users SET team_balance=0 WHERE id=12`,
		`UPDATE team_memberships SET status='invited' WHERE user_id=12`,
		`INSERT INTO users(id,balance,team_balance) VALUES(999,-1,2);INSERT INTO team_memberships VALUES(999,1,'active')`,
		`UPDATE users SET balance=0 WHERE id=13`,
	} {
		t.Run(change, func(t *testing.T) {
			s, dsn := repairFixture(t)
			if _, err := s.DB.Exec(change); err != nil {
				t.Fatal(err)
			}
			if out, err := runRepair(t, dsn, true, true); err == nil || !strings.Contains(out, "ERROR:") {
				t.Fatalf("expected SQL safety rejection, got %v %s", err, out)
			}
			var personal string
			s.DB.QueryRow(`SELECT balance::text FROM users WHERE id=11`).Scan(&personal)
			if personal != "-1.20000000" {
				t.Fatal("partial correction committed")
			}
			var exists bool
			s.DB.QueryRow(`SELECT to_regclass('wallet_balance_repairs') IS NOT NULL`).Scan(&exists)
			if exists {
				t.Fatal("failed repair committed DDL/audit")
			}
		})
	}
}
