package client

import (
	"context"
	"errors"
	"net/url"

	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
)

// ErrNotMigrated is returned by every method on Client while the UCAN 1.0
// migration is in progress. The previous go-ucanto RPC client
// (BlobAllocate, BlobAccept, PDPInfo) needs to be rebuilt on top of
// ucantone + libforge. Tracked as Phase 5f.
var ErrNotMigrated = errors.New("piri/pkg/client: not migrated to UCAN 1.0 yet")

// Sentinel errors preserved for callers that match against them.
var ErrNoReceipt = errors.New("no error for invocation")
var ErrIncorrectCapability = errors.New("did not receive expected capability")

// Config preserves the field shape used by callers; the StorageProof type
// is now a raw container envelope (was go-ucanto delegation.Proof).
type Config struct {
	ID             ucan.Signer
	StorageNodeID  did.DID
	StorageNodeURL url.URL
	StorageProof   []byte
}

// Client is the outbound RPC client to a remote piri node.
type Client struct{}

// NewClient stub.
func NewClient(_ Config) (*Client, error) {
	return &Client{}, nil
}

// BlobAddress is the upload target returned by BlobAllocate.
type BlobAddress struct {
	URL     *url.URL
	Headers map[string][]string
	Expires int64
}

// BlobAcceptResult is what BlobAccept returns.
type BlobAcceptResult struct {
	LocationCommitment *LocationCommitment
	PDPAccept          *PDPAcceptResult
}

type LocationCommitment struct {
	Location []url.URL
}

type PDPAcceptResult struct {
	Blob cid.Cid
}

// BlobAllocate stub.
func (c *Client) BlobAllocate(_ context.Context, _ did.DID, _ multihash.Multihash, _ uint64, _ cid.Cid) (*BlobAddress, error) {
	return nil, ErrNotMigrated
}

// BlobAccept stub.
func (c *Client) BlobAccept(_ context.Context, _ did.DID, _ multihash.Multihash, _ uint64, _ cid.Cid) (*BlobAcceptResult, error) {
	return nil, ErrNotMigrated
}

// PDPInfo stub.
func (c *Client) PDPInfo(_ context.Context, _ did.DID, _ cid.Cid) error {
	return ErrNotMigrated
}
