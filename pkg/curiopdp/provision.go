package curiopdp

import (
	"context"
	"fmt"

	logging "github.com/ipfs/go-log/v2"
	"go.uber.org/fx"

	"github.com/filecoin-project/curio/harmony/harmonydb"

	appconfig "github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/wallet"
)

var log = logging.Logger("curiopdp")

// pdpServiceLabel must match PDPService.name — pdp_data_set_creates.service and
// pdp_piece_uploads.service FK to pdp_services(service_label), so a row with this
// label must exist before CreateProofSet / piece upload.
const pdpServiceLabel = "storacha"

// provisionPDPState seeds the Curio harmonydb rows that Curio's PDP code expects
// to already exist but Piri's flow doesn't otherwise populate:
//
//   - eth_keys (role='pdp'): message.SenderETH (every PDP tx) and
//     contract.FSRegister (registration) sign by reading the private key from
//     this table, NOT from Piri's wallet.
//   - pdp_services (service_label='storacha'): FK target for
//     pdp_data_set_creates / pdp_piece_uploads / pdp_piecerefs.
//
// Runs as an OnStart hook so it executes after the owner key is in the keystore
// (imported before fxApp.Start during `piri init`, or already persisted in the
// serve path) and before any task/init step needs it. Idempotent. Mirrors
// Curio's own provisioning in web/api/webrpc/pdp.go.
func provisionPDPState(lc fx.Lifecycle, db *harmonydb.DB, wlt *wallet.LocalWallet, cfg appconfig.PDPServiceConfig) {
	owner := cfg.OwnerAddress
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			keys, err := wlt.List(ctx)
			if err != nil {
				return fmt.Errorf("listing wallet keys: %w", err)
			}
			var ownerKey *wallet.Key
			for _, k := range keys {
				if k.Address == owner {
					ownerKey = k
					break
				}
			}
			if ownerKey == nil {
				return fmt.Errorf("owner key %s not found in wallet; cannot provision PDP state", owner.Hex())
			}

			// eth_keys (role='pdp')
			var hasKey bool
			if err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM eth_keys WHERE role = 'pdp')`).Scan(&hasKey); err != nil {
				return fmt.Errorf("checking for existing pdp eth_keys: %w", err)
			}
			if !hasKey {
				if _, err := db.Exec(ctx,
					`INSERT INTO eth_keys (address, private_key, role) VALUES ($1, $2, 'pdp')`,
					owner.Hex(), ownerKey.PrivateKey,
				); err != nil {
					return fmt.Errorf("provisioning owner key into eth_keys: %w", err)
				}
				log.Infow("provisioned owner key into eth_keys", "address", owner.Hex(), "role", "pdp")
			}

			// pdp_services row for the 'storacha' service label. The pubkey here is
			// a placeholder (the dataset-create path verifies its EIP-712 signature
			// on-chain, not against this row); the upload-auth path will eventually
			// need the real storacha service pubkey.
			var hasSvc bool
			if err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pdp_services WHERE service_label = $1)`, pdpServiceLabel).Scan(&hasSvc); err != nil {
				return fmt.Errorf("checking for existing pdp_services row: %w", err)
			}
			if !hasSvc {
				if _, err := db.Exec(ctx,
					`INSERT INTO pdp_services (service_label, pubkey) VALUES ($1, $2)`,
					pdpServiceLabel, ownerKey.PublicKey,
				); err != nil {
					return fmt.Errorf("provisioning pdp_services row: %w", err)
				}
				log.Infow("provisioned pdp_services row", "service_label", pdpServiceLabel)
			}
			return nil
		},
	})
}
