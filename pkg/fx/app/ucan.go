package app

import (
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/fx/blobs"
	"github.com/fil-forge/piri/pkg/fx/claims"
	"github.com/fil-forge/piri/pkg/fx/presigner"
	"github.com/fil-forge/piri/pkg/fx/principalresolver"
	"github.com/fil-forge/piri/pkg/fx/publisher"
	"github.com/fil-forge/piri/pkg/fx/replicator"
	"github.com/fil-forge/piri/pkg/fx/retrieval"
	retrievalucan "github.com/fil-forge/piri/pkg/fx/retrieval/ucan"
	"github.com/fil-forge/piri/pkg/fx/root"
	"github.com/fil-forge/piri/pkg/fx/storage"
	storageucan "github.com/fil-forge/piri/pkg/fx/storage/ucan"
	"github.com/fil-forge/piri/pkg/service/egresstracker"
)

var UCANModule = fx.Module("ucan",
	// TODO(forrest): this module is providing an S3 based pre-signed which we don't need
	// its currently required by a blob service, whos interface is too large, most of which we don't need.
	// the todo here is the delete the "blob service" and this presigner as its code path is never used with PDP.
	presigner.Module, // Provides presigner.RequestPresigner

	root.Module,              // Provides root http handler
	blobs.Module,             // Provides blob service and handler
	claims.Module,            // Provides claims service and handler
	publisher.Module,         // Provides publisher service and handler
	egresstracker.Module,     // Provides egress tracker service
	replicator.Module,        // Provides replicator service (works with or without PDP)
	storage.Module,           // Provides storage service wrapper
	retrieval.Module,         // Provides retrieval service wrapper
	principalresolver.Module, // Provides principal resolver for UCAN
	storageucan.Module,       // Provides storage UCAN handler
	retrievalucan.Module,     // Provides retrieval UCAN handler
)
