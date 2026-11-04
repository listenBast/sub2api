package service

import (
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

func TestTeamUsageClientModel_PrefersRequestedModel(t *testing.T) {
	requested := "gpt-6.1-sol"
	blank := "  "
	require.Equal(t, "gpt-6.1-sol", teamUsageClientModel(&dbent.UsageLog{Model: "gpt-6-luna", RequestedModel: &requested}))
	require.Equal(t, "gpt-6-luna", teamUsageClientModel(&dbent.UsageLog{Model: "gpt-6-luna"}))
	require.Equal(t, "gpt-6-luna", teamUsageClientModel(&dbent.UsageLog{Model: "gpt-6-luna", RequestedModel: &blank}))
}
