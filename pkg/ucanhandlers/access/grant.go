package access

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/fil-forge/ucantone/binding"
	"github.com/fil-forge/ucantone/server"
	"github.com/fil-forge/ucantone/ucan/delegation/policy"
	"github.com/fil-forge/ucantone/validator"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
	"go.opentelemetry.io/otel/codes"
	fxlib "go.uber.org/fx"

	"github.com/fil-forge/libforge/commands/access"
	"github.com/fil-forge/libforge/commands/assert"
	"github.com/fil-forge/libforge/commands/blob"
	"github.com/fil-forge/libforge/commands/blob/replica"
	"github.com/fil-forge/libforge/identity"
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/errors"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/container"
	"github.com/fil-forge/ucantone/ucan/delegation"

	"github.com/fil-forge/piri/pkg/config/app"
)

// validity is the time a granted delegation is valid for.
const validity = time.Hour

// GrantDeps is the dependency set populated by fx for the access/grant
// UCAN method.
type GrantDeps struct {
	fxlib.In
	ID        identity.Identity
	Upload    app.UploadServiceConfig
	Resolver  did.Resolver
	Factories map[string]validator.VerifierFactory
}

func NewGrantHandler(deps GrantDeps) server.Route {
	return access.Grant.Route(func(req *binding.Request[*access.GrantArguments], rsp *binding.Response[*access.GrantOK]) (err error) {
		ctx, span := tracer.Start(req.Context(), "access.grant")
		defer func() {
			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			}
			span.End()
		}()

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

		// Build the validator options once per request. The proof
		// resolver looks up delegation envelopes shipped alongside
		// the cause in the request container; the verifier resolver
		// reuses the same map the rpc server uses so did:web
		// identities (e.g. the upload service) resolve correctly.
		validateOpts := []validator.Option{
			validator.WithProofResolver(proofResolverFromMetadata(req.Metadata())),
			validator.WithDIDResolver(deps.Resolver),
			validator.WithVerifierFactories(deps.Factories),
		}

		grantedDlgs := make([]ucan.Delegation, 0, len(args.Attenuations))
		grantedLinks := make([]cid.Cid, 0, len(args.Attenuations))
		for _, att := range args.Attenuations {
			dlg, err := grantUcan1Capability(ctx, deps, validateOpts, audience, att.Command, cause)
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
	ctx context.Context,
	deps GrantDeps,
	validateOpts []validator.Option,
	audience did.DID,
	cmd ucan.Command,
	cause ucan.Invocation,
) (ucan.Delegation, error) {
	switch cmd {
	case blob.Retrieve.Command:
		return grantUcan1BlobRetrieve(ctx, deps, validateOpts, audience, cause)
	default:
		return nil, errors.New(access.UnknownAbilityErrorName, "unknown ability: %s", cmd)
	}
}

// proofResolverFromMetadata returns a ProofResolverFunc that resolves
// delegation CIDs against the delegations shipped in the request
// container alongside the cause invocation. Callers attach proofs via
// execution.WithProofs(...) on the request.
func proofResolverFromMetadata(meta ucan.Container) validator.ProofResolverFunc {
	return func(_ context.Context, link cid.Cid) (ucan.Delegation, error) {
		for _, dlg := range meta.Delegations() {
			if dlg.Link() == link {
				return dlg, nil
			}
		}
		return nil, fmt.Errorf("proof %s not present in request container", link)
	}
}

// grantUcan1BlobRetrieve issues a /blob/retrieve delegation when the
// supplied cause invocation justifies it — i.e. it is a /blob/replica/
// allocate or /assert/index invocation issued by the upload service to
// the same audience. The cause's blob digest is extracted (for logging,
// and as a future policy constraint anchor).
func grantUcan1BlobRetrieve(
	ctx context.Context,
	deps GrantDeps,
	validateOpts []validator.Option,
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

	// Extract digest from the cause's args. Done before the validator
	// call so unknown commands surface as UnknownCause rather than
	// UnauthorizedCause — the cause shape failing structurally is more
	// informative than its proof chain failing.
	var digest multihash.Multihash
	switch cause.Command() {
	case replica.Allocate.Command:
		var ca replica.AllocateArguments
		if err := ca.UnmarshalCBOR(bytes.NewReader(cause.ArgumentsBytes())); err != nil {
			return nil, errors.New(
				access.InvalidCauseErrorName,
				"decoding /blob/replica/allocate args: %s", err,
			)
		}
		digest = ca.Blob.Digest
	case assert.Index.Command:
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

	// Re-validate the cause as a standalone UCAN invocation. This
	// catches missing/expired/wrong-subject proof chains that the
	// shape checks above can't see.
	if err := validator.ValidateInvocation(ctx, cause, validateOpts...); err != nil {
		return nil, errors.New(
			access.UnauthorizedCauseErrorName,
			"cause not authorized: %s", err,
		)
	}

	dlg, err := blob.Retrieve.Delegate(
		deps.ID,
		audience,
		deps.ID.DID(),
		delegation.WithExpiration(ucan.Now()+ucan.UnixTimestamp(int64(validity.Seconds()))),
		// Convert to plain []byte: datamodel.Any's type switch matches
		// []byte literally, so the named multihash.Multihash type would
		// fall through to the reflect path and fail with "unsupported
		// type: uint8".
		delegation.WithPolicyBuilder(policy.Equal(".blob.digest", []byte(digest))),
	)
	if err != nil {
		return nil, err
	}
	log.Infow(
		"delegated capability",
		"command", blob.Retrieve.Command,
		"digest", digest,
		"audience", audience,
		"cause", cause.Command(),
	)
	return dlg, nil
}
