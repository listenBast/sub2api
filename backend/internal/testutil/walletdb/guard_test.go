package walletdb

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/pkg/walletguard"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestEveryWalletWriterRejectsNewDebtAndPreservesHistoricalDebt(t *testing.T) {
	db, _ := OpenHistorical(t)
	if _, err := db.Exec(`INSERT INTO users(id,balance,team_balance) VALUES(1,1,2),(2,-1,-0.5)`); err != nil {
		t.Fatal(err)
	}
	_, f, _, _ := runtime.Caller(0)
	wire, err := os.ReadFile(filepath.Join(filepath.Dir(f), "../../../migrations/254_wallet_nonnegative_writes.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(wire)); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`UPDATE users SET balance=-0.01 WHERE id=1`, `UPDATE users SET team_balance=-0.01 WHERE id=1`, `UPDATE users SET balance=balance-2 WHERE id=1`, `UPDATE users SET balance=-1.1 WHERE id=2`, `UPDATE users SET team_balance=-0.6 WHERE id=2`, `INSERT INTO users(id,balance) VALUES(3,-0.01)`, `UPDATE users SET balance='NaN'::numeric WHERE id=1`, `UPDATE users SET team_balance='NaN'::numeric WHERE id=1`} {
		if _, err = db.Exec(q); err == nil {
			t.Fatalf("overdraft allowed: %s", q)
		}
	}
	if _, err = db.Exec(`UPDATE users SET balance=-0.9,team_balance=-0.4 WHERE id=2`); err != nil {
		t.Fatal("repayment must remain possible", err)
	}
	if _, err = db.Exec(`UPDATE users SET balance=0,team_balance=0 WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE users SET balance=0.1,team_balance=0 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	var p, tm string
	if err = db.QueryRow(`SELECT balance::text,team_balance::text FROM users WHERE id=1`).Scan(&p, &tm); err != nil {
		t.Fatal(err)
	}
	if p != "0.10000000" || tm != "0.00000000" {
		t.Fatal("unexpected balance", p, tm)
	}
}

func TestPendingTeamAllocationCannotChangeMembership(t *testing.T) {
	db, _ := Open(t)
	if _, err := db.Exec(`INSERT INTO users(id,balance,team_balance) VALUES(1,1,2),(2,1,0);INSERT INTO team_memberships VALUES(1,1,'active');INSERT INTO teams VALUES(2,'active')`); err != nil {
		t.Fatal(err)
	}
	store := walletguard.Store{DB: db}
	amount, _ := walletguard.ParseAmount("0.5")
	if _, err := store.Reserve(context.Background(), walletguard.Request{ID: "pending-team", UserID: 1, Fingerprint: "synthetic", PricingDigest: "synthetic", Mode: "team_only", Ceiling: amount}); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`DELETE FROM team_memberships WHERE user_id=1`, `UPDATE team_memberships SET team_id=2 WHERE user_id=1`, `UPDATE team_memberships SET status='left' WHERE user_id=1`, `UPDATE team_memberships SET user_id=2 WHERE user_id=1`} {
		if _, err := db.Exec(q); err == nil {
			t.Fatalf("allowed membership change with reserved funds: %s", q)
		}
	}
	if _, err := db.Exec(`UPDATE team_memberships SET status='exit_pending' WHERE user_id=1`); err != nil {
		t.Fatal(err)
	}
	var team int
	if err := db.QueryRow(`SELECT team_id FROM wallet_reservations`).Scan(&team); err != nil || team != 1 {
		t.Fatal("missing original team identity", err)
	}
	if _, err := store.Finalize(context.Background(), 1, "pending-team", amount, false); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM team_memberships WHERE user_id=1`); err != nil {
		t.Fatal("settled funds must not block leaving", err)
	}
}
