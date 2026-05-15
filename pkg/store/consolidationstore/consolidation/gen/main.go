//go:generate go run .

package main

import (
	"github.com/fil-forge/piri/pkg/store/consolidationstore/consolidation"
	cbg "github.com/whyrusleeping/cbor-gen"
)

func main() {
	models := []any{
		consolidation.Consolidation{},
	}

	if err := cbg.WriteMapEncodersToFile("../cbor_gen.go", "consolidation", models...); err != nil {
		panic(err)
	}
}
