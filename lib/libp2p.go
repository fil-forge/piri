package lib

import (
	"crypto/ed25519"
	"fmt"

	"github.com/fil-forge/ucantone/multikey"
	"github.com/libp2p/go-libp2p/core/crypto"
)

// Libp2pKey returns the node identity's key in libp2p form, which IPNI signs
// advertisements with and derives the provider peer ID from.
func Libp2pKey(id multikey.Issuer) (crypto.PrivKey, error) {
	// ucantone's ed25519 Signer.Raw() returns the 32-byte seed; libp2p
	// expects the 64-byte Go private-key form (seed || pub). Expand here.
	priv, err := crypto.UnmarshalEd25519PrivateKey(ed25519.NewKeyFromSeed(id.Raw()))
	if err != nil {
		return nil, fmt.Errorf("unmarshaling private key: %w", err)
	}
	return priv, nil
}
