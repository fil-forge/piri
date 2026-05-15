// Package signer is piri's outbound wrapper around piri-signing-service.
//
// The wrapper forwards every Sign* call to a remote piri-signing-service
// via piri-signing-service/pkg/client. It optionally fetches an
// authorization delegation from the configured proof service and attaches
// it as a proof on each invocation.
package signer

import (
	"context"
	"fmt"
	"math/big"
	"reflect"

	"github.com/ethereum/go-ethereum/common"
	"github.com/fil-forge/filecoin-services/go/eip712"
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/execution"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/container"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/ipfs/go-cid"

	signertypes "github.com/fil-forge/piri-signing-service/pkg/types"

	"github.com/fil-forge/piri/pkg/service/proofs"
)

// remoteUpstream is the subset of piri-signing-service/pkg/client.Client
// the wrapper needs. Keeping it as an interface lets tests inject a fake.
type remoteUpstream interface {
	signertypes.SigningService
}

// Client implements signertypes.SigningService by forwarding to a remote
// piri-signing-service over UCAN. If a proofService is configured, every
// Sign* call first fetches a delegation authorizing the local issuer
// against the remote signing service for the relevant capability, and
// attaches it as a proof.
type Client struct {
	upstream     remoteUpstream
	proofService proofs.ProofService
	serviceDID   did.DID
}

// NewProofServiceSigner constructs a Client wired to a remote signing
// service identified by `serviceDID` and reachable via `conn`. When `conn`
// is nil the wrapper returns errUpstreamUnwired from every Sign* call to
// make the misconfiguration obvious.
//
// `proofService` is consulted on every call: if it returns a cached
// delegation for the requested command the delegation's CID is attached
// as a UCAN proof. A failure to fetch a proof is soft (the upstream
// rejects the invocation if a proof is required, surfacing a clear
// permission error).
func NewProofServiceSigner(
	serviceDID did.DID,
	conn execution.Executor,
	proofService proofs.ProofService,
) signertypes.
SigningService {
	c := &Client{
		proofService: proofService,
		serviceDID:   serviceDID,
	}
	if !isNilExecutor(conn) {
		c.upstream = newUpstreamClient(serviceDID, conn)
	}
	return c
}

// isNilExecutor reports whether conn is nil, including a nil pointer wrapped
// in a non-nil interface value (e.g. a (*client.HTTPClient)(nil) passed as
// execution.Executor) — a case a plain `conn != nil` check would miss,
// leaving the wrapper with a non-nil upstream that panics on first use.
func isNilExecutor(conn execution.Executor) bool {
	if conn == nil {
		return true
	}
	switch v := reflect.ValueOf(conn); v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func:
		return v.IsNil()
	default:
		return false
	}
}

// errUpstreamUnwired is returned by every Sign* method when the wrapper
// has no upstream client configured (Connection nil in the app config).
var errUpstreamUnwired = fmt.Errorf("signer: no upstream piri-signing-service client configured")

func (c *Client) ensureUpstream() error {
	if c.upstream == nil {
		return errUpstreamUnwired
	}
	return nil
}

// proofOptionsFor fetches an access delegation from the configured proof
// service (if any) for the given command and returns it as an
// invocation.Option list ready to merge with caller-provided options.
func (c *Client) proofOptionsFor(ctx context.Context, issuer ucan.Signer, cmd ucan.Command) []invocation.Option {
	if c.proofService == nil {
		return nil
	}
	dlg, err := c.proofService.RequestAccess(ctx, issuer, c.serviceDID, cmd, nil)
	if err != nil {
		return nil
	}
	return []invocation.Option{invocation.WithProofs(dlg.Link())}
}

// SignCreateDataSet forwards to the upstream client.
func (c *Client) SignCreateDataSet(
	ctx context.Context, issuer ucan.Signer, dataSet *big.Int, payee common.Address,
	metadata []eip712.MetadataEntry, options ...invocation.Option,
) (*eip712.AuthSignature, error) {
	if err := c.ensureUpstream(); err != nil {
		return nil, err
	}
	opts := append([]invocation.Option{}, options...)
	opts = append(opts, c.proofOptionsFor(ctx, issuer, "/pdp/sign/dataset/create")...)
	return c.upstream.SignCreateDataSet(ctx, issuer, dataSet, payee, metadata, opts...)
}

// SignAddPieces forwards to the upstream client.
func (c *Client) SignAddPieces(
	ctx context.Context, issuer ucan.Signer, dataSet, nonce *big.Int,
	pieceData [][]byte, metadata [][]eip712.MetadataEntry, proofs [][]cid.Cid,
	proofBundle []*container.Container, options ...invocation.Option,
) (*eip712.AuthSignature, error) {
	if err := c.ensureUpstream(); err != nil {
		return nil, err
	}
	opts := append([]invocation.Option{}, options...)
	opts = append(opts, c.proofOptionsFor(ctx, issuer, "/pdp/sign/pieces/add")...)
	return c.upstream.SignAddPieces(ctx, issuer, dataSet, nonce, pieceData, metadata, proofs, proofBundle, opts...)
}

// SignSchedulePieceRemovals forwards to the upstream client.
func (c *Client) SignSchedulePieceRemovals(
	ctx context.Context, issuer ucan.Signer, dataSet *big.Int, pieceIds []*big.Int,
	options ...invocation.Option,
) (*eip712.AuthSignature, error) {
	if err := c.ensureUpstream(); err != nil {
		return nil, err
	}
	opts := append([]invocation.Option{}, options...)
	opts = append(opts, c.proofOptionsFor(ctx, issuer, "/pdp/sign/pieces/remove/schedule")...)
	return c.upstream.SignSchedulePieceRemovals(ctx, issuer, dataSet, pieceIds, opts...)
}

// SignDeleteDataSet forwards to the upstream client.
func (c *Client) SignDeleteDataSet(
	ctx context.Context, issuer ucan.Signer, dataSet *big.Int,
	options ...invocation.Option,
) (*eip712.AuthSignature, error) {
	if err := c.ensureUpstream(); err != nil {
		return nil, err
	}
	opts := append([]invocation.Option{}, options...)
	opts = append(opts, c.proofOptionsFor(ctx, issuer, "/pdp/sign/dataset/delete")...)
	return c.upstream.SignDeleteDataSet(ctx, issuer, dataSet, opts...)
}
