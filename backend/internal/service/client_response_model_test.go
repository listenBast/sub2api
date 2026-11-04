//go:build unit

package service

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestSetAnthropicPublicModel_OnlyWhenDifferent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	setAnthropicPublicModel(c, "gpt-6.1-sol", "gpt-6-luna")
	require.Equal(t, "gpt-6.1-sol", anthropicPublicModel(c))

	c2, _ := gin.CreateTestContext(httptest.NewRecorder())
	setAnthropicPublicModel(c2, "same", "same")
	require.Empty(t, anthropicPublicModel(c2))
	setAnthropicPublicModel(c2, "", "x")
	require.Empty(t, anthropicPublicModel(c2))
	require.Empty(t, anthropicPublicModel(nil))
}

func TestRewriteAnthropicBodyModel(t *testing.T) {
	body := []byte(`{"id":"msg_1","model":"claude-upstream","content":[{"type":"text","text":"claude-upstream"}]}`)
	out := rewriteAnthropicBodyModel(body, "claude-requested")
	require.Equal(t, "claude-requested", gjson.GetBytes(out, "model").String())
	// content untouched
	require.Equal(t, "claude-upstream", gjson.GetBytes(out, "content.0.text").String())
	// empty public model / invalid JSON are no-ops
	require.Equal(t, string(body), string(rewriteAnthropicBodyModel(body, "")))
	require.Equal(t, "not json", string(rewriteAnthropicBodyModel([]byte("not json"), "x")))
}

func TestRewriteAnthropicSSELineModel(t *testing.T) {
	line := `data: {"type":"message_start","message":{"id":"m","model":"claude-upstream"}}`
	out := rewriteAnthropicSSELineModel(line, "claude-requested")
	require.Contains(t, out, `"model":"claude-requested"`)
	require.NotContains(t, out, "claude-upstream")
	require.True(t, len(out) > 6 && out[:6] == "data: ")

	// non-data lines and empty public model unchanged
	require.Equal(t, "event: message_start", rewriteAnthropicSSELineModel("event: message_start", "x"))
	require.Equal(t, line, rewriteAnthropicSSELineModel(line, ""))
	delta := `data: {"type":"content_block_delta","delta":{"text":"hi"}}`
	require.Equal(t, delta, rewriteAnthropicSSELineModel(delta, "x"))
}

func TestRewriteAnthropicEventDataModel(t *testing.T) {
	data := []byte(`{"type":"message_start","message":{"model":"anthropic.claude-upstream-v1:0"}}`)
	out := rewriteAnthropicEventDataModel(data, "claude-requested")
	require.Equal(t, "claude-requested", gjson.GetBytes(out, "message.model").String())
	require.Equal(t, string(data), string(rewriteAnthropicEventDataModel(data, "")))
}

func TestRewriteGeminiBodyModelVersion(t *testing.T) {
	body := []byte(`{"modelVersion":"gemini-upstream","candidates":[]}`)
	out := rewriteGeminiBodyModelVersion(body, "gemini-requested")
	require.Equal(t, "gemini-requested", gjson.GetBytes(out, "modelVersion").String())

	wrapped := []byte(`{"response":{"modelVersion":"gemini-upstream"}}`)
	out = rewriteGeminiBodyModelVersion(wrapped, "gemini-requested")
	require.Equal(t, "gemini-requested", gjson.GetBytes(out, "response.modelVersion").String())

	require.Equal(t, string(body), string(rewriteGeminiBodyModelVersion(body, "")))
}

func TestScrubUpstreamModelText(t *testing.T) {
	cases := []struct {
		name, text, upstream, requested, want string
	}{
		{"plain", "model gpt-6-luna not found", "gpt-6-luna", "gpt-6.1-sol", "model gpt-6.1-sol not found"},
		{"quoted", `The model "gpt-6-luna" does not exist.`, "gpt-6-luna", "gpt-6.1-sol", `The model "gpt-6.1-sol" does not exist.`},
		{"trailing period", "unsupported model gpt-6-luna.", "gpt-6-luna", "gpt-6.1-sol", "unsupported model gpt-6.1-sol."},
		{"prefix of longer name kept", "gpt-5.4 and gpt-5", "gpt-5", "alias", "gpt-5.4 and alias"},
		{"suffix of longer name kept", "xgpt-5 gpt-5-mini", "gpt-5", "alias", "xgpt-5 gpt-5-mini"},
		{"same model noop", "gpt-5 error", "gpt-5", "gpt-5", "gpt-5 error"},
		{"empty upstream noop", "gpt-5 error", "", "alias", "gpt-5 error"},
		{"empty requested noop", "gpt-5 error", "gpt-5", "", "gpt-5 error"},
		{"multiple", "gpt-6-luna / gpt-6-luna", "gpt-6-luna", "r", "r / r"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, ScrubUpstreamModelText(tc.text, tc.upstream, tc.requested))
			require.Equal(t, tc.want, string(scrubUpstreamModelName([]byte(tc.text), tc.upstream, tc.requested)))
		})
	}
}
