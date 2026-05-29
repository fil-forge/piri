package rpc_test

import (
	"bytes"
	"testing"

	"github.com/fil-forge/libforge/commands/access"
	"github.com/fil-forge/libforge/commands/blob"
	"github.com/fil-forge/ucantone/errors/datamodel"
	"github.com/fil-forge/ucantone/execution"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"
)

// sendInvocation executes inv through the suite's RPC client and returns
// the receipt. The invocation must already carry whatever proof CIDs the
// validator needs; use sendInvocationWithProofs to ship proof envelopes
// in the request container too.
func (s *RPCSuite) sendInvocation(t *testing.T, inv ucan.Invocation) ucan.Receipt {
	t.Helper()
	return s.sendInvocationWithProofs(t, inv)
}

// sendInvocationWithProofs is sendInvocation plus a slice of proof
// delegations attached to the request container so the validator can
// resolve any CIDs referenced by inv.Proofs().
func (s *RPCSuite) sendInvocationWithProofs(
	t *testing.T,
	inv ucan.Invocation,
	proofs ...ucan.Delegation,
) ucan.Receipt {
	t.Helper()
	reqOpts := []execution.RequestOption{}
	if len(proofs) > 0 {
		reqOpts = append(reqOpts, execution.WithDelegations(proofs...))
	}
	resp, err := s.RPCClient(t).Execute(execution.NewRequest(t.Context(), inv, reqOpts...))
	require.NoError(t, err)
	return resp.Receipt()
}

// assertReceiptOK asserts the receipt's Out branch is Ok. For handlers
// whose OK payload is empty (or where the test only cares that the
// handler accepted the invocation), this is enough; tests that need to
// decode a typed OK should call rcpt.Out().Unpack() directly after this
// passes.
//
// On unexpected failure, the helper decodes and surfaces the error name
// so a regression is immediately obvious in the test output.
func assertReceiptOK(t *testing.T, rcpt ucan.Receipt) {
	t.Helper()
	if rcpt.Out().IsErr() {
		_, errBytes := rcpt.Out().Unpack()
		var em datamodel.ErrorModel
		_ = em.UnmarshalCBOR(bytes.NewReader(errBytes))
		t.Fatalf("expected receipt success, got error %q: %s", em.ErrorName, em.Message)
	}
}

// assertReceiptFailure asserts the receipt carries a UCAN error whose
// name matches expectedName. Decoding the error model lets the test
// distinguish between e.g. InvalidCause and UnknownCause — assertions
// against bare IsErr() would conflate every error path.
func assertReceiptFailure(t *testing.T, rcpt ucan.Receipt, expectedName string) {
	t.Helper()
	require.True(t, rcpt.Out().IsErr(), "expected receipt failure, got success")

	_, errBytes := rcpt.Out().Unpack()
	var em datamodel.ErrorModel
	require.NoError(t, em.UnmarshalCBOR(bytes.NewReader(errBytes)), "decoding error model")
	require.Equal(t, expectedName, em.ErrorName, "wrong error name (message: %q)", em.Message)
}

// sendGrant builds an access.Grant invocation and returns its receipt.
// Convenience wrapper around sendGrantWithProofs for tests that don't
// need to attach a proof chain for the cause invocation.
func (s *RPCSuite) sendGrant(
	t *testing.T,
	grantee ucan.Signer,
	ability ucan.Command,
	cause ucan.Invocation,
) ucan.Receipt {
	t.Helper()
	return s.sendGrantWithProofs(t, grantee, ability, cause)
}

// sendGrantWithProofs builds an access.Grant invocation and returns its
// receipt, shipping additional proof delegations in the request
// container alongside the access/grant proof and the cause envelope.
//
// access.Grant is a public-request endpoint: the grantee signs the
// invocation but isn't the service itself, so the UCAN validator demands
// a proof chain. The helper always mints an inline delegation from the
// service to the grantee authorizing /access/grant; a real client would
// hold one out-of-band.
//
// extraProofs are delegations needed to authorize the cause invocation
// (e.g. a delegation from the cause's subject to the cause's issuer
// granting the cause's command). The handler re-validates the cause
// after structural checks pass; without sufficient proofs, the cause
// receipt comes back as UnauthorizedCause.
//
// If cause is non-nil, its envelope ships in the request container so
// the handler can resolve the args.Cause CID. Pass nil for cause to
// exercise the no-cause failure path.
func (s *RPCSuite) sendGrantWithProofs(
	t *testing.T,
	grantee ucan.Signer,
	ability ucan.Command,
	cause ucan.Invocation,
	extraProofs ...ucan.Delegation,
) ucan.Receipt {
	t.Helper()

	service := s.ServiceID.DID()

	grantProof, err := access.Grant.Delegate(s.ServiceID, grantee.DID(), service)
	require.NoError(t, err)

	var causePtr *cid.Cid
	if cause != nil {
		c := cause.Link()
		causePtr = &c
	}

	inv, err := access.Grant.Invoke(
		grantee,
		service,
		&access.GrantArguments{
			Attenuations: []access.CapabilityRequest{{Command: ability}},
			Cause:        causePtr,
		},
		invocation.WithAudience(service),
		invocation.WithProofs(grantProof.Link()),
	)
	require.NoError(t, err)

	proofs := append([]ucan.Delegation{grantProof}, extraProofs...)
	reqOpts := []execution.RequestOption{execution.WithDelegations(proofs...)}
	if cause != nil {
		reqOpts = append(reqOpts, execution.WithInvocations(cause))
	}

	resp, err := s.RPCClient(t).Execute(execution.NewRequest(t.Context(), inv, reqOpts...))
	require.NoError(t, err)
	return resp.Receipt()
}

// assertGrantOK asserts the receipt is a success carrying a GrantOK
// payload with exactly expectedDelegations CIDs. Returns the decoded
// payload for any further per-test assertions.
func assertGrantOK(t *testing.T, rcpt ucan.Receipt, expectedDelegations int) *access.GrantOK {
	t.Helper()
	assertReceiptOK(t, rcpt)

	okBytes, _ := rcpt.Out().Unpack()
	var ok access.GrantOK
	require.NoError(t, ok.UnmarshalCBOR(bytes.NewReader(okBytes)), "decoding GrantOK")
	require.Len(t, ok.Delegations, expectedDelegations)
	return &ok
}

// decodeAllocateOK extracts the typed payload from a successful
// /blob/allocate receipt, surfacing any encoded error name if the
// receipt unexpectedly failed.
func decodeAllocateOK(t *testing.T, rcpt ucan.Receipt) *blob.AllocateOK {
	t.Helper()
	assertReceiptOK(t, rcpt)

	okBytes, _ := rcpt.Out().Unpack()
	var ok blob.AllocateOK
	require.NoError(t, ok.UnmarshalCBOR(bytes.NewReader(okBytes)), "decoding AllocateOK")
	return &ok
}

// decodeAcceptOK extracts the typed payload from a successful
// /blob/accept receipt.
func decodeAcceptOK(t *testing.T, rcpt ucan.Receipt) *blob.AcceptOK {
	t.Helper()
	assertReceiptOK(t, rcpt)

	okBytes, _ := rcpt.Out().Unpack()
	var ok blob.AcceptOK
	require.NoError(t, ok.UnmarshalCBOR(bytes.NewReader(okBytes)), "decoding AcceptOK")
	return &ok
}
