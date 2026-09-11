package repository

import (
	"context"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// DeductBalanceByMode is the compatibility-path equivalent of the unified
// usage billing deduction. It deliberately lives beside the concrete user
// repository so legacy callers can opt in without widening UserRepository.
// Independent users always fall back to their personal balance.
func (r *userRepository) DeductBalanceByMode(ctx context.Context, id int64, amount float64, mode string) (service.BalanceDeductionResult, error) {
	if r == nil || r.client == nil {
		return service.BalanceDeductionResult{}, errors.New("user repository is nil")
	}
	if amount <= 0 {
		return service.BalanceDeductionResult{}, nil
	}
	mode = service.NormalizeBalanceMode(mode)
	client := clientFromContext(ctx, r.client)
	rows, err := client.QueryContext(ctx, `
		WITH current_user_row AS (
			SELECT u.id, u.balance, COALESCE(u.team_balance, 0) AS team_balance,
				EXISTS (
					SELECT 1 FROM team_memberships tm
					WHERE tm.user_id = u.id AND tm.status IN ('active', 'exit_pending')
				) AS is_team_member
			FROM users u
			WHERE u.id = $2 AND u.deleted_at IS NULL
			FOR UPDATE
		), split AS (
			SELECT id,
				CASE
					WHEN NOT is_team_member THEN 0::numeric
					WHEN $3::text = 'personal_only' THEN 0::numeric
					WHEN $3::text = 'team_only' THEN $1::numeric
					WHEN $3::text = 'personal_first' THEN $1::numeric - LEAST(GREATEST(balance, 0), $1::numeric)
					ELSE LEAST(GREATEST(team_balance, 0), $1::numeric)
				END AS team_part
			FROM current_user_row
		)
		, updated AS (
			UPDATE users u
			SET balance = u.balance - ($1::numeric - s.team_part),
				team_balance = u.team_balance - s.team_part,
				updated_at = NOW()
			FROM split s
			WHERE u.id = s.id
			RETURNING u.id
		)
		SELECT split.team_part
		FROM updated
		JOIN split ON split.id = updated.id
	`, amount, id, mode)
	if err != nil {
		return service.BalanceDeductionResult{}, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if rowsErr := rows.Err(); rowsErr != nil {
			return service.BalanceDeductionResult{}, rowsErr
		}
		return service.BalanceDeductionResult{}, service.ErrUserNotFound
	}
	var teamPart float64
	if err := rows.Scan(&teamPart); err != nil {
		return service.BalanceDeductionResult{}, err
	}
	return service.BalanceDeductionResult{
		Personal: service.QuantizeUsageBillingAmount(amount - teamPart),
		Team:     service.QuantizeUsageBillingAmount(teamPart),
	}, nil
}

var _ service.BalanceModeDeductor = (*userRepository)(nil)
