package rpc_test

// Each test method exercises one scenario against the access/grant handler.
// Shared scaffolding (build grant invocation, ship cause in container,
// decode the receipt) lives in helpers_test.go.

import (
	"github.com/fil-forge/libforge/commands/access"
	"github.com/fil-forge/libforge/commands/assert"
	"github.com/fil-forge/libforge/commands/blob"
	"github.com/fil-forge/libforge/commands/blob/replica"
	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/ucantone/execution"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/command"
	"github.com/fil-forge/ucantone/ucan/delegation"
	"github.com/fil-forge/ucantone/ucan/invocation"
)

func (s *RPCSuite) TestAccessGrant_MissingCause() {
	t := s.T()
	rcpt := s.sendGrant(t, testutil.Bob, blob.Retrieve.Command, nil)
	assertReceiptFailure(t, rcpt, access.MissingCauseErrorName)
}

func (s *RPCSuite) TestAccessGrant_UnknownAbility() {
	t := s.T()
	grantee := testutil.Bob

	cause := testutil.Must(replica.Allocate.Invoke(
		s.UploadServiceIdentity,
		grantee.DID(),
		&replica.AllocateArguments{
			Blob:  blob.Blob{Digest: testutil.RandomMultihash(t), Size: 1234},
			Site:  testutil.RandomCID(t),
			Cause: testutil.RandomCID(t),
		},
		invocation.WithAudience(grantee.DID()),
	))(t)

	rcpt := s.sendGrant(t, grantee, command.New("/unknown/ability"), cause)
	assertReceiptFailure(t, rcpt, access.UnknownAbilityErrorName)
}

func (s *RPCSuite) TestAccessGrant_UnknownCauseEnvelope() {
	t := s.T()
	grantee := testutil.Bob
	service := s.ServiceID.DID()

	// Mint a cause invocation locally to get a real CID, then DON'T ship
	// the envelope in the request container. The handler can't resolve
	// the cause and fails with UnknownCause. Bypasses sendGrant because
	// sendGrant always packages whatever cause it's handed.
	orphan := testutil.Must(replica.Allocate.Invoke(
		s.UploadServiceIdentity,
		grantee.DID(),
		&replica.AllocateArguments{
			Blob:  blob.Blob{Digest: testutil.RandomMultihash(t), Size: 1234},
			Site:  testutil.RandomCID(t),
			Cause: testutil.RandomCID(t),
		},
		invocation.WithAudience(grantee.DID()),
	))(t)

	proof := testutil.Must(access.Grant.Delegate(s.ServiceID, grantee.DID(), service))(t)

	orphanCID := orphan.Link()
	inv := testutil.Must(access.Grant.Invoke(
		grantee,
		service,
		&access.GrantArguments{
			Attenuations: []access.CapabilityRequest{{Command: blob.Retrieve.Command}},
			Cause:        &orphanCID,
		},
		invocation.WithAudience(service),
		invocation.WithProofs(proof.Link()),
	))(t)

	resp := testutil.Must(s.RPCClient(t).Execute(execution.NewRequest(
		t.Context(), inv, execution.WithDelegations(proof),
	)))(t)
	assertReceiptFailure(t, resp.Receipt(), access.UnknownCauseErrorName)
}

func (s *RPCSuite) TestAccessGrant_UnknownCauseCommand() {
	t := s.T()
	grantee := testutil.Bob

	// assert.Equals is a valid UCAN invocation but not one the grant
	// handler knows how to extract a digest from.
	cause := testutil.Must(assert.Equals.Invoke(
		s.UploadServiceIdentity,
		grantee.DID(),
		&assert.EqualsArguments{
			Content: testutil.RandomMultihash(t),
			Equals:  testutil.RandomCID(t),
		},
		invocation.WithAudience(grantee.DID()),
	))(t)

	rcpt := s.sendGrant(t, grantee, blob.Retrieve.Command, cause)
	assertReceiptFailure(t, rcpt, access.UnknownCauseErrorName)
}

func (s *RPCSuite) TestAccessGrant_CauseAudienceMismatch() {
	t := s.T()
	grantee := testutil.Bob

	// Cause audience is set to a different identity than the grantee.
	// Handler rejects: cause must target the grantee.
	cause := testutil.Must(replica.Allocate.Invoke(
		s.UploadServiceIdentity,
		grantee.DID(),
		&replica.AllocateArguments{
			Blob:  blob.Blob{Digest: testutil.RandomMultihash(t), Size: 1234},
			Site:  testutil.RandomCID(t),
			Cause: testutil.RandomCID(t),
		},
		invocation.WithAudience(testutil.Mallory.DID()),
	))(t)

	rcpt := s.sendGrant(t, grantee, blob.Retrieve.Command, cause)
	assertReceiptFailure(t, rcpt, access.InvalidCauseErrorName)
}

func (s *RPCSuite) TestAccessGrant_CauseIssuerMismatch() {
	t := s.T()
	grantee := testutil.Bob

	// Cause issued by the grantee (not the upload service). Handler
	// rejects: only upload-service-issued causes can justify a grant.
	cause := testutil.Must(replica.Allocate.Invoke(
		grantee,
		grantee.DID(),
		&replica.AllocateArguments{
			Blob:  blob.Blob{Digest: testutil.RandomMultihash(t), Size: 1234},
			Site:  testutil.RandomCID(t),
			Cause: testutil.RandomCID(t),
		},
		invocation.WithAudience(grantee.DID()),
	))(t)

	rcpt := s.sendGrant(t, grantee, blob.Retrieve.Command, cause)
	assertReceiptFailure(t, rcpt, access.InvalidCauseErrorName)
}

func (s *RPCSuite) TestAccessGrant_UnauthorizedCause() {
	t := s.T()
	grantee := testutil.Bob

	// Cause has the right shape (issuer = upsvc, audience = grantee,
	// command in the handler's allow-list) but no proof chain authorizes
	// upsvc to invoke replica.Allocate on grantee's space. The handler's
	// validator step rejects with UnauthorizedCause.
	cause := testutil.Must(replica.Allocate.Invoke(
		s.UploadServiceIdentity,
		grantee.DID(),
		&replica.AllocateArguments{
			Blob:  blob.Blob{Digest: testutil.RandomMultihash(t), Size: 1234},
			Site:  testutil.RandomCID(t),
			Cause: testutil.RandomCID(t),
		},
		invocation.WithAudience(grantee.DID()),
		// intentionally no invocation.WithProofs(...)
	))(t)

	rcpt := s.sendGrant(t, grantee, blob.Retrieve.Command, cause)
	assertReceiptFailure(t, rcpt, access.UnauthorizedCauseErrorName)
}

func (s *RPCSuite) TestAccessGrant_BlobRetrieve_WithReplicaAllocateCause() {
	t := s.T()
	grantee := testutil.Bob

	// grantee delegates /blob/replica/allocate authority on its own
	// space to the upload service. The cause invocation references this
	// delegation as proof; the handler's validator walks the chain and
	// accepts.
	causeProof := testutil.Must(delegation.Delegate(
		grantee,
		s.UploadServiceIdentity.DID(),
		grantee.DID(),
		ucan.Command(replica.Allocate.Command),
	))(t)

	cause := testutil.Must(replica.Allocate.Invoke(
		s.UploadServiceIdentity,
		grantee.DID(),
		&replica.AllocateArguments{
			Blob:  blob.Blob{Digest: testutil.RandomMultihash(t), Size: 1234},
			Site:  testutil.RandomCID(t),
			Cause: testutil.RandomCID(t),
		},
		invocation.WithAudience(grantee.DID()),
		invocation.WithProofs(causeProof.Link()),
	))(t)

	rcpt := s.sendGrantWithProofs(t, grantee, blob.Retrieve.Command, cause, causeProof)
	assertGrantOK(t, rcpt, 1)
}

func (s *RPCSuite) TestAccessGrant_BlobRetrieve_WithAssertIndexCause() {
	t := s.T()
	grantee := testutil.Bob

	causeProof := testutil.Must(delegation.Delegate(
		grantee,
		s.UploadServiceIdentity.DID(),
		grantee.DID(),
		ucan.Command(assert.Index.Command),
	))(t)

	cause := testutil.Must(assert.Index.Invoke(
		s.UploadServiceIdentity,
		grantee.DID(),
		&assert.IndexArguments{
			Index: testutil.RandomCID(t),
		},
		invocation.WithAudience(grantee.DID()),
		invocation.WithProofs(causeProof.Link()),
	))(t)

	rcpt := s.sendGrantWithProofs(t, grantee, blob.Retrieve.Command, cause, causeProof)
	assertGrantOK(t, rcpt, 1)
}
