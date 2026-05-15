package claimstore

import (
	"github.com/fil-forge/piri/pkg/store/invocationstore"
)

// ClaimStore stores signed content claims. In UCAN 1.0, a claim is a signed
// invocation (e.g. an `/assert/location` invocation), so claim storage is
// backed by the invocation store.
type ClaimStore interface {
	invocationstore.InvocationStore
}
