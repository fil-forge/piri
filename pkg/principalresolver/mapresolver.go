package principalresolver

import (
	"context"
	"errors"

	"github.com/fil-forge/ucantone/did"
	verrs "github.com/fil-forge/ucantone/validator/errors"
)

// MapResolver resolves a non-did:key DID (e.g. did:web) to one or more
// did:key principals via an in-memory map. Satisfies
// [validator.DIDResolverFunc] (use `r.Resolve` as the function value).
type MapResolver struct {
	mapping map[did.DID]did.DID
}

// Resolve looks up the input DID in the map. Returns a single-element slice
// on success; an error when no mapping is present.
func (r *MapResolver) Resolve(_ context.Context, input did.DID) ([]did.DID, error) {
	dk, ok := r.mapping[input]
	if !ok {
		return nil, verrs.NewDIDKeyResolutionError(input, errors.New("not found in mapping"))
	}
	return []did.DID{dk}, nil
}

func NewMapResolver(smap map[string]string) (*MapResolver, error) {
	dmap := map[did.DID]did.DID{}
	for k, v := range smap {
		dk, err := did.Parse(k)
		if err != nil {
			return nil, err
		}
		dv, err := did.Parse(v)
		if err != nil {
			return nil, err
		}
		dmap[dk] = dv
	}
	return &MapResolver{dmap}, nil
}
