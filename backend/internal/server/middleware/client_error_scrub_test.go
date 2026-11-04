//go:build unit

package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type rawCaptureWriter struct {
	gin.ResponseWriter
	buf strings.Builder
}

func (w *rawCaptureWriter) Write(b []byte) (int, error) {
	w.buf.Write(b)
	return w.ResponseWriter.Write(b)
}

func setupScrubRouter(raw *rawCaptureWriter, handler gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	// Stand-in for the ops error logger: captures what is written to it.
	opsLike := func(c *gin.Context) {
		raw.ResponseWriter = c.Writer
		c.Writer = raw
		c.Next()
	}
	r.Use(ClientErrorScrubWrap(opsLike))
	r.POST("/x", func(c *gin.Context) {
		service.RegisterClientScrubRequestedModel(c, "gpt-6.1-sol")
		service.RegisterClientScrubUpstreamModel(c, "gpt-6-luna")
		handler(c)
	})
	return r
}

func TestClientErrorScrub_ErrorBodyScrubbedForClientRawForOps(t *testing.T) {
	raw := &rawCaptureWriter{}
	body := `{"error":{"message":"The model gpt-6-luna does not exist"}}`
	r := setupScrubRouter(raw, func(c *gin.Context) {
		c.Header("Content-Length", "999")
		c.Data(http.StatusNotFound, "application/json", []byte(body))
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/x", nil))

	require.Equal(t, http.StatusNotFound, w.Code)
	require.Equal(t, `{"error":{"message":"The model gpt-6.1-sol does not exist"}}`, w.Body.String())
	require.NotContains(t, w.Body.String(), "gpt-6-luna")
	require.Contains(t, raw.buf.String(), "gpt-6-luna", "ops capture must stay raw")
}

func TestClientErrorScrub_SSEErrorEventScrubbedButDataEventsUntouched(t *testing.T) {
	raw := &rawCaptureWriter{}
	r := setupScrubRouter(raw, func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		c.Status(http.StatusOK)
		_, _ = c.Writer.WriteString("data: {\"type\":\"response.output_text.delta\",\"delta\":\"gpt-6-luna\"}\n\n")
		_, _ = c.Writer.WriteString("event: error\ndata: {\"type\":\"error\",\"error\":{\"message\":\"gpt-6-luna overloaded\"}}\n\n")
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/x", nil))

	out := w.Body.String()
	require.Contains(t, out, `"delta":"gpt-6-luna"`)
	require.Contains(t, out, `gpt-6.1-sol overloaded`)
	require.NotContains(t, out, "gpt-6-luna overloaded")
}

func TestClientErrorScrub_ReturnsOriginalLengthAndNoopWhenNothingRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(ClientErrorScrub())
	r.POST("/x", func(c *gin.Context) {
		n, err := c.Writer.Write([]byte(`{"error":"gpt-6-luna"}`))
		require.NoError(t, err)
		require.Equal(t, len(`{"error":"gpt-6-luna"}`), n)
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/x", nil))
	require.Equal(t, `{"error":"gpt-6-luna"}`, w.Body.String())
}

func TestClientErrorScrub_ReportedWriteLengthMatchesInput(t *testing.T) {
	raw := &rawCaptureWriter{}
	in := `{"error":{"message":"gpt-6-luna"}}`
	r := setupScrubRouter(raw, func(c *gin.Context) {
		c.Status(http.StatusBadRequest)
		n, err := c.Writer.Write([]byte(in))
		require.NoError(t, err)
		require.Equal(t, len(in), n)
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/x", nil))
	require.Contains(t, w.Body.String(), "gpt-6.1-sol")
}
