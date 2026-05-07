package storage

import (
	"github.com/fil-forge/go-ucanto/server"
	logging "github.com/ipfs/go-log/v2"

	"github.com/fil-forge/piri/pkg/service/storage/ucan"
)

var log = logging.Logger("storage")

func NewUCANServer(storageService Service, options ...server.Option) (server.ServerView[server.Service], error) {
	options = append(
		options,
		ucan.WithAccessGrantMethod(storageService),
		ucan.WithBlobAllocateMethod(storageService),
		ucan.WithBlobAcceptMethod(storageService),
		ucan.WithPDPInfoMethod(storageService),
		ucan.WithReplicaAllocateMethod(storageService),
	)

	return server.NewServer(storageService.ID(), options...)
}
