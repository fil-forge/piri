package replica

// TODO(forrest)[ucan1]: not doing

/*
import (
	"bytes"
	"fmt"
	"net/url"
	"time"

	"github.com/fil-forge/libforge/commands/assert"
	"github.com/fil-forge/libforge/commands/blob"
	"github.com/fil-forge/libforge/commands/blob/replica"
	"github.com/fil-forge/ucantone/errors"
	"github.com/fil-forge/ucantone/binding"
	"github.com/fil-forge/ucantone/multikey"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/container"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/fil-forge/ucantone/ucan/promise"
	fxlib "go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/service/replicator"
	"github.com/fil-forge/piri/pkg/ucanhandlers"
	blob2 "github.com/fil-forge/piri/pkg/ucanhandlers/blob"
)

// transferTimeout caps how long we allow ourselves to transfer the blob
// and conclude the task.
const transferTimeout = time.Hour

// Error names emitted by this handler.
const (
	MissingSiteErrorName = "MissingSite"
	InvalidSiteErrorName = "InvalidSite"
)

// ReplicaAllocateDeps is the dependency set populated by fx for the
// blob/replica/allocate UCAN method. It composes AllocateDeps with the
// signer and the replicator queue.
type ReplicaAllocateDeps struct {
	fxlib.In
	blob2.AllocateDeps
	ID         ucan.Signer
	Replicator replicator.Replicator
}

func NewReplicaAllocateHandler(deps ReplicaAllocateDeps) server.Route {
	return server.NewRoute(
		replica.Allocate,
		func(req *binding.Request[*replica.AllocateArguments], rsp *binding.Response[*replica.AllocateOK]) error {
			if err := ucanhandlers.RequireSubject(req, deps.ID.DID()); err != nil {
				return rsp.SetFailure(err)
			}

			args := req.Task().Arguments()
			space := req.Task().Subject()

			// 1. Resolve the site (location commitment) from the request
			//    container. UCAN 1.0 args carry the site CID; the
			//    envelope rides alongside in container.Invocations().
			var site ucan.Invocation
			for _, inv := range req.Metadata().Invocations() {
				if inv.Link() == args.Site {
					site = inv
					break
				}
			}
			if site == nil {
				return rsp.SetFailure(errors.New(
					MissingSiteErrorName,
					"site invocation %s not in request container", args.Site,
				))
			}

			var loc assert.LocationArguments
			if err := loc.UnmarshalCBOR(bytes.NewReader(site.ArgumentsBytes())); err != nil {
				return rsp.SetFailure(errors.New(
					InvalidSiteErrorName,
					"decoding location args: %s", err,
				))
			}
			if len(loc.Location) == 0 {
				return rsp.SetFailure(errors.New(
					InvalidSiteErrorName,
					"location claim has no URLs",
				))
			}
			// TODO(forrest)[ucan1]: if loc.Location has multiple entries,
			// the legacy path took the first. Keep that for now; revisit
			// if we ever want a smarter picker.
			replicaAddress := loc.Location[0]

			resp, err := blobhandler.Allocate(req.Context(), deps.AllocateDeps, &blobhandler.AllocateRequest{
				Space: space,
				Blob:  blob.Blob{Digest: args.Blob.Digest, Size: args.Blob.Size},
				Cause: req.Invocation().Link(),
			})
			if err != nil {
				// Mirrors blob/allocate: a named error (e.g. the piece size
				// limit) is a decision about the request, so it belongs in
				// the receipt rather than surfacing as a transport error.
				var named errors.Named
				if errors.As(err, &named) {
					return rsp.SetFailure(err)
				}
				return fmt.Errorf("allocating replica: %w", err)
			}

			// 3. Build the transfer invocation that fulfills the promise
			//    we hand back to the caller.
			transferInv, err := replica.Transfer.Invoke(
				deps.ID,
				deps.ID.DID(),
				&replica.TransferArguments{
					Blob: blob.Blob{
						Digest: args.Blob.Digest,
						// Use the allocation-response size (may be 0 when
						// an allocation already exists, signaling no
						// transfer is required).
						Size: resp.Size,
					},
					Site:  args.Site,
					Cause: req.Invocation().Link(),
				},
				invocation.WithExpiration(ucan.Now()+ucan.UnixTimestamp(int64(transferTimeout.Seconds()))),
			)
			if err != nil {
				return fmt.Errorf("building transfer invocation: %w", err)
			}

			var sink *url.URL
			if resp.Address != nil {
				sink = resp.Address.URL.URL()
			}
			if err := deps.Replicator.Replicate(req.Context(), &replicahandler.TransferRequest{
				Space: space,
				Blob:  blob.Blob{Digest: args.Blob.Digest, Size: resp.Size},
				Source: replicahandler.TransferSource{
					ID:  site.Issuer(),
					URL: replicaAddress.URL(),
				},
				Sink:  sink,
				Cause: req.Invocation(),
			}); err != nil {
				return fmt.Errorf("enqueueing replication: %w", err)
			}

			// 5. Build the typed OK with a promise pointing at the
			//    transfer invocation we just enqueued.
			ok := &replica.AllocateOK{
				Site: promise.AwaitOK{
					Task: transferInv.Link(),
				},
			}
			if err := rsp.SetMetadata(container.New(container.WithInvocations(transferInv))); err != nil {
				return fmt.Errorf("attaching transfer invocation: %w", err)
			}
			return rsp.SetSuccess(ok)
		},
	)
}

*/
