package handler

import (
	"io"
	"net/http"

	"github.com/labstack/echo/v5"
)

// Context is a context compatible with echo server [*echo.Context] that has only
// interface methods that are needed.
type Context interface {
	// Request returns [*http.Request]
	Request() *http.Request
	// Response returns the [http.ResponseWriter] for the request.
	Response() http.ResponseWriter
	// Stream sends a streaming response with status code and content type.
	Stream(code int, contentType string, r io.Reader) error
}

var _ Context = (*echo.Context)(nil)

// Func is a HTTP handler function that takes an echo compatible [Context].
type Func func(c Context) error

func (h Func) ToEcho() echo.HandlerFunc {
	return func(c *echo.Context) error {
		return h(c)
	}
}
