package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/pdp/httpapi/server/middleware"
	"github.com/fil-forge/piri/pkg/pdp/piecesize"
)

// TestLimitUploadBody covers the cheap early exit on the upload route: a
// declared Content-Length over the limit must be refused before the handler
// runs, so no database work happens for a body that cannot be stored.
func TestLimitUploadBody(t *testing.T) {
	newRequest := func(t *testing.T, h *PDPHandler, contentLength int64, chunked bool) *httptest.ResponseRecorder {
		t.Helper()
		e := echo.New()
		e.HTTPErrorHandler = middleware.CustomHTTPErrorHandler

		req := httptest.NewRequest(http.MethodPut, "/pdp/piece/upload/x", strings.NewReader(""))
		if chunked {
			req.ContentLength = -1
		} else {
			req.ContentLength = contentLength
		}
		rec := httptest.NewRecorder()

		reached := false
		handler := h.limitUploadBody(func(c echo.Context) error {
			reached = true
			return c.NoContent(http.StatusNoContent)
		})

		c := e.NewContext(req, rec)
		if err := handler(c); err != nil {
			e.HTTPErrorHandler(err, c)
		}
		t.Cleanup(func() {
			if rec.Code == http.StatusRequestEntityTooLarge {
				assert.False(t, reached, "handler must not run for a rejected body")
			}
		})
		return rec
	}

	t.Run("at the limit passes through", func(t *testing.T) {
		h := &PDPHandler{}
		rec := newRequest(t, h, 266338304, false)
		assert.Equal(t, http.StatusNoContent, rec.Code)
	})

	t.Run("one byte over is refused with 413", func(t *testing.T) {
		h := &PDPHandler{}
		rec := newRequest(t, h, 266338305, false)
		assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
		assert.Contains(t, rec.Body.String(), "too large")
	})

	t.Run("chunked bodies pass through to the stream-level check", func(t *testing.T) {
		// No declared length means nothing to compare here; verifyread's
		// per-upload bound is the authoritative enforcement.
		h := &PDPHandler{}
		rec := newRequest(t, h, 0, true)
		assert.Equal(t, http.StatusNoContent, rec.Code)
	})

	t.Run("reads the limit per request", func(t *testing.T) {
		limits := piecesize.Limits{Padded: 1 << 28}
		h := &PDPHandler{pieceSize: piecesize.NewPolicy(func() piecesize.Limits { return limits })}

		rec := newRequest(t, h, 400_000_000, false)
		require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)

		limits = piecesize.Limits{Padded: 1 << 29}
		rec = newRequest(t, h, 400_000_000, false)
		assert.Equal(t, http.StatusNoContent, rec.Code,
			"raising the limit at runtime must take effect without re-registering routes")
	})
}
