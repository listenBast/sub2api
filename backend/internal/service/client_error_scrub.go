package service

import (
	"net/url"
	"sort"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

// Client-facing error scrubbing.
//
// Upstream providers sometimes echo the (account-mapped) model name, our
// custom base URL or other routing details in their error bodies. Those error
// bodies are forwarded to API callers on many gateway paths. Instead of
// patching every path, the gateway collects the "things that must never reach
// a non-admin client" in a per-request state (registered at the points where
// an account / mapped model is selected) and a response-writer middleware
// (server/middleware.ClientErrorScrub) plus the WebSocket write funnels apply
// ScrubClientErrorText to error payloads. Site-admin ops error logs capture
// the raw payload below the scrubbing writer and stay untouched.

const ctxKeyClientErrorScrub = "client_error_scrub_state"

type clientErrorScrubState struct {
	mu         sync.Mutex
	requested  string
	upstream   []string
	redactions []string
}

func clientErrorScrubStateFor(c *gin.Context, create bool) *clientErrorScrubState {
	if c == nil {
		return nil
	}
	if v, ok := c.Get(ctxKeyClientErrorScrub); ok {
		if st, ok := v.(*clientErrorScrubState); ok {
			return st
		}
	}
	if !create {
		return nil
	}
	st := &clientErrorScrubState{}
	c.Set(ctxKeyClientErrorScrub, st)
	return st
}

func (s *clientErrorScrubState) addUpstream(model string) {
	model = strings.TrimSpace(model)
	if model == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.upstream {
		if m == model {
			return
		}
	}
	s.upstream = append(s.upstream, model)
}

func (s *clientErrorScrubState) addRedaction(v string) {
	v = strings.TrimSpace(v)
	if v == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.redactions {
		if m == v {
			return
		}
	}
	s.redactions = append(s.redactions, v)
}

// RegisterClientScrubRequestedModel records the model the client asked for. It
// is the replacement text for any upstream model name found in errors.
func RegisterClientScrubRequestedModel(c *gin.Context, requested string) {
	requested = strings.TrimSpace(requested)
	if c == nil || requested == "" {
		return
	}
	st := clientErrorScrubStateFor(c, true)
	st.mu.Lock()
	st.requested = requested
	st.mu.Unlock()
}

// RegisterClientScrubUpstreamModel records a model name that was (or will be)
// sent upstream. Names are accumulated across failover attempts.
func RegisterClientScrubUpstreamModel(c *gin.Context, upstream string) {
	if c == nil || strings.TrimSpace(upstream) == "" {
		return
	}
	clientErrorScrubStateFor(c, true).addUpstream(upstream)
}

// RegisterClientScrubAccount records routing details of the selected account
// that must not be echoed to clients: the account-mapped model for the
// requested model, the custom upstream base URL (and its host) and the
// account name.
func RegisterClientScrubAccount(c *gin.Context, account *Account, requestedModel string) {
	if c == nil || account == nil {
		return
	}
	st := clientErrorScrubStateFor(c, true)
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel != "" {
		st.mu.Lock()
		if st.requested == "" {
			st.requested = requestedModel
		}
		st.mu.Unlock()
		if mapped, matched := account.ResolveMappedModel(requestedModel); matched {
			st.addUpstream(mapped)
		}
	}
	if baseURL := strings.TrimSpace(account.GetCredential("base_url")); baseURL != "" {
		st.addRedaction(strings.TrimRight(baseURL, "/"))
		if u, err := url.Parse(baseURL); err == nil && u.Host != "" {
			st.addRedaction(u.Host)
		}
	}
	if name := strings.TrimSpace(account.Name); len(name) >= 6 {
		st.addRedaction(name)
	}
}

// ClientErrorScrubActive reports whether anything has been registered.
func ClientErrorScrubActive(c *gin.Context) bool {
	st := clientErrorScrubStateFor(c, false)
	if st == nil {
		return false
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	return len(st.upstream) > 0 || len(st.redactions) > 0
}

// ScrubClientErrorText removes upstream model names (replaced by the requested
// model) and routing details (replaced by "upstream") from text that is about
// to be shown to a non-site-admin audience.
func ScrubClientErrorText(c *gin.Context, text string) string {
	st := clientErrorScrubStateFor(c, false)
	if st == nil || text == "" {
		return text
	}
	st.mu.Lock()
	requested := st.requested
	upstream := append([]string(nil), st.upstream...)
	redactions := append([]string(nil), st.redactions...)
	st.mu.Unlock()
	if len(upstream) == 0 && len(redactions) == 0 {
		return text
	}
	// Longest first so that a name containing another registered name wins.
	sort.SliceStable(redactions, func(i, j int) bool { return len(redactions[i]) > len(redactions[j]) })
	for _, r := range redactions {
		if strings.Contains(text, r) {
			text = strings.ReplaceAll(text, r, "upstream")
		}
	}
	sort.SliceStable(upstream, func(i, j int) bool { return len(upstream[i]) > len(upstream[j]) })
	replacement := requested
	if replacement == "" {
		replacement = "the requested model"
	}
	for _, m := range upstream {
		if m == requested {
			continue
		}
		text = scrubModelNameWithReplacement(text, m, replacement)
	}
	return text
}

// ScrubClientErrorBytes is the []byte form of ScrubClientErrorText. The input
// slice is returned unchanged when nothing needs replacing.
func ScrubClientErrorBytes(c *gin.Context, body []byte) []byte {
	if len(body) == 0 || !ClientErrorScrubActive(c) {
		return body
	}
	out := ScrubClientErrorText(c, string(body))
	if out == string(body) {
		return body
	}
	return []byte(out)
}

// LooksLikeClientErrorEvent reports whether an SSE chunk is an error-ish event
// (Anthropic "event: error", OpenAI {"error":...} / response.failed).
func LooksLikeClientErrorEvent(chunk []byte) bool {
	s := string(chunk)
	return strings.Contains(s, "event: error") ||
		strings.Contains(s, `"error"`) ||
		strings.Contains(s, "response.failed") ||
		strings.Contains(s, "response.incomplete") ||
		strings.Contains(s, `"type":"error"`) ||
		strings.Contains(s, `"type": "error"`)
}

// scrubOpenAIWSClientErrorPayload scrubs error-like WebSocket frames headed to
// the API client. WS frames bypass the gin response writer (the connection is
// hijacked), so the WS write funnels call this explicitly.
func scrubOpenAIWSClientErrorPayload(c *gin.Context, payload []byte) []byte {
	if len(payload) == 0 || !ClientErrorScrubActive(c) || !LooksLikeClientErrorEvent(payload) {
		return payload
	}
	return ScrubClientErrorBytes(c, payload)
}
