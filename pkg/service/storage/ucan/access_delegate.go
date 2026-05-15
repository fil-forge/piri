package ucan

import (
	"fmt"

	accesscaps "github.com/fil-forge/libforge/capabilities/access"
	"github.com/fil-forge/ucantone/errors"
	"github.com/fil-forge/ucantone/execution/bindexec"
	"github.com/fil-forge/ucantone/principal"
	"github.com/fil-forge/ucantone/ucan/delegation"

	"github.com/fil-forge/piri/pkg/store/delegationstore"
)

// AccessDelegateService is the surface the /access/delegate handler depends on.
type AccessDelegateService interface {
	ID() principal.Signer
	Delegations() delegationstore.DelegationStore
}

// MissingDelegationErrorName is the typed failure name reported when an
// /access/delegate invocation references a delegation CID whose bytes are
// not attached to the inbound container.
const MissingDelegationErrorName = "MissingDelegation"

// NewAccessDelegateHandler returns the handler bound to libforge's
// `/access/delegate` capability. It accepts an inbound list of delegation
// CIDs (with the corresponding delegation bytes attached as proof blocks)
// and stores them in the local delegation store so they can later be
// claimed via `/access/claim`.
//
// Storage is keyed by delegation root CID; per-audience indexing is
// deferred to the claim path, which iterates and filters.
func NewAccessDelegateHandler(service AccessDelegateService) Handler {
	return Handler{
		Capability: accesscaps.Delegate,
		Handler: bindexec.NewHandler(func(
			req *bindexec.Request[*accesscaps.DelegateArguments],
			res *bindexec.Response[*accesscaps.DelegateOK],
		) error {
			// TODO(forrest)[ucan1.0] I believe the intention here is to delegate
			// specific capabilities to specific services based on their dids
			// so upload service, indexing service, and etracker will all need
			// bespoke delegations delegated to them based on their needs
			// FIXME(forrest): This code doesn't meet the TODO.
			args := req.Task().Arguments()
			ctr := req.Metadata()

			store := service.Delegations()
			for _, cid := range args.Delegations {
				dlg, ok := ctr.Delegation(cid)
				if !ok {
					return res.SetFailure(errors.New(
						MissingDelegationErrorName,
						"delegation %s referenced in /access/delegate arguments is not attached as a proof block",
						cid,
					))
				}
				// `ctr.Delegation` returns a ucan.Delegation interface; the
				// store wants the concrete *delegation.Delegation. Re-decode
				// from the envelope bytes.
				stored, err := delegation.Decode(dlg.Bytes())
				if err != nil {
					return res.SetFailure(errors.New(
						MissingDelegationErrorName,
						"decoding delegation %s: %s",
						cid, err,
					))
				}
				if err := store.Put(req.Context(), stored); err != nil {
					return fmt.Errorf("storing delegation %s: %w", cid, err)
				}
			}

			return res.SetSuccess(&accesscaps.DelegateOK{})
		}),
	}
}
