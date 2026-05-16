package delegate

import (
	"fmt"
	"io"

	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/container"
	"github.com/fil-forge/ucantone/ucan/delegation"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multibase"
	"github.com/multiformats/go-multicodec"
	"github.com/multiformats/go-multihash"
)

// MakeDelegations issues one UCAN 1.0 delegation per command (UCAN 1.0
// delegations are single-command) and returns them as a slice. Callers
// typically bundle the result into a [container.Container].
func MakeDelegations(issuer ucan.Signer, audience did.DID, commands []ucan.Command, opts ...delegation.Option) ([]ucan.Delegation, error) {
	out := make([]ucan.Delegation, 0, len(commands))
	for _, cmd := range commands {
		dlg, err := delegation.Delegate(issuer, audience, issuer.DID(), cmd, opts...)
		if err != nil {
			return nil, fmt.Errorf("delegating %s: %w", cmd, err)
		}
		out = append(out, dlg)
	}
	return out, nil
}

// EncodeDelegationsContainer encodes a set of delegations as a single
// container envelope using the Raw (uncompressed CBOR) codec.
func EncodeDelegationsContainer(dlgs []ucan.Delegation) ([]byte, error) {
	ctr := container.New(container.WithDelegations(dlgs...))
	return container.Encode(container.Raw, ctr)
}

// FormatDelegationBytes takes a delegation container envelope and returns a
// multibase-base64-encoded CIDv1 carrying the envelope as identity-hashed
// content (the same shape consumed by piri's existing delegation readers,
// just with a DagCBOR codec instead of CAR).
func FormatDelegationBytes(envelope []byte) (string, error) {
	mh, err := multihash.Sum(envelope, multihash.IDENTITY, -1)
	if err != nil {
		return "", fmt.Errorf("failed to create identity hash: %w", err)
	}
	link := cid.NewCidV1(uint64(multicodec.DagCbor), mh)
	str, err := link.StringOfBase(multibase.Base64)
	if err != nil {
		return "", fmt.Errorf("failed to encode CID to base64: %w", err)
	}
	return str, nil
}

// FormatDelegation reads a delegation container envelope from r and returns
// the multibase-base64 CIDv1 form produced by FormatDelegationBytes.
func FormatDelegation(d io.Reader) (string, error) {
	db, err := io.ReadAll(d)
	if err != nil {
		return "", fmt.Errorf("failed to read delegation: %w", err)
	}
	return FormatDelegationBytes(db)
}
