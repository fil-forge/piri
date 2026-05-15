package aatodo_types

import (
	"errors"
	"fmt"

	"github.com/fil-forge/libforge/piece/digest"
	"github.com/fil-forge/libforge/piece/size"
	"github.com/ipfs/go-cid"
)

// ErrWrongCodec is returned when a CID offered as a piece link does not use the
// raw codec required for Filecoin piece commitments.
var ErrWrongCodec = errors.New("piece cid must use the raw codec")

// PieceLink is a Filecoin v2 piece reference. The underlying CID encodes the
// piece commitment plus padding/height as a multihash; the methods below derive
// those parameters from it.
type PieceLink struct {
	Cid cid.Cid `cborgen:"cid" dagjsongen:"cid"`
}

// FromCid builds a PieceLink from a v2 piece CID, validating that it carries a
// well-formed piece digest.
func FromCid(c cid.Cid) (*PieceLink, error) {
	if c.Prefix().Codec != cid.Raw {
		return nil, ErrWrongCodec
	}
	if _, err := digest.NewPieceDigest(c.Hash()); err != nil {
		return nil, fmt.Errorf("invalid piece digest: %w", err)
	}
	return &PieceLink{Cid: c}, nil
}

// FromPieceDigest builds a PieceLink from a piece digest.
func FromPieceDigest(pd digest.PieceDigest) *PieceLink {
	return &PieceLink{Cid: cid.NewCidV1(cid.Raw, pd.Bytes())}
}

// Link returns the v2 piece CID.
func (p *PieceLink) Link() cid.Cid {
	return p.Cid
}

func (p *PieceLink) DataCommitment() []byte {
	dc, _ := digest.DataCommitment(p.Cid.Hash())
	return dc
}

func (p *PieceLink) Height() uint8 {
	h, _ := digest.Height(p.Cid.Hash())
	return h
}

func (p *PieceLink) PaddedSize() uint64 {
	return size.HeightToPaddedSize(p.Height())
}

func (p *PieceLink) Padding() (uint64, error) {
	return digest.Padding(p.Cid.Hash())
}
