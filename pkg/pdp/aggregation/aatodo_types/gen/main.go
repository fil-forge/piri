//go:generate go run .

package main

import (
	jsg "github.com/alanshaw/dag-json-gen"
	cbg "github.com/whyrusleeping/cbor-gen"

	"github.com/fil-forge/piri/pkg/pdp/aggregation/aatodo_types"
)

func main() {
	models := []any{
		aatodo_types.PieceLink{},
		aatodo_types.Buffer{},
		aatodo_types.ProofData{},
		aatodo_types.AggregatePiece{},
		aatodo_types.Aggregate{},
		aatodo_types.Aggregation{},
		aatodo_types.Blob{},
	}

	if err := cbg.WriteMapEncodersToFile("../cbor_gen.go", "aatodo_types", models...); err != nil {
		panic(err)
	}

	if err := jsg.WriteMapEncodersToFile("../json_gen.go", "aatodo_types", models...); err != nil {
		panic(err)
	}
}
