package app

import (
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/fx/claimvalidation"
	"github.com/fil-forge/piri/pkg/fx/root"
	"github.com/fil-forge/piri/pkg/principalresolver"
	"github.com/fil-forge/piri/pkg/service/claims"
	"github.com/fil-forge/piri/pkg/service/egresstracker"
	"github.com/fil-forge/piri/pkg/service/publisher"
	"github.com/fil-forge/piri/pkg/service/replicator"
	retrievalucan "github.com/fil-forge/piri/pkg/service/retrieval/ucan"
	"github.com/fil-forge/piri/pkg/service/storage"
	storageucan "github.com/fil-forge/piri/pkg/service/storage/ucan"
)

var UCANModule = fx.Module("ucan",
	root.Module,              // Provides root http handler
	claims.Module,            // Provides claims service and handler
	claimvalidation.Module,   // Provides context for validating UCANs
	publisher.Module,         // Provides publisher service and handler
	egresstracker.Module,     // Provides egress tracker service
	replicator.Module,        // Provides replicator service
	storage.Module,           // Wires upload connection + consumer-side interface bindings
	principalresolver.Module, // Provides principal resolver for UCAN
	storageucan.Module,       // Provides storage UCAN handler
	retrievalucan.Module,     // Provides retrieval UCAN handler
)
