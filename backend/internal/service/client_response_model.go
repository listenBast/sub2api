package service

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ctxKeyAnthropicPublicModel stores the client-requested model for Anthropic
// style pass-through responses so the upstream (account-mapped) model name is
// never echoed back to the API caller.
const ctxKeyAnthropicPublicModel = "anthropic_public_model"

func setAnthropicPublicModel(c *gin.Context, requested, upstream string) {
	if c == nil {
		return
	}
	requested = strings.TrimSpace(requested)
	if requested == "" || requested == strings.TrimSpace(upstream) {
		return
	}
	c.Set(ctxKeyAnthropicPublicModel, requested)
}

func anthropicPublicModel(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if v, ok := c.Get(ctxKeyAnthropicPublicModel); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// rewriteAnthropicBodyModel rewrites only the protocol-level model field of an
// Anthropic message JSON body to publicModel.
func rewriteAnthropicBodyModel(body []byte, publicModel string) []byte {
	if publicModel == "" || !gjson.ValidBytes(body) {
		return body
	}
	if m := gjson.GetBytes(body, "model"); m.Type == gjson.String && m.String() != publicModel {
		if out, err := sjson.SetBytes(body, "model", publicModel); err == nil {
			return out
		}
	}
	return body
}

// rewriteAnthropicSSELineModel rewrites model / message.model in an SSE data
// line (message_start event) to publicModel. Other lines are returned as is.
func rewriteAnthropicSSELineModel(line, publicModel string) string {
	if publicModel == "" {
		return line
	}
	data, ok := extractAnthropicSSEDataLine(line)
	if !ok || !gjson.Valid(data) {
		return line
	}
	updated := data
	for _, path := range []string{"message.model", "model"} {
		if m := gjson.Get(updated, path); m.Type == gjson.String && m.String() != publicModel {
			next, err := sjson.Set(updated, path, publicModel)
			if err != nil {
				return line
			}
			updated = next
		}
	}
	if updated == data {
		return line
	}
	return "data: " + updated
}

// scrubUpstreamModelName replaces occurrences of the upstream (mapped) model
// name inside text/bytes that is about to be shown to a non-admin audience
// (API caller error bodies, user-facing error logs) with the requested model.
func scrubUpstreamModelName(body []byte, upstreamModel, requestedModel string) []byte {
	if len(body) == 0 {
		return body
	}
	return []byte(ScrubUpstreamModelText(string(body), upstreamModel, requestedModel))
}

// ScrubUpstreamModelText is the string form of scrubUpstreamModelName.
func ScrubUpstreamModelText(text, upstreamModel, requestedModel string) string {
	upstreamModel = strings.TrimSpace(upstreamModel)
	requestedModel = strings.TrimSpace(requestedModel)
	if text == "" || upstreamModel == "" || requestedModel == "" || upstreamModel == requestedModel {
		return text
	}
	if !strings.Contains(text, upstreamModel) {
		return text
	}
	var b strings.Builder
	rest := text
	for {
		i := strings.Index(rest, upstreamModel)
		if i < 0 {
			b.WriteString(rest)
			break
		}
		end := i + len(upstreamModel)
		beforeOK := i == 0 || !isModelNameChar(rest[i-1])
		afterOK := end >= len(rest) || !isModelNameChar(rest[end]) ||
			(rest[end] == '.' && (end+1 >= len(rest) || !isModelNameChar(rest[end+1])))
		b.WriteString(rest[:i])
		if beforeOK && afterOK {
			b.WriteString(requestedModel)
		} else {
			b.WriteString(upstreamModel)
		}
		rest = rest[end:]
	}
	return b.String()
}

func isModelNameChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' || c == '/' || c == ':'
}

// rewriteGeminiBodyModelVersion rewrites the Gemini protocol-level modelVersion
// field (also response.modelVersion for wrapped bodies) to publicModel.
func rewriteGeminiBodyModelVersion(body []byte, publicModel string) []byte {
	if publicModel == "" || !gjson.ValidBytes(body) {
		return body
	}
	out := body
	for _, path := range []string{"modelVersion", "response.modelVersion"} {
		if m := gjson.GetBytes(out, path); m.Type == gjson.String && m.String() != publicModel {
			if next, err := sjson.SetBytes(out, path, publicModel); err == nil {
				out = next
			}
		}
	}
	return out
}

// rewriteAnthropicEventDataModel rewrites model / message.model in a bare JSON
// Anthropic stream event payload.
func rewriteAnthropicEventDataModel(data []byte, publicModel string) []byte {
	if publicModel == "" || !gjson.ValidBytes(data) {
		return data
	}
	out := data
	for _, path := range []string{"message.model", "model"} {
		if m := gjson.GetBytes(out, path); m.Type == gjson.String && m.String() != publicModel {
			if next, err := sjson.SetBytes(out, path, publicModel); err == nil {
				out = next
			}
		}
	}
	return out
}
