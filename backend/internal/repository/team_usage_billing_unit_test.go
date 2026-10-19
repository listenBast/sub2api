//go:build unit

package repository

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// 团队模式（sub2api-custom）下余额扣费在同一条 UPDATE 中拆分个人余额与团队额度。
const splitBalanceDeductSQL = `(?s)WITH current_user_row AS .*FROM users u.*FOR UPDATE.*UPDATE users u.*RETURNING u\.id, u\.balance, u\.team_balance.*SELECT updated\.balance, updated\.team_balance, split\.team_part, split\.personal_part,.*AS uncovered`

var splitBalanceDeductColumns = []string{"balance", "team_balance", "team_part", "personal_part", "uncovered"}

// expectTeamSplitBalanceDeduct 供上游 applyUsageBillingEffects 测试使用：非团队成员、未设置 BalanceMode（team_first），
// 只扣个人余额（team_part = 0）。不透支：newBalance 传负数表示余额不够，此时 SQL 只扣到 0，
// 差额作为 uncovered 返回（扣后余额为 0）。
func expectTeamSplitBalanceDeduct(mock sqlmock.Sqlmock, amount, newBalance float64) {
	personalPart, uncovered := amount, 0.0
	if newBalance < 0 {
		uncovered = -newBalance
		personalPart = amount - uncovered
		newBalance = 0
	}
	mock.ExpectQuery(splitBalanceDeductSQL).
		WithArgs(amount, int64(42), service.BalanceModeTeamFirst).
		WillReturnRows(sqlmock.NewRows(splitBalanceDeductColumns).AddRow(newBalance, 0.0, 0.0, personalPart, uncovered))
}

func newTeamUsageBillingTx(t *testing.T) (context.Context, *sql.DB, sqlmock.Sqlmock, *sql.Tx) {
	t.Helper()
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectBegin()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	return ctx, db, mock, tx
}

func TestTeamUsageBillingDeduct_DefaultModeDeductsPersonalPoolForNonMember(t *testing.T) {
	ctx, _, mock, tx := newTeamUsageBillingTx(t)
	// 非团队成员：team_balance 为 0，team_first 退化为只扣个人余额（team_part = 0）。
	mock.ExpectQuery(splitBalanceDeductSQL).
		WithArgs(2.5, int64(42), service.BalanceModeTeamFirst).
		WillReturnRows(sqlmock.NewRows(splitBalanceDeductColumns).AddRow(7.5, 0.0, 0.0, 2.5, 0.0))
	mock.ExpectCommit()

	deduction, err := deductTeamUsageBillingBalance(ctx, tx, 42, 2.5, "")
	require.NoError(t, err)
	require.True(t, deduction.sufficient)
	require.InDelta(t, 7.5, deduction.newBalance, 0.000001)
	require.InDelta(t, 2.5, deduction.personalPart, 0.000001)
	require.InDelta(t, 0, deduction.teamPart, 0.000001)
	require.NoError(t, tx.Commit())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTeamUsageBillingDeduct_TeamFirstSplitsAcrossPools(t *testing.T) {
	ctx, _, mock, tx := newTeamUsageBillingTx(t)
	// 团队额度只剩 4，其余 6 从个人余额扣：两个资金池都未透支。
	mock.ExpectQuery(splitBalanceDeductSQL).
		WithArgs(10.0, int64(42), service.BalanceModeTeamFirst).
		WillReturnRows(sqlmock.NewRows(splitBalanceDeductColumns).AddRow(14.0, 0.0, 4.0, 6.0, 0.0))
	mock.ExpectCommit()

	deduction, err := deductTeamUsageBillingBalance(ctx, tx, 42, 10, service.BalanceModeTeamFirst)
	require.NoError(t, err)
	require.True(t, deduction.sufficient)
	require.InDelta(t, 4.0, deduction.teamPart, 0.000001)
	require.InDelta(t, 6.0, deduction.personalPart, 0.000001)
	require.InDelta(t, 14.0, deduction.newBalance, 0.000001)
	require.InDelta(t, 0, deduction.newTeamBalance, 0.000001)
	require.NoError(t, tx.Commit())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTeamUsageBillingDeduct_ClampsInsteadOfOverdraft(t *testing.T) {
	ctx, _, mock, tx := newTeamUsageBillingTx(t)
	// team_only：团队额度只有 4，扣到 0 为止，剩余 6 不扣（uncovered），不会出现负数。
	mock.ExpectQuery(splitBalanceDeductSQL).
		WithArgs(10.0, int64(42), service.BalanceModeTeamOnly).
		WillReturnRows(sqlmock.NewRows(splitBalanceDeductColumns).AddRow(20.0, 0.0, 4.0, 0.0, 6.0))
	mock.ExpectCommit()

	deduction, err := deductTeamUsageBillingBalance(ctx, tx, 42, 10, service.BalanceModeTeamOnly)
	require.NoError(t, err)
	require.False(t, deduction.sufficient)
	require.InDelta(t, 0, deduction.newTeamBalance, 0.000001)
	require.InDelta(t, 20.0, deduction.newBalance, 0.000001)
	require.InDelta(t, 4.0, deduction.teamPart, 0.000001)
	require.InDelta(t, 0, deduction.personalPart, 0.000001)
	require.InDelta(t, 6.0, deduction.uncovered, 0.000001)
	require.NoError(t, tx.Commit())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTeamUsageBillingDeduct_ReturnsUserNotFoundWhenNoUserUpdated(t *testing.T) {
	ctx, _, mock, tx := newTeamUsageBillingTx(t)
	mock.ExpectQuery(splitBalanceDeductSQL).
		WithArgs(10.0, int64(42), service.BalanceModeTeamFirst).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	_, err := deductTeamUsageBillingBalance(ctx, tx, 42, 10, service.BalanceModeTeamFirst)
	require.ErrorIs(t, err, service.ErrUserNotFound)
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTeamUsageBillingApply_FillsTeamSplitIntoResult(t *testing.T) {
	ctx, _, mock, tx := newTeamUsageBillingTx(t)
	// 团队成员 team_first：个人 0、团队 15，扣 3 → 全部从团队额度扣。
	mock.ExpectQuery(splitBalanceDeductSQL).
		WithArgs(3.0, int64(42), service.BalanceModeTeamFirst).
		WillReturnRows(sqlmock.NewRows(splitBalanceDeductColumns).AddRow(0.0, 12.0, 3.0, 0.0, 0.0))
	mock.ExpectCommit()

	result := &service.UsageBillingApplyResult{Applied: true}
	err := (&usageBillingRepository{}).applyUsageBillingEffects(ctx, tx, &service.UsageBillingCommand{
		UserID:      42,
		BalanceCost: 3,
		BalanceMode: service.BalanceModeTeamFirst,
	}, result)
	require.NoError(t, err)
	require.NotNil(t, result.NewBalance)
	require.InDelta(t, 0, *result.NewBalance, 0.000001)
	require.NotNil(t, result.NewTeamBalance)
	require.InDelta(t, 12.0, *result.NewTeamBalance, 0.000001)
	require.InDelta(t, 0, result.PersonalBalanceCost, 0.000001)
	require.InDelta(t, 3.0, result.TeamBalanceCost, 0.000001)
	require.False(t, result.BalanceOverdrafted)
	require.NoError(t, tx.Commit())
	require.NoError(t, mock.ExpectationsWereMet())
}
