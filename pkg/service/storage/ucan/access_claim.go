package ucan

import (
	"fmt"

	accesscaps "github.com/fil-forge/libforge/capabilities/access"
	"github.com/fil-forge/ucantone/execution/bindexec"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/container"
	"github.com/ipfs/go-cid"
)

// NewAccessClaimHandler returns the handler bound to libforge's
// `/access/claim` capability. It looks up delegations in the local
// delegation store whose audience matches the invocation's issuer DID and
// returns their CIDs in the receipt, with the delegation bytes attached as
// proof blocks via the response metadata container.
//
// AccessDelegateService is reused for the dependency; both /access/delegate
// and /access/claim share the same delegation store.
func NewAccessClaimHandler(service AccessDelegateService) Handler {
	return Handler{
		Capability: accesscaps.Claim,
		Handler: bindexec.NewHandler(func(
			req *bindexec.Request[*accesscaps.ClaimArguments],
			res *bindexec.Response[*accesscaps.ClaimOK],
		) error {
			issuer := req.Invocation().Issuer()
			store := service.Delegations()

			var cids []cid.Cid
			var matched []ucan.Delegation
			for dlg, err := range store.ListByAudience(req.Context(), issuer) {
				if err != nil {
					return fmt.Errorf("listing delegations for %s: %w", issuer, err)
				}
				cids = append(cids, dlg.Link())
				matched = append(matched, dlg)
			}

			if len(matched) > 0 {
				ct := container.New(container.WithDelegations(matched...))
				if err := res.SetMetadata(ct); err != nil {
					return fmt.Errorf("attaching delegations to response: %w", err)
				}
			}

			return res.SetSuccess(&accesscaps.ClaimOK{Delegations: cids})
		}),
	}
}
