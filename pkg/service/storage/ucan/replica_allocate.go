package ucan

import (
	"bytes"
	"fmt"
	"net/url"
	"time"

	assertcaps "github.com/fil-forge/libforge/capabilities/assert"
	blobcaps "github.com/fil-forge/libforge/capabilities/blob"
	"github.com/fil-forge/libforge/capabilities/blob/replica"
	ucantone_errors "github.com/fil-forge/ucantone/errors"
	"github.com/fil-forge/ucantone/execution/bindexec"
	"github.com/fil-forge/ucantone/principal"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/container"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/fil-forge/ucantone/ucan/promise"
	"github.com/ipfs/go-cid"

	"github.com/fil-forge/piri/pkg/pdp"
	"github.com/fil-forge/piri/pkg/service/blobs"
	"github.com/fil-forge/piri/pkg/service/replicator"
	blobhandler "github.com/fil-forge/piri/pkg/service/storage/handlers/blob"
	replicahandler "github.com/fil-forge/piri/pkg/service/storage/handlers/replica"
)

// transferTimeout is the maximum time the queued /blob/replica/transfer task
// has to complete; replication peers use this invocation as proof for
// requesting a retrieval delegation, so the window must comfortably exceed
// realistic retry/transfer durations.
const transferTimeout = time.Hour

// MissingLocationCommitmentErrorName surfaces in failure receipts when the
// upload service did not include the location-commitment invocation in the
// request container.
const MissingLocationCommitmentErrorName = "MissingLocationCommitment"

type ReplicaAllocateService interface {
	ID() principal.Signer
	PDP() pdp.PDP
	Blobs() blobs.Blobs
	Replicator() replicator.Replicator
}

// NewReplicaAllocateHandler returns the /blob/replica/allocate handler.
//
// The caller (typically the upload service) submits this invocation when it
// wants the storage node to fetch a replica of a blob from somewhere else.
// args.Site is the CID of an /assert/location invocation describing where the
// blob currently lives; that invocation MUST be attached to the request
// container so the handler can read its URL list.
//
// The handler:
//  1. Decodes the location commitment from request metadata.
//  2. Runs the local /blob/allocate to obtain (or skip) an upload slot.
//  3. Builds a /blob/replica/transfer invocation as the side-effect that the
//     replication task will execute.
//  4. Enqueues a background replicahandler.Transfer task.
//  5. Returns AllocateOK whose Site promise resolves to the transfer
//     invocation's task link.
func NewReplicaAllocateHandler(storageService ReplicaAllocateService) Handler {
	return Handler{
		Capability: replica.Allocate,
		Handler: bindexec.NewHandler(func(
			req *bindexec.Request[*replica.AllocateArguments],
			res *bindexec.Response[*replica.AllocateOK],
		) error {
			args := req.Task().Arguments()
			space := req.Invocation().Subject()
			cause := req.Invocation().Task().Link()

			claim, err := findInvocationByLink(req.Metadata(), args.Site)
			if err != nil {
				return res.SetFailure(ucantone_errors.New(
					MissingLocationCommitmentErrorName,
					"location commitment invocation %s not found in request metadata",
					args.Site,
				))
			}
			if claim.Command() != assertcaps.LocationCommand {
				return res.SetFailure(ucantone_errors.New(
					MissingLocationCommitmentErrorName,
					"site invocation %s is %s, expected %s",
					args.Site, claim.Command(), assertcaps.LocationCommand,
				))
			}
			var lc assertcaps.LocationArguments
			if err := lc.UnmarshalCBOR(bytes.NewReader(claim.ArgumentsBytes())); err != nil {
				return fmt.Errorf("decoding location commitment arguments: %w", err)
			}
			if len(lc.Location) < 1 {
				return res.SetFailure(ucantone_errors.New(
					MissingLocationCommitmentErrorName,
					"location commitment has no URLs",
				))
			}
			replicaAddress := lc.Location[0].URL()

			// Local allocation — may return a nil address if we already have
			// the blob (in which case the transfer task will skip the HTTP PUT
			// and only issue the receipt).
			localBlob := blobcaps.Blob{Digest: args.Blob.Digest, Size: args.Blob.Size}
			resp, err := blobhandler.Allocate(req.Context(), storageService, &blobhandler.AllocateRequest{
				Space: space,
				Blob:  localBlob,
				Cause: cause,
			})
			if err != nil {
				return fmt.Errorf("allocating replica: %w", err)
			}

			// Build the /blob/replica/transfer invocation; this is the
			// side-effect that the upload service waits on (via the AllocateOK
			// site promise).
			trnsfInv, err := replica.Transfer.Invoke(
				storageService.ID(),
				storageService.ID().DID(),
				&replica.TransferArguments{
					Blob:  replica.Blob{Digest: args.Blob.Digest, Size: args.Blob.Size},
					Site:  args.Site,
					Cause: cause,
				},
				// TODO(forrest)[ucan1]: extent the ucan UnixTimestamp to support time add/sub operations
				invocation.WithExpiration(ucan.UnixTimestamp(time.Now().Add(transferTimeout).Unix())),
				//uint64(ucan.Now()),
			)
			if err != nil {
				return fmt.Errorf("creating transfer invocation: %w", err)
			}

			var sink *url.URL
			if resp.Address != nil {
				u := resp.Address.URL.URL()
				sink = u
			}

			// TODO(forrest)[ucan1.0]: unsure if we want to pass the request context to this async operation
			if err := storageService.Replicator().Replicate(req.Context(), &replicahandler.TransferRequest{
				Space: space,
				Blob:  localBlob,
				Source: replicahandler.TransferSource{
					ID:  claim.Issuer(),
					URL: *replicaAddress,
				},
				Sink:  sink,
				Cause: trnsfInv,
			}); err != nil {
				return fmt.Errorf("enqueuing replication task: %w", err)
			}

			// Attach the transfer invocation to the response so the upload
			// service can fetch it later via a receipt lookup.
			res.SetMetadata(container.New(container.WithInvocations(trnsfInv)))

			return res.SetSuccess(&replica.AllocateOK{
				Site: promise.AwaitOK{Task: trnsfInv.Task().Link()},
			})
		}),
	}
}

// findInvocationByLink scans a container's invocations for one whose link
// matches `link`. Returns an error if not found or if the container is nil.
func findInvocationByLink(meta ucan.Container, link cid.Cid) (ucan.Invocation, error) {
	if meta == nil {
		return nil, fmt.Errorf("no metadata container on request")
	}
	for _, mInv := range meta.Invocations() {
		if mInv.Link() == link {
			return mInv, nil
		}
	}
	return nil, fmt.Errorf("invocation %s not found", link)
}
