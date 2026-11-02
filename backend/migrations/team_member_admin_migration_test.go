package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTeamMemberAdminMigrationIsAdditiveAndIdempotent(t *testing.T) {
	sqlBytes, err := FS.ReadFile("250_team_member_admin.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(strings.ToLower(string(sqlBytes))), " ")

	// 只新增一个带默认值的非空列：对已有数据安全（所有现有成员默认不是管理员），可重复执行。
	require.Contains(t, sql, "alter table team_memberships add column if not exists is_admin boolean not null default false")
	require.NotContains(t, sql, "drop ")
	require.NotContains(t, sql, "delete ")
	require.NotContains(t, sql, "update ")
}
