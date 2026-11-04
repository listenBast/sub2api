package middleware

import (
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// ClientErrorScrubWrap installs the client-error scrubbing response writer and
// then runs inner. It must wrap the ops error logger so that the ops capture
// writer sits above (and therefore records the raw, unscrubbed payload for
// site admins) while the real client only receives scrubbed bytes.
func ClientErrorScrubWrap(inner gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer = &clientErrorScrubWriter{ResponseWriter: c.Writer, c: c}
		inner(c)
	}
}

// ClientErrorScrub installs the scrubbing writer only.
func ClientErrorScrub() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer = &clientErrorScrubWriter{ResponseWriter: c.Writer, c: c}
		c.Next()
	}
}

type clientErrorScrubWriter struct {
	gin.ResponseWriter
	c *gin.Context
}

func (w *clientErrorScrubWriter) scrub(b []byte) []byte {
	if len(b) == 0 || !service.ClientErrorScrubActive(w.c) {
		return b
	}
	status := w.Status()
	if status >= http.StatusBadRequest {
		if enc := w.Header().Get("Content-Encoding"); enc != "" && !strings.EqualFold(enc, "identity") {
			return b
		}
		out := service.ScrubClientErrorBytes(w.c, b)
		if len(out) != len(b) || string(out) != string(b) {
			if !w.Written() {
				w.Header().Del("Content-Length")
			}
		}
		return out
	}
	// Successful (usually streaming) responses: only touch error-ish events.
	if service.LooksLikeClientErrorEvent(b) {
		return service.ScrubClientErrorBytes(w.c, b)
	}
	return b
}

func (w *clientErrorScrubWriter) Write(b []byte) (int, error) {
	out := w.scrub(b)
	if len(out) == len(b) && (len(b) == 0 || &out[0] == &b[0]) {
		return w.ResponseWriter.Write(b)
	}
	if _, err := w.ResponseWriter.Write(out); err != nil {
		return 0, err
	}
	return len(b), nil
}

func (w *clientErrorScrubWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}
