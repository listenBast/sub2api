//go:build unit

package handler

import (
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSetOpsSelectedAccountFor_RegistersRoutingDetailsNotModelNames(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	setOpsRequestContext(c, "gpt-6.1-sol", false)
	acc := &service.Account{
		ID: 7, Name: "internal-relay-account", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Credentials: map[string]any{
			"base_url":      "https://relay.example-upstream.com/v1",
			"model_mapping": map[string]any{"gpt-6.1-sol": "gpt-6-luna"},
		},
	}
	setOpsSelectedAccountFor(c, acc)
	require.Equal(t, "model gpt-6-luna missing at upstream", service.ScrubClientErrorText(c, "model gpt-6-luna missing at https://relay.example-upstream.com/v1"))
}
