//go:build unit

package handler

import (
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSetOpsSelectedAccountFor_RegistersMappedModelForClientScrub(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	setOpsRequestContext(c, "gpt-6.1-sol", false)
	acc := &service.Account{
		ID: 7, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Credentials: map[string]any{"model_mapping": map[string]any{"gpt-6.1-sol": "gpt-6-luna"}},
	}
	setOpsSelectedAccountFor(c, acc)
	require.Equal(t, "model gpt-6.1-sol missing", service.ScrubClientErrorText(c, "model gpt-6-luna missing"))
}
