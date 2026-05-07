package retrieval

import (
	"github.com/fil-forge/go-ucanto/server"
	"github.com/fil-forge/go-ucanto/server/retrieval"
	"github.com/fil-forge/piri/pkg/service/retrieval/ucan"
)

func NewUCANServer(retrievalService Service, options ...retrieval.Option) (server.ServerView[retrieval.Service], error) {
	options = append(
		options,
		ucan.WithBlobRetrieveMethod(retrievalService),
		ucan.WithSpaceContentRetrieveMethod(retrievalService),
	)

	return retrieval.NewServer(retrievalService.ID(), options...)
}
