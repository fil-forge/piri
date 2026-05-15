package storage

import (
	"github.com/fil-forge/ucantone/principal"

	"github.com/fil-forge/piri/pkg/pdp"
	"github.com/fil-forge/piri/pkg/service/blobs"
	"github.com/fil-forge/piri/pkg/service/claims"
	"github.com/fil-forge/piri/pkg/service/replicator"
	"github.com/fil-forge/piri/pkg/store/delegationstore"
	"github.com/fil-forge/piri/pkg/store/receiptstore"
)

type Service interface {
	// ID is the storage service identity, used to sign UCAN invocations and receipts.
	ID() principal.Signer
	// PDP handles PDP aggregation
	PDP() pdp.PDP
	// Blobs provides access to the blobs service.
	Blobs() blobs.Blobs
	// Claims provides access to the claims service.
	Claims() claims.Claims
	// Receipts provides access to receipts
	Receipts() receiptstore.ReceiptStore
	// Replicator provides access to the replication service
	Replicator() replicator.Replicator
	// Delegations provides access to the delegation store backing the
	// /access/delegate + /access/claim flows.
	Delegations() delegationstore.DelegationStore
	// UploadConnection provides the connection details to an upload service.
	// Returns `any` during the UCAN 1.0 migration: callers type-assert to
	// `*app.ServiceConnection` to obtain {DID, *ucantone/client.HTTPClient}.
	UploadConnection() any
}
