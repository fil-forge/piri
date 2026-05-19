package replica

// TODO(forrest)[ucan1]: lot of toil to implement this, we don't know in what form will support replication
// punting
/*
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/fil-forge/go-libstoracha/capabilities/types"
	rclient "github.com/fil-forge/go-ucanto/client/retrieval"
	"github.com/fil-forge/go-ucanto/core/dag/blockstore"
	"github.com/fil-forge/go-ucanto/core/ipld"
	"github.com/fil-forge/go-ucanto/core/receipt/fx"
	"github.com/fil-forge/go-ucanto/core/receipt/ran"
	"github.com/fil-forge/go-ucanto/core/result"
	ucan_http "github.com/fil-forge/go-ucanto/transport/http"
	"github.com/fil-forge/go-ucanto/validator"
	"github.com/fil-forge/ucantone/ucan/promise"
	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	basicnode "github.com/ipld/go-ipld-prime/node/basic"
	"github.com/ipld/go-ipld-prime/printer"
	"go.opentelemetry.io/otel/attribute"
	fxlib "go.uber.org/fx"

	"github.com/fil-forge/libforge/capabilities/access"
	"github.com/fil-forge/libforge/capabilities/assert"
	"github.com/fil-forge/libforge/capabilities/blob"
	"github.com/fil-forge/libforge/capabilities/blob/replica"
	ucancap "github.com/fil-forge/libforge/capabilities/ucan"
	"github.com/fil-forge/ucantone/client"
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/principal"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/delegation"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/fil-forge/ucantone/ucan/receipt"

	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/pdp/aggregation/commp"
	"github.com/fil-forge/piri/pkg/service/publisher"
	blobhandler "github.com/fil-forge/piri/pkg/service/storage/handlers/blob"
	"github.com/fil-forge/piri/pkg/store/delegationstore"
	"github.com/fil-forge/piri/pkg/store/receiptstore"
)

var log = logging.Logger("storage/handlers/replica")

// TransferDeps is the dependency set populated by fx for the Transfer
// handler.
type TransferDeps struct {
	fxlib.In
	ID          principal.Signer
	Acceptances blobhandler.AcceptanceStore
	Pieces      blobhandler.PieceReader
	Commp       commp.Calculator
	ClaimStore  delegationstore.DelegationStore
	Publisher   publisher.Publisher
	Receipts    receiptstore.ReceiptStore
	Upload      app.UploadServiceConfig
}

func (d TransferDeps) acceptDeps() blobhandler.AcceptDeps {
	return blobhandler.AcceptDeps{
		ID:          d.ID,
		Acceptances: d.Acceptances,
		Pieces:      d.Pieces,
		Commp:       d.Commp,
		ClaimStore:  d.ClaimStore,
		Publisher:   d.Publisher,
	}
}

type TransferSource struct {
	// Identity of the node to transfer from.
	ID did.DID
	// URL the blob may be requested from.
	URL *url.URL
}

type transferSourceModel struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

type TransferRequest struct {
	// Space is the space to associate with blob.
	Space did.DID
	// Blob is the blob in question.
	Blob blob.Blob
	// Source is the location to replicate the blob from.
	Source TransferSource
	// Sink is the location to replicate the blob to.
	Sink *url.URL
	// Cause is the invocation responsible for spawning this replication
	// should be a replica/transfer invocation.
	Cause ucan.Invocation
}

type transferRequestModel struct {
	Space  string              `json:"space"`
	Blob   blob.Blob           `json:"blob"`
	Source transferSourceModel `json:"source"`
	Sink   *string             `json:"sink,omitempty"`
	Cause  []byte              `json:"cause"`
}

func (t *TransferRequest) MarshalJSON() ([]byte, error) {
	aux := transferRequestModel{
		Space: t.Space.String(),
		Blob:  t.Blob,
		Source: transferSourceModel{
			ID:  t.Source.ID.String(),
			URL: t.Source.URL.String(),
		},
	}

	if t.Sink != nil {
		sinkStr := t.Sink.String()
		aux.Sink = &sinkStr
	}

	aux.Cause = t.Cause.Bytes()

	return json.Marshal(aux)
}

func (t *TransferRequest) UnmarshalJSON(b []byte) error {
	aux := transferRequestModel{}
	if err := json.Unmarshal(b, &aux); err != nil {
		return fmt.Errorf("unmarshaling TransferRequest: %w", err)
	}

	spaceDID, err := did.Parse(aux.Space)
	if err != nil {
		return fmt.Errorf("parsing space DID: %w", err)
	}
	t.Space = spaceDID

	t.Blob = aux.Blob

	sourceID, err := did.Parse(aux.Source.ID)
	if err != nil {
		return fmt.Errorf("parsing source DID: %w", err)
	}
	t.Source.ID = sourceID
	sourceURL, err := url.Parse(aux.Source.URL)
	if err != nil {
		return fmt.Errorf("parsing source URL: %w", err)
	}
	t.Source.URL = sourceURL

	if aux.Sink != nil {
		sinkURL, err := url.Parse(*aux.Sink)
		if err != nil {
			return fmt.Errorf("parsing sink URL: %w", err)
		}
		t.Sink = sinkURL
	}

	inv, err := invocation.Decode(aux.Cause)
	if err != nil {
		return fmt.Errorf("unmarshaling cause: %w", err)
	}
	t.Cause = inv

	return nil
}

// Transfer handles blob replication with idempotent behavior to support reliable retries.
//
// This function is called by a job queue that retries failed operations up to 10 times.
// To prevent redundant data transfers when retries occur, the function is carefully
// structured to be idempotent:
//
// 1. Always check if the blob already exists BEFORE attempting any transfer
// 2. Only transfer data from source to sink if the blob doesn't exist locally
// 3. If the blob exists (from a previous attempt), skip transfer and just issue receipts
//
// The function handles two distinct scenarios:
// - New blob (request.Sink != nil && !exists): Transfer from source → sink → accept → receipt
// - Existing blob (exists || no sink): Create location assertion → receipt
//
// Both paths end with sending the receipt to the upload service, which confirms
// successful replication to the requesting node.
func Transfer(ctx context.Context, deps TransferDeps, request *TransferRequest, metrics *Metrics) (err error) {
	var (
		rcpt  *receipt.Receipt
		forks []fx.Effect
	)

	stopwatch := metrics.startDuration(sourceLabel(request.Source.URL), sinkLabel(request.Sink))
	defer func() {
		success := true
		if err != nil {
			success = false
		}
		stopwatch.Stop(ctx, attribute.Bool("success", success))
	}()

	// Check if the blob already exists
	blobExists, err := deps.Pieces.Has(ctx, request.Blob.Digest)
	if err != nil {
		return fmt.Errorf("checking if blob has been received before transfer: %w", err)
	}

	if request.Sink != nil && !blobExists {
		// Need to transfer the blob from source to sink
		acceptResp, err := transferBlobFromSource(ctx, deps, request)
		if err != nil {
			return fmt.Errorf("failed to accept replication source blob %s: %w", request.Blob.Digest, err)
		}

		pdpLink := acceptResp.PDP.Link()
		forks = []fx.Effect{fx.FromInvocation(acceptResp.Claim), fx.FromInvocation(acceptResp.PDP)}

		rcpt, err = issueTransferReceipt(ctx, deps, request, acceptResp.Claim.Link(), &pdpLink, forks)
		if err != nil {
			return err
		}
	} else {
		// Blob already exists (skip transfer for idempotency) or no sink specified - create location assertion
		claim, pdpAcceptInv, err := createLocationAssertion(ctx, deps, request)
		if err != nil {
			return err
		}

		pdpLink := pdpAcceptInv.Link()
		forks = []fx.Effect{fx.FromInvocation(claim), fx.FromInvocation(pdpAcceptInv)}

		rcpt, err = issueTransferReceipt(ctx, deps, request, claim.Link(), &pdpLink, forks)
		if err != nil {
			return err
		}
	}

	// Build and send message to upload service
	return sendMessageToUploadService(ctx, deps, rcpt)
}

func sinkLabel(sink *url.URL) string {
	if sink == nil {
		return "none"
	}
	return sink.Host
}

func sourceLabel(source *url.URL) string {
	if source == nil {
		return "none"
	}
	return source.Host
}

// transferBlobFromSource fetches blob from source and PUTs it to sink
func transferBlobFromSource(ctx context.Context, deps TransferDeps, request *TransferRequest) (*blobhandler.AcceptResponse, error) {
	allocInv, err := extractReplicaAllocateInvocation(request.Cause)
	if err != nil {
		return nil, fmt.Errorf("extracting %s invocation: %w", replica.AllocateAbility, err)
	}

	dlg, err := requestBlobRetrieveDelegation(ctx, request.Source.URL, deps.ID, request.Source.ID, allocInv)
	if err != nil {
		return nil, fmt.Errorf("requesting %s delegation: %w", blob.RetrieveAbility, err)
	}

	// perform authorized retrieval from source using the delegation
	inv, err := blob.Retrieve.Invoke(
		deps.ID,
		request.Source.ID,
		request.Source.ID.DID().String(),
		blob.RetrieveCaveats{Blob: blob.Blob{Digest: request.Blob.Digest}},
		delegation.WithProof(delegation.FromDelegation(dlg)),
	)
	if err != nil {
		return nil, fmt.Errorf("creating %s invocation: %w", blob.RetrieveAbility, err)
	}

	conn, err := rclient.NewConnection(request.Source.ID, &request.Source.URL)
	if err != nil {
		return nil, fmt.Errorf("creating connection to %s: %w", request.Source.ID.DID(), err)
	}

	replicaExecResp, replicaResp, err := rclient.Execute(ctx, inv, conn)
	if err != nil {
		return nil, fmt.Errorf("executing %s invocation: %w", blob.RetrieveAbility, err)
	}
	defer replicaResp.Body().Close()

	rcptLink, ok := replicaExecResp.Get(inv.Link())
	if !ok {
		return nil, fmt.Errorf("missing %s receipt: %s", blob.RetrieveAbility, inv.Link())
	}

	rcptReader, err := blob.NewRetrieveReceiptReader()
	if err != nil {
		return nil, err
	}

	rcpt, err := rcptReader.Read(rcptLink, replicaExecResp.Blocks())
	if err != nil {
		return nil, fmt.Errorf("reading %s receipt: %w", blob.RetrieveAbility, err)
	}

	_, x := result.Unwrap(rcpt.Out())
	if !errors.Is(x, blob.RetrieveError{}) {
		return nil, fmt.Errorf("replication source (%s) returned failure in receipt: %w", request.Source.URL.String(), x)
	}

	// Verify status from source
	if replicaResp.Status() >= 300 || replicaResp.Status() < 200 {
		return nil, fmt.Errorf("replication source (%s) returned unexpected status: %d", request.Source.URL.String(), replicaResp.Status())
	}

	// Stream source to sink
	req, err := http.NewRequest(http.MethodPut, request.Sink.String(), replicaResp.Body())
	if err != nil {
		return nil, fmt.Errorf("failed to create replication sink request: %w", err)
	}
	req.Header = replicaResp.Headers()
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf(
			"failed http PUT to replicate blob %s from %s to %s failed: %w",
			request.Blob.Digest,
			request.Source.URL.String(),
			request.Sink.String(),
			err,
		)
	}
	defer res.Body.Close()

	// Verify status
	if res.StatusCode >= 300 || res.StatusCode < 200 {
		topErr := fmt.Errorf(
			"unsuccessful http PUT to replicate blob %s from %s to %s status code %d",
			request.Blob.Digest,
			request.Source.URL.String(),
			request.Sink.String(),
			res.StatusCode,
		)
		resData, err := io.ReadAll(res.Body)
		if err != nil {
			return nil, fmt.Errorf("%s failed to read replication sink response body: %w", topErr, err)
		}
		return nil, fmt.Errorf("%s response body: %s", topErr, resData)
	}

	// Accept the blob using the AcceptDeps subset.
	return blobhandler.Accept(ctx, deps.acceptDeps(), &blobhandler.AcceptRequest{
		Space: request.Space,
		Blob:  request.Blob,
		Put: blob.Promise{
			UcanAwait: blob.Await{
				Selector: ".out.ok",
				Link:     request.Cause.Link(),
			},
		},
		Cause: request.Cause.Link(),
	})
}

// extractReplicaAllocateInvocation extracts the `blob/replica/allocate`
// invocation which is expected to be attached to the `blob/transfer` invocation
func extractReplicaAllocateInvocation(trnsfInv ucan.Invocation) (ucan.Invocation, error) {
	if len(trnsfInv.Command()) != 1 {
		return nil, fmt.Errorf("invalid %s invocation", replica.TransferAbility)
	}
	match, err := replica.Transfer.Match(validator.NewSource(trnsfInv.Capabilities()[0], trnsfInv))
	if err != nil {
		return nil, fmt.Errorf("matching %s invocation: %w", replica.TransferAbility, err)
	}
	blocks, err := blockstore.NewBlockReader(blockstore.WithBlocksIterator(trnsfInv.Blocks()))
	if err != nil {
		return nil, fmt.Errorf("reading %s invocation blocks: %w", replica.TransferAbility, err)
	}
	return invocation.NewInvocationView(match.Value().Nb().Cause, blocks)
}

// requestBlobRetrieveDelegation obtains a delegation for `blob/retrieve` from a
// node by invoking `access/grant`, using the `blob/replica/allocate` invocation
// as evidence that the delegation should be granted.
func requestBlobRetrieveDelegation(
	ctx context.Context,
	endpoint url.URL,
	issuer ucan.Signer,
	audience did.DID,
	cause invocation.Invocation, // the `blob/replica/allocate` invocation
) (ucan.Delegation, error) {
	inv, err := access.Grant.Invoke(
		issuer,
		audience,
		&access.GrantArguments{
			Attenuations: []access.CapabilityRequest{{Command: blob.Retrieve.Command()}},
			Cause:        cause.Link(),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("creating %s invocation: %w", access.GrantAbility, err)
	}
	for b, err := range cause.Export() {
		if err != nil {
			return nil, fmt.Errorf("exporting %s blocks: %w", replica.AllocateAbility, err)
		}
		if err = inv.Attach(b); err != nil {
			return nil, fmt.Errorf("attaching %s blocks: %w", replica.AllocateAbility, err)
		}
	}

	ch := ucan_http.NewChannel(&endpoint)
	conn, err := client.NewConnection(audience, ch)
	if err != nil {
		return nil, fmt.Errorf("creating connection to %s: %w", audience.DID(), err)
	}

	resp, err := client.Execute(ctx, []invocation.Invocation{inv}, conn)
	if err != nil {
		return nil, fmt.Errorf("executing %s invocation: %w", access.GrantAbility, err)
	}

	rcptLink, ok := resp.Get(inv.Link())
	if !ok {
		return nil, fmt.Errorf("missing %s receipt: %s", access.GrantAbility, inv.Link())
	}

	rcptReader, err := access.NewGrantReceiptReader()
	if err != nil {
		return nil, err
	}

	rcpt, err := rcptReader.Read(rcptLink, resp.Blocks())
	if err != nil {
		return nil, fmt.Errorf("reading %s receipt: %w", access.GrantAbility, err)
	}

	return result.MatchResultR2(
		rcpt.Out(),
		func(o access.GrantOk) (delegation.Delegation, error) {
			dlgBytes := o.Delegations.Values[o.Delegations.Keys[0]]
			return delegation.Extract(dlgBytes)
		},
		func(x access.GrantError) (delegation.Delegation, error) {
			return nil, x
		},
	)
}

// createLocationAssertion creates a location assertion for an existing blob.
func createLocationAssertion(ctx context.Context, deps TransferDeps, request *TransferRequest) (invocation.Invocation, invocation.Invocation, error) {
	has, err := deps.Pieces.Has(ctx, request.Blob.Digest)
	if err != nil {
		return nil, nil, fmt.Errorf("finding piece for blob: %w", err)
	}
	if !has {
		return nil, nil, fmt.Errorf("piece not found")
	}

	blobCID := cid.NewCidV1(cid.Raw, request.Blob.Digest)
	loc, err := deps.Pieces.ReadPieceURL(blobCID)
	if err != nil {
		return nil, nil, fmt.Errorf("creating retrieval URL for blob: %w", err)
	}
	pdpAcceptInv, err := pdp_cap.Accept.Invoke(
		deps.ID,
		deps.ID,
		deps.ID.DID().String(),
		pdp_cap.AcceptCaveats{
			Blob: blobCID.Hash(),
		}, delegation.WithNoExpiration())
	if err != nil {
		return nil, nil, fmt.Errorf("creating piece accept invocation: %w", err)
	}

	claim, err := assert.Location.Delegate(
		deps.ID,
		request.Space,
		deps.ID.DID().String(),
		assert.LocationCaveats{
			Space:    request.Space,
			Content:  types.FromHash(request.Blob.Digest),
			Location: []url.URL{loc},
		},
		delegation.WithNoExpiration(),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("creating location commitment: %w", err)
	}

	return claim, pdpAcceptInv, nil
}

// issueTransferReceipt creates and stores a transfer receipt
func issueTransferReceipt(ctx context.Context, deps TransferDeps, request *TransferRequest, siteLink ipld.Link, pdpLink *ipld.Link, forks []fx.Effect) (*receipt.Receipt, error) {
	transferReceipt := replica.TransferOk{
		Site: siteLink,
		PDP:  pdpLink,
	}

	ok := result.Ok[replica.TransferOk, ipld.Builder](transferReceipt)
	var opts []receipt.Option
	if len(forks) > 0 {
		opts = append(opts, receipt.WithFork(forks...))
	}

	rcpt, err := receipt.Issue(deps.ID, ok, ran.FromInvocation(request.Cause), opts...)
	if err != nil {
		return nil, fmt.Errorf("issuing receipt: %w", err)
	}

	if err := deps.Receipts.Put(ctx, rcpt); err != nil {
		return nil, fmt.Errorf("failed to put transfer receipt: %w", err)
	}

	return rcpt, nil
}

// linksFact is a [ucan.FactBuilder] for IPLD links.
type linksFact []ipld.Link

func (f linksFact) ToIPLD() (map[string]ipld.Node, error) {
	m := map[string]ipld.Node{}
	for _, l := range f {
		nb := basicnode.Prototype.Link.NewBuilder()
		if err := nb.AssignLink(l); err != nil {
			return nil, err
		}
		m[l.String()] = nb.Build()
	}
	return m, nil
}

// sendMessageToUploadService sends the message containing invocations and receipts to the upload service
func sendMessageToUploadService(ctx context.Context, deps TransferDeps, rcpt *receipt.Receipt) error {
	var rcptBlocks []ipld.Block
	var rcptBlockLinks linksFact
	for b, err := range rcpt.Blocks() {
		if err != nil {
			return fmt.Errorf("iterating receipt blocks: %w", err)
		}
		rcptBlocks = append(rcptBlocks, b)
		rcptBlockLinks = append(rcptBlockLinks, b.Link())
	}

	concludeInv, err := ucancap.Conclude.Invoke(
		deps.ID,
		deps.Upload.DID,
		&ucancap.ConcludeArguments{
			Receipt: rcpt.Link(),
		},
		// ensure all receipt blocks remain included with this invocation
		invocation.WithMetadata([]ucan.FactBuilder{rcptBlockLinks}),
	)
	if err != nil {
		return fmt.Errorf("generating conclude invocation: %w", err)
	}

	// attach the receipt blocks to the conclude invocation
	for _, b := range rcptBlocks {
		if err := concludeInv.Attach(b); err != nil {
			return fmt.Errorf("attaching receipt block: %w", err)
		}
	}

	resp, err := client.Execute(ctx, []invocation.Invocation{concludeInv}, deps.Upload.Connection)
	if err != nil {
		return fmt.Errorf("executing conclude invocation: %w", err)
	}

	concludeRcptLink, ok := resp.Get(concludeInv.Link())
	if !ok {
		return fmt.Errorf("missing receipt for invocation: %s", concludeInv.Link().String())
	}

	blocks, err := blockstore.NewBlockReader(blockstore.WithBlocksIterator(resp.Blocks()))
	if err != nil {
		return fmt.Errorf("constructing blockstore: %w", err)
	}

	concludeRcpt, err := receipt.NewAnyReceipt(concludeRcptLink, blocks)
	if err != nil {
		return fmt.Errorf("constructing receipt: %w", err)
	}

	// we're not expecting any meaningful response here so we just check for error
	_, x := result.Unwrap(concludeRcpt.Out())
	if x != nil {
		log.Errorf("conclude invocation failure: %s", printer.Sprint(x))
		return errors.New("conclude invocation failed")
	}

	return nil
}

// SendFailureReceipt sends a failure receipt to the upload service when Transfer fails after all retries
func SendFailureReceipt(ctx context.Context, deps TransferDeps, request *TransferRequest, transferErr error) error {
	// TODO(forrest)[ucan1]: unsure what to provide in the error aside from TransferOK?
	rcpt, err := receipt.IssueErr(deps.ID, request.Cause.Link(), &replica.TransferOK{
		Site: cid.Undef,
		PDP:  promise.AwaitOK{Task: cid.Undef},
	})
	if err != nil {
		return fmt.Errorf("issuing failure receipt: %w", err)
	}

	if err := deps.Receipts.Put(ctx, rcpt); err != nil {
		return fmt.Errorf("failed to store failure receipt: %w", err)
	}

	if err := sendMessageToUploadService(ctx, deps, rcpt); err != nil {
		return fmt.Errorf("sending failure receipt: %w", err)
	}

	return nil
}

*/
