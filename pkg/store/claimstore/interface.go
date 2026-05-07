package claimstore

import (
	"github.com/fil-forge/piri/pkg/store/delegationstore"
)

// TODO a glorified type alias, remove this
type ClaimStore interface {
	delegationstore.DelegationStore
}
