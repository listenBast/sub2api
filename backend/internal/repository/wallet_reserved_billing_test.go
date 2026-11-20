package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/pkg/walletguard"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/testutil/walletdb"
	"github.com/stretchr/testify/require"
	"sync"
	"sync/atomic"
	"testing"
)

func reservedCommand(t *testing.T, store walletguard.Store, id, cost string) *service.UsageBillingCommand {
	t.Helper()
	a, err := walletguard.ParseAmount(cost)
	require.NoError(t, err)
	_, err = store.Reserve(context.Background(), walletguard.Request{ID: id, UserID: 1, APIKeyID: 1, Fingerprint: "synthetic-payload", PricingDigest: "fixed-mock-price", Mode: "team_first", Ceiling: a})
	require.NoError(t, err)
	require.NoError(t, store.MarkDispatched(context.Background(), 1, id))
	cmd := &service.UsageBillingCommand{RequestID: "wallet:" + id, APIKeyID: 1, UserID: 1, AccountID: 1, RequestPayloadHash: "synthetic-payload", WalletReservationID: id, WalletActualUnits: int64(a)}
	switch cost {
	case "0.4":
		cmd.BalanceCost = 0.4
	case "0.3":
		cmd.BalanceCost = 0.3
	default:
		t.Fatal("test cost not defined")
	}
	cmd.Normalize()
	return cmd
}
func TestReservedWalletApplyAndApplyBatchNeverDoubleDeduct(t *testing.T) {
	for _, batch := range []bool{false, true} {
		t.Run(map[bool]string{true: "batch", false: "single"}[batch], func(t *testing.T) {
			db, _ := walletdb.Open(t)
			_, err := db.Exec(`INSERT INTO users(id,balance,team_balance) VALUES(1,1,2);INSERT INTO team_memberships VALUES(1,1,'active')`)
			require.NoError(t, err)
			store := walletguard.Store{DB: db}
			repo := &usageBillingRepository{db: db}
			a := reservedCommand(t, store, "a", "0.4")
			b := reservedCommand(t, store, "b", "0.3")
			for repeat := 0; repeat < 2; repeat++ {
				if batch {
					out, err := repo.ApplyBatch(context.Background(), []*service.UsageBillingCommand{a, b})
					require.NoError(t, err)
					for _, o := range out {
						require.Equal(t, repeat == 0, o.Result.Applied)
					}
				} else {
					for _, cmd := range []*service.UsageBillingCommand{a, b} {
						out, err := repo.Apply(context.Background(), cmd)
						require.NoError(t, err)
						require.Equal(t, repeat == 0, out.Applied)
					}
				}
			}
			var p, tm string
			require.NoError(t, db.QueryRow(`SELECT balance::text,team_balance::text FROM users WHERE id=1`).Scan(&p, &tm))
			require.Equal(t, "1.00000000", p)
			require.Equal(t, "1.30000000", tm)
			var count int
			require.NoError(t, db.QueryRow(`SELECT count(*) FROM usage_billing_dedup`).Scan(&count))
			require.Equal(t, 2, count)
			require.NoError(t, db.QueryRow(`SELECT count(*) FROM wallet_billing_outbox`).Scan(&count))
			require.Zero(t, count)
		})
	}
}
func TestReservedWalletOutboxRecoveryAfterFailedTransaction(t *testing.T) {
	db, dsn := walletdb.Open(t)
	_, err := db.Exec(`INSERT INTO users(id,balance) VALUES(1,2);ALTER TABLE api_keys ADD CONSTRAINT fixture_quota_failure CHECK(quota_used<0.3)`)
	require.NoError(t, err)
	store := walletguard.Store{DB: db}
	cmd := reservedCommand(t, store, "recover", "0.4")
	cmd.APIKeyQuotaCost = 0.4
	cmd.RequestFingerprint = ""
	cmd.Normalize()
	repo := &usageBillingRepository{db: db}
	_, err = repo.Apply(context.Background(), cmd)
	require.Error(t, err)
	var state, p string
	require.NoError(t, db.QueryRow(`SELECT state FROM wallet_reservations`).Scan(&state))
	require.Equal(t, "reserved", state)
	require.NoError(t, db.QueryRow(`SELECT balance::text FROM users WHERE id=1`).Scan(&p))
	require.Equal(t, "1.60000000", p)
	var n int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM usage_billing_dedup`).Scan(&n))
	require.Zero(t, n)
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM wallet_billing_outbox`).Scan(&n))
	require.Equal(t, 1, n)
	_, err = db.Exec(`ALTER TABLE api_keys DROP CONSTRAINT fixture_quota_failure`)
	require.NoError(t, err)
	// A brand-new repository/connection pool can recover the durable command.
	freshDB, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, freshDB.Close()) }()
	fresh := &usageBillingRepository{db: freshDB}
	n, err = fresh.RecoverWalletBilling(context.Background(), 10)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	n, err = repo.RecoverWalletBilling(context.Background(), 10)
	require.NoError(t, err)
	require.Zero(t, n)
	require.NoError(t, db.QueryRow(`SELECT balance::text FROM users WHERE id=1`).Scan(&p))
	require.Equal(t, "1.60000000", p)
	var q string
	require.NoError(t, db.QueryRow(`SELECT quota_used::text FROM api_keys WHERE id=1`).Scan(&q))
	require.Equal(t, "0.40000000", q)
}

func TestReservedWalletConcurrentMultiInstanceSingleBatchAndRecovery(t *testing.T) {
	db, dsn := walletdb.Open(t)
	_, err := db.Exec(`INSERT INTO users(id,balance,team_balance) VALUES(1,1,2);INSERT INTO team_memberships VALUES(1,1,'active')`)
	require.NoError(t, err)
	db2, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, db2.Close()) }()
	db.SetMaxOpenConns(12)
	db2.SetMaxOpenConns(12)
	repos := []*usageBillingRepository{{db: db}, {db: db2}}
	original := reservedCommand(t, walletguard.Store{DB: db}, "concurrent-settle", "0.4")
	original.APIKeyQuotaCost = .4
	original.RequestFingerprint = ""
	original.Normalize()
	require.NoError(t, repos[0].EnqueueWalletBilling(context.Background(), original))
	var applied atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			repo := repos[i%2]
			cmd := *original
			if i%3 == 0 {
				_, err := repo.RecoverWalletBilling(context.Background(), 1)
				if err != nil {
					t.Error(err)
				}
				return
			}
			if i%3 == 1 {
				r, err := repo.Apply(context.Background(), &cmd)
				if err != nil {
					t.Error(err)
				} else if r.Applied {
					applied.Add(1)
				}
				return
			}
			out, err := repo.ApplyBatch(context.Background(), []*service.UsageBillingCommand{&cmd})
			if err != nil {
				t.Error(err)
			} else if out[0].Result.Applied {
				applied.Add(1)
			}
		}(i)
	}
	wg.Wait()
	require.LessOrEqual(t, applied.Load(), int32(1)) // a recovery worker can win
	var p, tm, q, state string
	var count int
	var wire []byte
	require.NoError(t, db.QueryRow(`SELECT balance::text,team_balance::text FROM users WHERE id=1`).Scan(&p, &tm))
	require.Equal(t, "1.00000000", p)
	require.Equal(t, "1.60000000", tm)
	require.NoError(t, db.QueryRow(`SELECT quota_used::text FROM api_keys WHERE id=1`).Scan(&q))
	require.Equal(t, "0.40000000", q)
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM usage_billing_dedup`).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM wallet_billing_outbox`).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, db.QueryRow(`SELECT state,billing_receipt FROM wallet_reservations`).Scan(&state, &wire))
	require.Equal(t, "settled", state)
	var receipt service.UsageBillingCommand
	require.NoError(t, json.Unmarshal(wire, &receipt))
	require.EqualValues(t, 40000000, receipt.WalletActualUnits)
	require.Equal(t, original.RequestFingerprint, receipt.RequestFingerprint)
}
