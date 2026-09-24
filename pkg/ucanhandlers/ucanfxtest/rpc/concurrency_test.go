package rpc_test

import (
	"bytes"

	"github.com/fil-forge/libforge/commands/blob"
	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/ucantone/binding"
	"github.com/fil-forge/ucantone/errors/datamodel"
	"github.com/fil-forge/ucantone/execution"
	"github.com/fil-forge/ucantone/execution/batch"
	"github.com/fil-forge/ucantone/server"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/command"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/stretchr/testify/require"
)

// panicCommand is registered on the RPC server by the suite so tests can
// check that a panicking handler is recovered into a receipt.
var panicCommand = binding.Bind[*blob.RejectArguments, *blob.RejectOK](command.MustParse("/test/panic"))

func newPanicRoute() server.Route {
	return panicCommand.Route(func(*binding.Request[*blob.RejectArguments], *binding.Response[*blob.RejectOK]) error {
		panic("test handler panic")
	})
}

func (s *RPCSuite) TestHandlerPanic_ReturnsExecutionFailure() {
	t := s.T()
	inv := testutil.Must(panicCommand.Invoke(
		s.ServiceID,
		s.ServiceID.DID(),
		&blob.RejectArguments{Space: testutil.RandomDID(t), Digest: testutil.RandomMultihash(t)},
		invocation.WithAudience(s.ServiceID.DID()),
	))(t)

	rcpt := s.sendInvocation(t, inv)
	assertReceiptFailure(t, rcpt, execution.ExecutionFailureErrorName)
}

// TestBatch_ConcurrentAllocations sends many /blob/allocate invocations in one
// request. The server executes them concurrently, so running this test with
// -race checks the allocate path against piri's stores.
func (s *RPCSuite) TestBatch_ConcurrentAllocations() {
	t := s.T()
	const count = 20
	space := testutil.RandomDID(t)

	invs := make([]ucan.Invocation, 0, count)
	for range count {
		invs = append(invs, testutil.Must(blob.Allocate.Invoke(
			s.ServiceID,
			s.ServiceID.DID(),
			&blob.AllocateArguments{
				Space: space,
				Blob:  blob.Blob{Digest: testutil.RandomMultihash(t), Size: 123},
				Cause: testutil.RandomCID(t),
			},
			invocation.WithAudience(s.ServiceID.DID()),
		))(t))
	}

	res, err := s.RPCClient(t).ExecuteBatch(batch.NewRequest(t.Context(), invs))
	require.NoError(t, err)

	var failures []string
	for _, inv := range invs {
		rcpt, ok := res.Receipt(inv.Task().Link())
		if !ok {
			failures = append(failures, inv.Task().Link().String()+": no receipt")
			continue
		}
		if rcpt.Out().IsErr() {
			_, errBytes := rcpt.Out().Unpack()
			var em datamodel.ErrorModel
			_ = em.UnmarshalCBOR(bytes.NewReader(errBytes))
			failures = append(failures, inv.Task().Link().String()+": "+em.ErrorName+": "+em.Message)
		}
	}
	require.Empty(t, failures)
}
