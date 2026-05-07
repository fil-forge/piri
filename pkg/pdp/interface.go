package pdp

import (
	"github.com/fil-forge/piri/pkg/pdp/aggregation/commp"
	"github.com/fil-forge/piri/pkg/pdp/types"
)

type PDP interface {
	API() types.PieceAPI
	CommpCalculate() commp.Calculator
}
