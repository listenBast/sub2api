package repository

import (
	"context"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// DeductBalanceByMode is the compatibility-path equivalent of the unified
// usage billing deduction. It deliberately lives beside the concrete user
// repository so legacy callers can opt in without widening UserRepository.
// Independent users always fall back to their personal balance. It shares
// teamUsageBillingDeductSQL, so it never overdraws either wallet.
func (r *userRepository) DeductBalanceByMode(ctx context.Context, id int64, amount float64, mode string) (service.BalanceDeductionResult, error) {
	if r == nil || r.client == nil {
		return service.BalanceDeductionResult{}, errors.New("user repository is nil")
	}
	if amount <= 0 {
		return service.BalanceDeductionResult{}, nil
	}
	mode = service.NormalizeBalanceMode(mode)
	client := clientFromContext(ctx, r.client)
	rows, err := client.QueryContext(ctx, teamUsageBillingDeductSQL, amount, id, mode)
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
	var out usageBillingBalanceDeduction
	if err := rows.Scan(&out.newBalance, &out.newTeamBalance, &out.teamPart, &out.personalPart, &out.uncovered); err != nil {
		return service.BalanceDeductionResult{}, err
	}
	if err := rows.Err(); err != nil {
		return service.BalanceDeductionResult{}, err
	}
	out.normalize()
	return service.BalanceDeductionResult{
		Personal:  out.personalPart,
		Team:      out.teamPart,
		Uncovered: out.uncovered,
	}, nil
}

var _ service.BalanceModeDeductor = (*userRepository)(nil)
