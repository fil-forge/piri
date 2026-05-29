package app

import (
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/fx/root"
	"github.com/fil-forge/piri/pkg/service/claims"
	"github.com/fil-forge/piri/pkg/service/publisher"
	ucanhandlersfx "github.com/fil-forge/piri/pkg/ucanhandlers/ucanfx"
)

var UCANModule = fx.Module("ucan",
	root.Module,      // Provides root http handler
	claims.Module,    // Provides claims service and handler
	publisher.Module, // Provides publisher service and handler
	ucanhandlersfx.Module,
	//egresstracker.Module,     // Provides egress tracker service — re-enable: see #14
	//replicator.Module,        // Provides replicator service — re-enable: see #15
	//storage.Module, // Wires upload connection + consumer-side interface bindings
	//principalresolver.Module, // Provides principal resolver for UCAN
	//storageucan.Module,   // Provides storage UCAN handler
	//retrievalucan.Module, // Provides retrieval UCAN handler
)
