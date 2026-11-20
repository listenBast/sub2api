package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/walletguard"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *userRepository) WalletStore() walletguard.Store {
	db, _ := r.sql.(*sql.DB)
	return walletguard.Store{DB: db}
}

// EnqueueWalletBilling persists a successful money event before the process-local
// lane/worker can lose it. Do not reset a lease on retries or overwrite a command.
func (r *usageBillingRepository) EnqueueWalletBilling(ctx context.Context, cmd *service.UsageBillingCommand) error {
	if cmd == nil || cmd.WalletReservationID == "" {
		return errors.New("wallet reservation identity required")
	}
	cmd.Normalize()
	actual := walletguard.Amount(cmd.WalletActualUnits)
	if actual <= 0 || strconv.FormatFloat(cmd.BalanceCost, 'f', 8, 64) != actual.String() || cmd.SubscriptionCost != 0 || cmd.RequestID != "wallet:"+cmd.WalletReservationID {
		return walletguard.ErrInvalid
	}
	wire, err := json.Marshal(cmd)
	if err != nil {
		return err
	}
	var matches bool
	err = r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM wallet_reservations
 WHERE request_id=$1 AND user_id=$2 AND api_key_id=$3 AND fingerprint=$4
 AND personal_units+team_units=$5 AND dispatched_at IS NOT NULL AND state IN ('reserved','settled'))`,
		cmd.WalletReservationID, cmd.UserID, cmd.APIKeyID, cmd.RequestPayloadHash, cmd.WalletActualUnits).Scan(&matches)
	if err != nil {
		return err
	}
	if !matches {
		return walletguard.ErrConflict
	}
	// Keep terminal rows out of the outbox. A retry racing settlement can reinsert
	// after deletion; the idempotent recovery transaction will remove it safely.
	_, err = r.db.ExecContext(ctx, `INSERT INTO wallet_billing_outbox(reservation_id,fingerprint,command)
 VALUES($1,$2,$3::jsonb) ON CONFLICT(reservation_id) DO NOTHING`, cmd.WalletReservationID, cmd.RequestFingerprint, string(wire))
	if err != nil {
		return err
	}
	var fingerprint string
	err = r.db.QueryRowContext(ctx, `SELECT fingerprint FROM wallet_billing_outbox WHERE reservation_id=$1`, cmd.WalletReservationID).Scan(&fingerprint)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	} // another worker already settled
	if err != nil {
		return err
	}
	if fingerprint != cmd.RequestFingerprint {
		return service.ErrUsageBillingRequestConflict
	}
	return nil
}

func settleReservedWallet(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand, result *service.UsageBillingApplyResult, duplicate bool) error {
	amount := walletguard.Amount(cmd.WalletActualUnits)
	if amount <= 0 || strconv.FormatFloat(cmd.BalanceCost, 'f', 8, 64) != amount.String() || cmd.SubscriptionCost != 0 || cmd.RequestID != "wallet:"+cmd.WalletReservationID {
		return walletguard.ErrInvalid
	}
	finalized, err := (walletguard.Store{}).FinalizeTx(ctx, tx, cmd.UserID, cmd.WalletReservationID, amount, false)
	if err != nil {
		return err
	}
	if duplicate && finalized.Changed {
		return walletguard.ErrConflict
	}
	if !duplicate {
		// Monetary audit survives a lost best-effort usage_logs write. The command
		// contains IDs/counters/pricing, never request bodies or API credentials.
		receipt, err := json.Marshal(cmd)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE wallet_reservations SET billing_receipt=$2::jsonb WHERE request_id=$1`, cmd.WalletReservationID, string(receipt)); err != nil {
			return err
		}
		result.PrepaidBalance = true
		var p, t float64
		if err = tx.QueryRowContext(ctx, `SELECT balance,team_balance FROM users WHERE id=$1`, cmd.UserID).Scan(&p, &t); err != nil {
			return err
		}
		result.NewBalance = &p
		result.NewTeamBalance = &t
		result.PersonalBalanceCost, _ = strconv.ParseFloat(finalized.PersonalSpent.String(), 64)
		result.TeamBalanceCost, _ = strconv.ParseFloat(finalized.TeamSpent.String(), 64)
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM wallet_billing_outbox WHERE reservation_id=$1 AND fingerprint=$2`, cmd.WalletReservationID, cmd.RequestFingerprint)
	return err
}

// RecoverWalletBilling claims durable commands across processes with expiring
// leases. No ledger funds expire. Apply includes dedup and outbox deletion in the
// same transaction, so an unknown commit or concurrent recovery cannot re-debit.
func (r *usageBillingRepository) RecoverWalletBilling(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 128 {
		return 0, walletguard.ErrInvalid
	}
	rows, err := r.db.QueryContext(ctx, `WITH ready AS (
 SELECT reservation_id FROM wallet_billing_outbox WHERE retry_after<=now()
 AND (lease_until IS NULL OR lease_until<now()) ORDER BY created_at LIMIT $1 FOR UPDATE SKIP LOCKED
 ) UPDATE wallet_billing_outbox o SET lease_until=now()+interval '60 seconds'
 FROM ready WHERE o.reservation_id=ready.reservation_id RETURNING o.reservation_id,o.command`, limit)
	if err != nil {
		return 0, err
	}
	type item struct {
		id   string
		wire []byte
	}
	var items []item
	for rows.Next() {
		var i item
		if err = rows.Scan(&i.id, &i.wire); err != nil {
			_ = rows.Close()
			return 0, err
		}
		items = append(items, i)
	}
	err = rows.Err()
	if closeErr := rows.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return 0, err
	}
	done := 0
	failed := 0
	for _, i := range items {
		var cmd service.UsageBillingCommand
		err = json.Unmarshal(i.wire, &cmd)
		if err == nil {
			_, err = r.Apply(ctx, &cmd)
		}
		if err != nil {
			failed++
			// Never store raw error strings that could contain unrelated SQL parameters.
			if _, e := r.db.ExecContext(ctx, `UPDATE wallet_billing_outbox SET attempts=attempts+1,retry_after=now()+interval '30 seconds',lease_until=NULL WHERE reservation_id=$1`, i.id); e != nil {
				return done, e
			}
			continue
		}
		done++
	}
	if failed > 0 {
		return done, fmt.Errorf("wallet outbox: %d command(s) remain pending for retry", failed)
	}
	return done, nil
}
