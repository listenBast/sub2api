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
	defer tx.Rollback()
	var pText, tText string
	var member bool
	err = tx.QueryRowContext(ctx, `SELECT balance::text, COALESCE(team_balance,0)::text,
 EXISTS(SELECT 1 FROM team_memberships tm JOIN teams t ON t.id=tm.team_id
 WHERE tm.user_id=u.id AND tm.status IN ('active','exit_pending') AND t.status='active')
 FROM users u WHERE u.id=$1 AND u.deleted_at IS NULL FOR UPDATE`, r.UserID).Scan(&pText, &tText, &member)
	if err != nil {
		return out, err
	}
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
 (request_id,user_id,fingerprint,pricing_digest,balance_mode,personal_units,team_units)
 VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(request_id) DO NOTHING`,
		r.ID, r.UserID, r.Fingerprint, r.PricingDigest, r.Mode, out.PersonalHeld, out.TeamHeld)
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
	defer tx.Rollback()
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
	var recorded, spentP, spentT sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT balance_mode,personal_units,team_units,state,actual_units,spent_personal_units,spent_team_units
 FROM wallet_reservations WHERE request_id=$1 AND user_id=$2 FOR UPDATE`, id, userID).
		Scan(&mode, &out.PersonalHeld, &out.TeamHeld, &out.State, &recorded, &spentP, &spentT)
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
