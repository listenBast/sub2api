package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTeamBalanceSplitMigrationIsIdempotentAndKeepsTeamOnlyBackfillScoped(t *testing.T) {
	sqlBytes, err := FS.ReadFile("238_team_balance_split.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(sqlBytes))

	require.Contains(t, sql, "alter table users add column if not exists team_balance")
	require.Contains(t, sql, "alter table api_keys add column if not exists balance_mode")
	require.Contains(t, sql, "check (balance_mode in ('team_first', 'personal_first', 'team_only', 'personal_only'))")
	require.Contains(t, sql, "from team_memberships m")
	require.Contains(t, sql, "m.status in ('active', 'exit_pending')")
	// The one-time backfill converts balances created by the pre-split team
	// implementation into team quota; it must never touch individual users.
	require.Contains(t, sql, "set team_balance = u.balance")
	require.Contains(t, sql, "balance = 0")
}
