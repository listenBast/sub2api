package walletguard

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrInsufficient = errors.New("walletguard: insufficient available balance or balance at/below 0.1")
	ErrReplay       = errors.New("walletguard: request ID already exists; do not forward again")
	ErrConflict     = errors.New("walletguard: conflicting finalization")
	ErrMissing      = errors.New("walletguard: reservation not found")
	ErrExceeded     = errors.New("walletguard: actual cost exceeds proven ceiling; hold retained")
	ErrInvalid      = errors.New("walletguard: invalid reservation")
)

// Store uses PostgreSQL users row locks as the cross-process serialization point.
// DB errors never grant admission. No Redis dependency and no TTL-based release.
type Store struct{ DB *sql.DB }

type Request struct {
	APIKeyID    int64
	ID          string
	UserID      int64
	Fingerprint string
	// PricingDigest must identify the immutable prices, multipliers, routing and
	// protocol limits used to PROVE Ceiling. This package cannot prove those bounds.
	PricingDigest string
	Mode          string
	Ceiling       Amount
}

type Result struct {
	PersonalHeld, TeamHeld   Amount
	PersonalSpent, TeamSpent Amount
	State                    string
	Changed                  bool
}

func validText(s string) bool { return strings.TrimSpace(s) == s && len(s) > 0 && len(s) <= 200 }
func validMode(m string) bool {
	return m == "personal_only" || m == "team_only" || m == "personal_first" || m == "team_first"
}

// split reserves the complete ceiling or nothing, respecting the selected pools.
func split(personal, team Amount, member bool, mode string, ceiling Amount) (Amount, Amount, error) {
	if personal < 0 || team < 0 || ceiling <= 0 || !validMode(mode) {
		return 0, 0, ErrInvalid
	}
	if !member {
		team = 0
		if mode == "team_only" {
			return 0, 0, ErrInsufficient
		}
	}
	var available Amount
	var err error
	switch mode {
	case "personal_only":
		available = personal
	case "team_only":
		available = team
	default:
		available, err = add(personal, team)
	}
	if err != nil {
		return 0, 0, err
	}
	if available <= Floor || available < ceiling {
		return 0, 0, ErrInsufficient
	}
	switch mode {
	case "personal_only":
		return ceiling, 0, nil
	case "team_only":
		return 0, ceiling, nil
	case "personal_first":
		p := min(personal, ceiling)
		return p, ceiling - p, nil
	default:
		t := min(team, ceiling)
		return ceiling - t, t, nil
	}
}

// Reserve removes the full proven ceiling from available funds, atomically with
// the durable ledger entry. A repeated ID always fails closed (even after a
// successful commit whose response was lost); it is NOT permission to re-forward.
func (s Store) Reserve(ctx context.Context, r Request) (out Result, err error) {
	if s.DB == nil || r.UserID <= 0 || !validText(r.ID) || !validText(r.Fingerprint) || !validText(r.PricingDigest) || !validMode(r.Mode) || r.Ceiling <= 0 {
		return out, ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback() }()
	var pText, tText string
	var member bool
	err = tx.QueryRowContext(ctx, `SELECT balance::text,COALESCE(team_balance,0)::text FROM users WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, r.UserID).Scan(&pText, &tText)
	if err != nil {
		return out, err
	}
	// Read membership in a FRESH statement after obtaining the wallet lock; a
	// membership change committed while waiting cannot leave a stale EXISTS result.
	var teamID sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT tm.team_id FROM team_memberships tm JOIN teams t ON t.id=tm.team_id
 WHERE tm.user_id=$1 AND tm.status IN ('active','exit_pending') AND t.status='active' FOR SHARE OF tm,t`, r.UserID).Scan(&teamID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	member = teamID.Valid
	// Historical negative balances are blocked, never normalized or forgiven.
	if strings.HasPrefix(pText, "-") || strings.HasPrefix(tText, "-") {
		return out, ErrInsufficient
	}
	p, err := ParseAmount(pText)
	if err != nil {
		return out, err
	}
	t, err := ParseAmount(tText)
	if err != nil {
		return out, err
	}
	// Check the ID before checking today's available balance, so a retry is
	// classified as a replay even when the first reserve consumed all funds.
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM wallet_reservations WHERE request_id=$1)`, r.ID).Scan(&exists); err != nil {
		return out, err
	}
	if exists {
		return out, ErrReplay
	}
	out.PersonalHeld, out.TeamHeld, err = split(p, t, member, r.Mode, r.Ceiling)
	if err != nil {
		return Result{}, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO wallet_reservations
 (request_id,user_id,fingerprint,pricing_digest,balance_mode,personal_units,team_units,api_key_id,team_id)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(request_id) DO NOTHING`,
		r.ID, r.UserID, r.Fingerprint, r.PricingDigest, r.Mode, out.PersonalHeld, out.TeamHeld, r.APIKeyID, teamID)
	if err != nil {
		return Result{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Result{}, err
	}
	if n != 1 {
		return Result{}, ErrReplay
	}
	res, err = tx.ExecContext(ctx, `UPDATE users SET balance=balance-$2::numeric,team_balance=COALESCE(team_balance,0)-$3::numeric,updated_at=now()
 WHERE id=$1 AND balance >= $2::numeric AND COALESCE(team_balance,0) >= $3::numeric`, r.UserID, out.PersonalHeld.String(), out.TeamHeld.String())
	if err != nil {
		return Result{}, err
	}
	n, err = res.RowsAffected()
	if err != nil {
		return Result{}, err
	}
	if n != 1 {
		return Result{}, ErrInsufficient
	}
	if err = tx.Commit(); err != nil {
		return Result{}, err
	}
	out.State = "reserved"
	out.Changed = true
	return out, nil
}

// Finalize is useful for a verified no-charge release. Production usage billing
// MUST use FinalizeTx inside the SAME transaction as dedup, usage/account/key
// effects, instead of committing this and then writing a separate usage record.
// A caller may release ONLY after proving no further charge can arrive. Client
// cancellation, process death and a timeout are not such proof.
func (s Store) Finalize(ctx context.Context, userID int64, id string, actual Amount, release bool) (out Result, err error) {
	if s.DB == nil {
		return out, ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback() }()
	out, err = s.FinalizeTx(ctx, tx, userID, id, actual, release)
	if err != nil {
		return Result{}, err
	}
	if err = tx.Commit(); err != nil {
		return Result{}, err
	}
	return out, nil
}

// FinalizeTx validates the complete charge before refunding the unused ceiling.
// Over-ceiling costs are an invariant violation: return an error and retain all
// funds for reconciliation. Never clamp, double-debit, or silently absorb them.
func (s Store) FinalizeTx(ctx context.Context, tx *sql.Tx, userID int64, id string, actual Amount, release bool) (out Result, err error) {
	if tx == nil || userID <= 0 || !validText(id) || actual < 0 || (release && actual != 0) {
		return out, ErrInvalid
	}
	var locked int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&locked); err != nil {
		return out, err
	}
	var mode string
	var dispatched sql.NullTime
	var noCharge sql.NullString
	var recorded, spentP, spentT sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT balance_mode,personal_units,team_units,state,actual_units,spent_personal_units,spent_team_units,dispatched_at,no_charge_reason
 FROM wallet_reservations WHERE request_id=$1 AND user_id=$2 FOR UPDATE`, id, userID).
		Scan(&mode, &out.PersonalHeld, &out.TeamHeld, &out.State, &recorded, &spentP, &spentT, &dispatched, &noCharge)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrMissing
	}
	if err != nil {
		return out, err
	}
	target := "settled"
	if release {
		target = "released"
	}
	if out.State != "reserved" {
		if out.State == target && recorded.Valid && Amount(recorded.Int64) == actual {
			out.PersonalSpent = Amount(spentP.Int64)
			out.TeamSpent = Amount(spentT.Int64)
			return out, nil
		}
		return Result{}, ErrConflict
	}
	if release && dispatched.Valid && !noCharge.Valid {
		return Result{}, ErrDispatched
	}
	total, err := add(out.PersonalHeld, out.TeamHeld)
	if err != nil {
		return Result{}, err
	}
	if actual > total {
		return Result{}, ErrExceeded
	}
	if mode == "personal_only" || mode == "personal_first" {
		out.PersonalSpent = min(out.PersonalHeld, actual)
		out.TeamSpent = actual - out.PersonalSpent
	} else {
		out.TeamSpent = min(out.TeamHeld, actual)
		out.PersonalSpent = actual - out.TeamSpent
	}
	// Refund to the original pools, even if membership or key mode changed. This
	// is NOT a fresh team allocation and must not charge the owner a second time.
	if _, err = tx.ExecContext(ctx, `UPDATE users SET balance=balance+$2::numeric,team_balance=COALESCE(team_balance,0)+$3::numeric,updated_at=now() WHERE id=$1`,
		userID, (out.PersonalHeld - out.PersonalSpent).String(), (out.TeamHeld - out.TeamSpent).String()); err != nil {
		return Result{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE wallet_reservations SET state=$2,actual_units=$3,spent_personal_units=$4,spent_team_units=$5,finalized_at=now() WHERE request_id=$1`,
		id, target, actual, out.PersonalSpent, out.TeamSpent); err != nil {
		return Result{}, fmt.Errorf("finalize wallet ledger: %w", err)
	}
	out.State = target
	out.Changed = true
	return out, nil
}

// ErrDispatched means a no-charge release is not proven safe. An upstream timeout
// or disconnect is not evidence that the upstream did no billable work.
var ErrDispatched = errors.New("walletguard: dispatched outcome needs reconciliation; automatic release refused")

// MarkDispatched is durable BEFORE the first network send. Repeated calls are
// safe for bounded internal failover under the same fixed-price invocation.
func (s Store) MarkDispatched(ctx context.Context, userID int64, id string) error {
	if s.DB == nil || userID <= 0 || !validText(id) {
		return ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var user int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&user); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE wallet_reservations SET dispatched_at=COALESCE(dispatched_at,now()) WHERE user_id=$1 AND request_id=$2 AND state='reserved'`, userID, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrConflict
	}
	return tx.Commit()
}

// ReleaseConfirmedHTTPFailure records a specific non-chargeable protocol outcome
// and refunds in the same transaction. Never call it on transport/cancellation/
// timeout errors; those outcomes are unknown and retain their full reservation.
func (s Store) ReleaseConfirmedHTTPFailure(ctx context.Context, userID int64, id string) (Result, error) {
	return s.releaseProvenNoCharge(ctx, userID, id, "confirmed_http_non_2xx")
}

// ReleaseBeforeNetwork may only be used by the live invocation while it can
// prove it never called the network forwarder. Never infer this after a crash.
func (s Store) ReleaseBeforeNetwork(ctx context.Context, userID int64, id string) (Result, error) {
	return s.releaseProvenNoCharge(ctx, userID, id, "confirmed_no_network_send")
}
func (s Store) releaseProvenNoCharge(ctx context.Context, userID int64, id, reason string) (Result, error) {
	if s.DB == nil || userID <= 0 || !validText(id) {
		return Result{}, ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var user int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&user); err != nil {
		return Result{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE wallet_reservations SET no_charge_reason=$3 WHERE request_id=$1 AND user_id=$2 AND state IN ('reserved','released')`, id, userID, reason); err != nil {
		return Result{}, err
	}
	out, err := s.FinalizeTx(ctx, tx, userID, id, 0, true)
	if err != nil {
		return Result{}, err
	}
	if err = tx.Commit(); err != nil {
		return Result{}, err
	}
	return out, nil
}

// CheckAvailable is the DB-authoritative floor check for an explicitly free
// operation. It does not invent a positive charge or reserve money for it.
func (s Store) CheckAvailable(ctx context.Context, userID int64, mode string) error {
	if s.DB == nil || userID <= 0 || !validMode(mode) {
		return ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var pText, tText string
	var member bool
	if err = tx.QueryRowContext(ctx, `SELECT balance::text,COALESCE(team_balance,0)::text FROM users WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, userID).Scan(&pText, &tText); err != nil {
		return err
	}
	if strings.HasPrefix(pText, "-") || strings.HasPrefix(tText, "-") {
		return ErrInsufficient
	}
	p, err := ParseAmount(pText)
	if err != nil {
		return err
	}
	t, err := ParseAmount(tText)
	if err != nil {
		return err
	}
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM team_memberships tm JOIN teams t ON t.id=tm.team_id WHERE tm.user_id=$1 AND tm.status IN ('active','exit_pending') AND t.status='active')`, userID).Scan(&member); err != nil {
		return err
	}
	_, _, err = split(p, t, member, mode, 1)
	return err
}
