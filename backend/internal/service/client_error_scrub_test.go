//go:build unit

package service

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newScrubTestContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	return c
}

func TestScrubClientErrorText_PreservesUpstreamModelNames(t *testing.T) {
	c := newScrubTestContext()
	acc := &Account{
		ID:       1,
		Name:     "internal-relay-account",
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"base_url":      "https://relay.example-upstream.com/v1",
			"model_mapping": map[string]any{"gpt-6.1-sol": "gpt-6-luna"},
		},
	}
	RegisterClientScrubAccount(c, acc, "gpt-6.1-sol")

	got := ScrubClientErrorText(c, `The model 'gpt-6-luna' does not exist; also gpt-6-luna-mini is overloaded.`)
	require.Equal(t, `The model 'gpt-6-luna' does not exist; also gpt-6-luna-mini is overloaded.`, got)
}

func TestScrubClientErrorText_NoStateIsNoop(t *testing.T) {
	c := newScrubTestContext()
	require.Equal(t, "hello gpt-5", ScrubClientErrorText(c, "hello gpt-5"))
	require.False(t, ClientErrorScrubActive(c))
	require.Equal(t, "x", ScrubClientErrorText(nil, "x"))
}

func TestRegisterClientScrubAccount_RedactsBaseURLAndNamePreservesModel(t *testing.T) {
	c := newScrubTestContext()
	acc := &Account{
		ID:       1,
		Name:     "internal-relay-account",
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"base_url":      "https://relay.example-upstream.com/v1",
			"model_mapping": map[string]any{"gpt-6.1-sol": "gpt-6-luna"},
		},
	}
	RegisterClientScrubAccount(c, acc, "gpt-6.1-sol")

	got := ScrubClientErrorText(c, `Post "https://relay.example-upstream.com/v1/responses": model gpt-6-luna not found (account internal-relay-account)`)
	require.Equal(t, `Post "upstream/responses": model gpt-6-luna not found (account upstream)`, got)
}

func TestRegisterClientScrubAccount_NoMappingKeepsText(t *testing.T) {
	c := newScrubTestContext()
	acc := &Account{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	RegisterClientScrubAccount(c, acc, "gpt-5")
	require.False(t, ClientErrorScrubActive(c))
}

func TestScrubOpenAIWSClientErrorPayload(t *testing.T) {
	c := newScrubTestContext()
	acc := &Account{
		ID:       1,
		Name:     "internal-relay-account",
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"base_url": "https://relay.example-upstream.com/v1",
		},
	}
	RegisterClientScrubAccount(c, acc, "gpt-6.1-sol")

	errFrame := []byte(`{"type":"error","error":{"message":"model gpt-6-luna overloaded at https://relay.example-upstream.com/v1"}}`)
	require.JSONEq(t, `{"type":"error","error":{"message":"model gpt-6-luna overloaded at upstream"}}`, string(scrubOpenAIWSClientErrorPayload(c, errFrame)))

	delta := []byte(`{"type":"response.output_text.delta","delta":"https://relay.example-upstream.com/v1 is mentioned"}`)
	require.Equal(t, string(delta), string(scrubOpenAIWSClientErrorPayload(c, delta)))
}
