//go:build unit

package repository

import (
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// 不透支（sub2api-custom: team billing）：Go 侧按 SQL 返回的拆分结果填写 result，
// uncovered > 0 时 sufficient=false / BalanceOverdrafted=true，但余额本身不为负。
func TestTeamNoOverdraft_ApplyFillsSplitAndUncovered(t *testing.T) {
	for _, tc := range noOverdraftCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _, mock, tx := newTeamUsageBillingTx(t)
			mock.ExpectQuery(splitBalanceDeductSQL).
				WithArgs(tc.amount, int64(42), service.NormalizeBalanceMode(tc.mode)).
				WillReturnRows(sqlmock.NewRows([]string{"balance", "team_balance", "team_part", "personal_part", "uncovered"}).
					AddRow(tc.wantBalance, tc.wantTeamBalance, tc.wantTeamPart, tc.wantPersonalPart, tc.wantUncovered))
			mock.ExpectCommit()

			result := &service.UsageBillingApplyResult{Applied: true}
			err := (&usageBillingRepository{}).applyUsageBillingEffects(ctx, tx, &service.UsageBillingCommand{
				UserID:      42,
				BalanceCost: tc.amount,
				BalanceMode: tc.mode,
			}, result)
			require.NoError(t, err)
			require.NotNil(t, result.NewBalance)
			require.NotNil(t, result.NewTeamBalance)
			require.InDelta(t, tc.wantBalance, *result.NewBalance, 1e-9)
			require.InDelta(t, tc.wantTeamBalance, *result.NewTeamBalance, 1e-9)
			require.InDelta(t, tc.wantPersonalPart, result.PersonalBalanceCost, 1e-9)
			require.InDelta(t, tc.wantTeamPart, result.TeamBalanceCost, 1e-9)
			require.InDelta(t, tc.wantUncovered, result.UncoveredBalanceCost, 1e-9)
			require.Equal(t, !tc.wantSufficient(), result.BalanceOverdrafted)
			require.InDelta(t, tc.amount, result.PersonalBalanceCost+result.TeamBalanceCost+result.UncoveredBalanceCost, 1e-8,
				"charged + uncovered must always equal the actual cost")
			require.NoError(t, tx.Commit())
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestTeamNoOverdraft_NormalizeQuantizesAndClampsUncovered(t *testing.T) {
	d := usageBillingBalanceDeduction{personalPart: 1.123456781, teamPart: 2.000000004, uncovered: 1e-12}
	d.normalize()
	require.InDelta(t, 1.12345678, d.personalPart, 1e-12)
	require.InDelta(t, 2.0, d.teamPart, 1e-12)
	require.Zero(t, d.uncovered)
	require.True(t, d.sufficient)

	d = usageBillingBalanceDeduction{uncovered: -0.5}
	d.normalize()
	require.Zero(t, d.uncovered)
	require.True(t, d.sufficient)

	d = usageBillingBalanceDeduction{uncovered: 0.00000001}
	d.normalize()
	require.InDelta(t, 0.00000001, d.uncovered, 1e-12)
	require.False(t, d.sufficient)
}

// SQL 守护：扣减量只能来自 GREATEST(...,0) 封顶后的可用额度，不能再出现上游那种无条件 balance - $1。
func TestTeamNoOverdraft_SQLNeverSubtractsUncappedAmount(t *testing.T) {
	sqlText := strings.Join(strings.Fields(teamUsageBillingDeductSQL), " ")
	require.Contains(t, sqlText, "GREATEST(u.balance, 0) AS personal_avail")
	require.Contains(t, sqlText, "GREATEST(COALESCE(u.team_balance, 0), 0) AS team_avail")
	require.Contains(t, sqlText, "SET balance = u.balance - s.personal_part")
	require.Contains(t, sqlText, "team_balance = u.team_balance - s.team_part")
	require.NotRegexp(t, regexp.MustCompile(`balance\s*-\s*\(?\$1`), sqlText)
	// 拆分的每一项都必须被可用额度封顶（team_part / personal_part 各两个分支，加上两处“先扣的那部分”）。
	require.Equal(t, 3, strings.Count(sqlText, "LEAST(team_avail,"))
	require.Equal(t, 3, strings.Count(sqlText, "LEAST(personal_avail,"))
}
