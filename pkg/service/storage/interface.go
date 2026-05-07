package storage

import (
	"github.com/fil-forge/go-ucanto/client"
	"github.com/fil-forge/go-ucanto/principal"
	"github.com/fil-forge/go-ucanto/validator"

	"github.com/fil-forge/piri/pkg/pdp"
	"github.com/fil-forge/piri/pkg/service/blobs"
	"github.com/fil-forge/piri/pkg/service/claims"
	"github.com/fil-forge/piri/pkg/service/replicator"
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
	// UploadConnection provides the connection details to an upload service
	UploadConnection() client.Connection
	// ClaimValidationContext provides the context required for validating UCANs.
	ClaimValidationContext() validator.ClaimContext
}
