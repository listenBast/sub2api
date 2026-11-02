package dto

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUsageLogFromService_DeletedAPIKeyKeepsNameAndFlagButNoKeyValue(t *testing.T) {
	ts := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	log := &service.UsageLog{ID: 1, UserID: 2, APIKeyID: 3, APIKey: &service.APIKey{ID: 3, UserID: 2, Name: "gzt", DeletedAt: &ts}}

	for name, out := range map[string]any{"user": UsageLogFromService(log), "admin": UsageLogFromServiceAdmin(log)} {
		raw, err := json.Marshal(out)
		require.NoError(t, err, name)
		var body struct {
			APIKey map[string]any `json:"api_key"`
		}
		require.NoError(t, json.Unmarshal(raw, &body), name)
		require.Equal(t, "gzt", body.APIKey["name"], name)
		require.NotEmpty(t, body.APIKey["deleted_at"], name)
		require.Equal(t, "", body.APIKey["key"], name)
		require.NotContains(t, string(raw), "__deleted__", name)
	}
}

func TestAPIKeyFromService_ActiveKeyHasNoDeletedAt(t *testing.T) {
	raw, err := json.Marshal(APIKeyFromService(&service.APIKey{ID: 1, Name: "live", Key: "sk-live"}))
	require.NoError(t, err)
	require.NotContains(t, string(raw), "deleted_at")
}
