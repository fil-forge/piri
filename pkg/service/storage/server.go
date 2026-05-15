package storage

import (
	"fmt"

	"github.com/fil-forge/go-ucanto/server"
	ucanhttp "github.com/fil-forge/go-ucanto/transport/http"

	"github.com/fil-forge/piri/pkg/server/handler"
)

func NewHandler(server server.ServerView[server.Service]) handler.Func {
	return func(ctx handler.Context) error {
		r := ctx.Request()
		res, err := server.Request(r.Context(), ucanhttp.NewRequest(r.Body, r.Header))
		if err != nil {
			return fmt.Errorf("handling UCAN request: %w", err)
		}

		for key, vals := range res.Headers() {
			for _, v := range vals {
				ctx.Response().Header().Add(key, v)
			}
		}

		// content type is empty as it will have been set by ucanto transport codec
		return ctx.Stream(res.Status(), "", res.Body())
	}
}
