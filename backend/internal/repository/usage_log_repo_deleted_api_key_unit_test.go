package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

func TestUsageLogHydrate_SoftDeletedAPIKeyKeepsNameWithoutKeyValue(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:usage_log_deleted_key?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })

	u := mustCreateAPIKeyRepoUser(t, ctx, client, "usage-deleted-key@example.com")
	live, err := client.APIKey.Create().SetUserID(u.ID).SetKey("sk-live-value").SetName("live-key").Save(ctx)
	require.NoError(t, err)
	gone, err := client.APIKey.Create().SetUserID(u.ID).SetKey("sk-secret-value").SetName("gzt").Save(ctx)
	require.NoError(t, err)

	// 通过真实的删除路径软删除：key 被改写成 __deleted__ 墓碑，deleted_at 被设置。
	keyRepo := &apiKeyRepository{client: client, sql: db}
	require.NoError(t, keyRepo.Delete(ctx, gone.ID))
	_, err = client.APIKey.Get(ctx, gone.ID)
	require.Error(t, err, "the soft-deleted key must stay invisible to ordinary queries")

	repo := &usageLogRepository{client: client}
	logs := []service.UsageLog{
		{ID: 1, UserID: u.ID, APIKeyID: live.ID, CreatedAt: time.Now()},
		{ID: 2, UserID: u.ID, APIKeyID: gone.ID, CreatedAt: time.Now()},
	}
	require.NoError(t, repo.hydrateUsageLogAssociations(ctx, logs))

	require.NotNil(t, logs[0].APIKey)
	require.Equal(t, "live-key", logs[0].APIKey.Name)
	require.Nil(t, logs[0].APIKey.DeletedAt)
	require.Equal(t, "sk-live-value", logs[0].APIKey.Key, "live keys keep the existing behavior")

	require.NotNil(t, logs[1].APIKey, "a deleted key must still resolve for the usage record")
	require.Equal(t, "gzt", logs[1].APIKey.Name)
	require.NotNil(t, logs[1].APIKey.DeletedAt)
	require.Empty(t, logs[1].APIKey.Key, "neither the original key nor the tombstone may leak")
	require.Equal(t, gone.ID, logs[1].APIKey.ID)
	require.Equal(t, u.ID, logs[1].APIKey.UserID)
}
