//go:generate go run .

package main

import (
	"github.com/fil-forge/piri/pkg/store/allocationstore/allocation"
	cbg "github.com/whyrusleeping/cbor-gen"
)

func main() {
	models := []any{
		allocation.Allocation{},
		allocation.Blob{},
	}

	if err := cbg.WriteMapEncodersToFile("../cbor_gen.go", "allocation", models...); err != nil {
		panic(err)
	}
}
