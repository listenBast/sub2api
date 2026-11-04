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

func TestScrubClientErrorText_ReplacesMappedModelAcrossAttempts(t *testing.T) {
	c := newScrubTestContext()
	RegisterClientScrubRequestedModel(c, "gpt-6.1-sol")
	RegisterClientScrubUpstreamModel(c, "gpt-6-luna")
	RegisterClientScrubUpstreamModel(c, "gpt-6-luna-mini")

	got := ScrubClientErrorText(c, `The model 'gpt-6-luna' does not exist; also gpt-6-luna-mini is overloaded, try gpt-6-lunar.`)
	require.Equal(t, `The model 'gpt-6.1-sol' does not exist; also gpt-6.1-sol is overloaded, try gpt-6-lunar.`, got)
}

func TestScrubClientErrorText_GeminiModelsPrefix(t *testing.T) {
	c := newScrubTestContext()
	RegisterClientScrubRequestedModel(c, "gemini-pro-x")
	RegisterClientScrubUpstreamModel(c, "gemini-2.5-pro")
	got := ScrubClientErrorText(c, `models/gemini-2.5-pro is not found for API version v1beta`)
	require.Equal(t, `models/gemini-pro-x is not found for API version v1beta`, got)
}

func TestScrubClientErrorText_NoRequestedModelUsesGenericName(t *testing.T) {
	c := newScrubTestContext()
	RegisterClientScrubUpstreamModel(c, "secret-model-7")
	require.Equal(t, "bad the requested model", ScrubClientErrorText(c, "bad secret-model-7"))
}

func TestScrubClientErrorText_NoStateIsNoop(t *testing.T) {
	c := newScrubTestContext()
	require.Equal(t, "hello gpt-5", ScrubClientErrorText(c, "hello gpt-5"))
	require.False(t, ClientErrorScrubActive(c))
	require.Equal(t, "x", ScrubClientErrorText(nil, "x"))
}

func TestRegisterClientScrubAccount_MappedModelBaseURLAndName(t *testing.T) {
	c := newScrubTestContext()
	RegisterClientScrubRequestedModel(c, "gpt-6.1-sol")
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
	require.Equal(t, `Post "upstream/responses": model gpt-6.1-sol not found (account upstream)`, got)
}

func TestRegisterClientScrubAccount_NoMappingKeepsText(t *testing.T) {
	c := newScrubTestContext()
	acc := &Account{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	RegisterClientScrubAccount(c, acc, "gpt-5")
	require.False(t, ClientErrorScrubActive(c))
}

func TestScrubOpenAIWSClientErrorPayload(t *testing.T) {
	c := newScrubTestContext()
	RegisterClientScrubRequestedModel(c, "gpt-6.1-sol")
	RegisterClientScrubUpstreamModel(c, "gpt-6-luna")

	errFrame := []byte(`{"type":"error","error":{"message":"model gpt-6-luna overloaded"}}`)
	require.JSONEq(t, `{"type":"error","error":{"message":"model gpt-6.1-sol overloaded"}}`, string(scrubOpenAIWSClientErrorPayload(c, errFrame)))

	delta := []byte(`{"type":"response.output_text.delta","delta":"gpt-6-luna is my name"}`)
	require.Equal(t, string(delta), string(scrubOpenAIWSClientErrorPayload(c, delta)))
}
