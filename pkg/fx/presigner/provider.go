package presigner

import (
	"fmt"

	"github.com/fil-forge/ucantone/principal"
	"github.com/multiformats/go-multihash"
	"go.uber.org/fx"

	"github.com/fil-forge/go-libstoracha/digestutil"

	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/presigner"
)

var Module = fx.Module("presigner",
	fx.Provide(
		NewRequestPresigner,
	),
)

// NewRequestPresigner creates a new S3 request presigner
func NewRequestPresigner(cfg app.AppConfig, id principal.Signer) (presigner.RequestPresigner, error) {
	if cfg.Server.PublicURL.Scheme == "" {
		return nil, fmt.Errorf("public URL required for presigner")
	}

	accessKeyID := id.DID().String()
	idDigest, _ := multihash.Sum(id.Raw(), multihash.SHA2_256, -1)
	secretAccessKey := digestutil.Format(idDigest)

	return presigner.NewS3RequestPresigner(accessKeyID, secretAccessKey, cfg.Server.PublicURL, "blob")
}
