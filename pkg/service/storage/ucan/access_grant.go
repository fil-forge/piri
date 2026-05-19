package ucan

import (
	"bytes"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
	fxlib "go.uber.org/fx"

	"github.com/fil-forge/libforge/capabilities/access"
	"github.com/fil-forge/libforge/capabilities/assert"
	"github.com/fil-forge/libforge/capabilities/blob"
	"github.com/fil-forge/libforge/capabilities/blob/replica"
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/errors"
	"github.com/fil-forge/ucantone/execution/bindexec"
	"github.com/fil-forge/ucantone/principal"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/container"
	"github.com/fil-forge/ucantone/ucan/delegation"

	"github.com/fil-forge/piri/pkg/config/app"
)

// validity is the time a granted delegation is valid for.
const validity = time.Hour

// AccessGrantDeps is the dependency set populated by fx for the access/grant
// UCAN method.
type AccessGrantDeps struct {
	fxlib.In
	ID     principal.Signer
	Upload app.UploadServiceConfig
}

func NewAccessGrantHandler(deps AccessGrantDeps) Handler {
	return TypedHandler(
		access.Grant,
		func(req *bindexec.Request[*access.GrantArguments], rsp *bindexec.Response[*access.GrantOK]) error {
			args := req.Task().Arguments()

			if len(args.Attenuations) == 0 {
				return rsp.SetFailure(access.ErrMissingCapability)
			}

			// Resolve the optional cause invocation from the request
			// container. UCAN 1.0 carries the cause CID in the args and
			// the envelope alongside in the container.
			var cause ucan.Invocation
			if args.Cause != nil {
				for _, inv := range req.Metadata().Invocations() {
					if inv.Link() == *args.Cause {
						cause = inv
						break
					}
				}
				if cause == nil {
					return rsp.SetFailure(access.ErrUnknownCause)
				}
			}

			audience := req.Invocation().Issuer()

			grantedDlgs := make([]ucan.Delegation, 0, len(args.Attenuations))
			grantedLinks := make([]cid.Cid, 0, len(args.Attenuations))
			for _, att := range args.Attenuations {
				dlg, err := grantUcan1Capability(deps, audience, att.Command, cause)
				if err != nil {
					return rsp.SetFailure(err)
				}
				grantedDlgs = append(grantedDlgs, dlg)
				grantedLinks = append(grantedLinks, dlg.Link())
			}

			// Attach signed delegation envelopes via response metadata so
			// the caller can recover them from the receipt container.
			if err := rsp.SetMetadata(container.New(container.WithDelegations(grantedDlgs...))); err != nil {
				return err
			}
			return rsp.SetSuccess(&access.GrantOK{Delegations: grantedLinks})
		},
	)
}

// grantUcan1Capability dispatches a single requested capability to its
// per-ability granter. Only /blob/retrieve is currently supported; other
// commands surface as a stable UnknownAbility receipt failure.
func grantUcan1Capability(
	deps AccessGrantDeps,
	audience did.DID,
	cmd ucan.Command,
	cause ucan.Invocation,
) (ucan.Delegation, error) {
	switch cmd {
	case blob.RetrieveCommand:
		return grantUcan1BlobRetrieve(deps, audience, cause)
	default:
		return nil, errors.New(access.UnknownAbilityErrorName, "unknown ability: %s", cmd)
	}
}

// grantUcan1BlobRetrieve issues a /blob/retrieve delegation when the
// supplied cause invocation justifies it — i.e. it is a /blob/replica/
// allocate or /assert/index invocation issued by the upload service to
// the same audience. The cause's blob digest is extracted (for logging,
// and as a future policy constraint anchor).
func grantUcan1BlobRetrieve(
	deps AccessGrantDeps,
	audience did.DID,
	cause ucan.Invocation,
) (ucan.Delegation, error) {
	if cause == nil {
		return nil, access.ErrMissingCause
	}
	if cause.Audience().String() != audience.String() {
		return nil, errors.New(
			access.InvalidCauseErrorName,
			"audience is %s not %s", cause.Audience(), audience,
		)
	}
	uploadDID := deps.Upload.DID
	if cause.Issuer() != uploadDID {
		return nil, errors.New(
			access.InvalidCauseErrorName,
			"issuer is %s not %s", cause.Issuer(), uploadDID,
		)
	}

	var digest multihash.Multihash
	switch cause.Command() {
	case replica.AllocateCommand:
		var ca replica.AllocateArguments
		if err := ca.UnmarshalCBOR(bytes.NewReader(cause.ArgumentsBytes())); err != nil {
			return nil, errors.New(
				access.InvalidCauseErrorName,
				"decoding /blob/replica/allocate args: %s", err,
			)
		}
		digest = ca.Blob.Digest
	case assert.IndexCommand:
		var ca assert.IndexArguments
		if err := ca.UnmarshalCBOR(bytes.NewReader(cause.ArgumentsBytes())); err != nil {
			return nil, errors.New(
				access.InvalidCauseErrorName,
				"decoding /assert/index args: %s", err,
			)
		}
		digest = ca.Index.Hash()
	default:
		return nil, access.ErrUnknownCause
	}

	// TODO(forrest)[ucan1]: constrain the delegation to `digest` via a
	// UCAN 1.0 policy (`delegation.WithPolicyBuilder`). For now the
	// delegation is unconstrained at the token level; the retrieve
	// handler still authorizes by allocation membership.
	dlg, err := blob.Retrieve.Delegate(
		deps.ID,
		audience,
		deps.ID.DID(),
		delegation.WithExpiration(ucan.Now()+ucan.UnixTimestamp(int64(validity.Seconds()))),
		delegation.WithPolicyBuilder(), // TODO(forrest)[ucan1]: ostensibly this is where policy is enforced, unsure if required at this point?
	)
	if err != nil {
		return nil, err
	}
	log.Infow(
		"delegated capability",
		"command", blob.RetrieveCommand,
		"digest", digest,
		"audience", audience,
		"cause", cause.Command(),
	)
	return dlg, nil
}
