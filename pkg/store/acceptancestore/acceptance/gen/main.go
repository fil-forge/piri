//go:generate go run .

package main

import (
	"github.com/fil-forge/piri/pkg/store/acceptancestore/acceptance"
	cbg "github.com/whyrusleeping/cbor-gen"
)

func main() {
	models := []any{
		acceptance.Acceptance{},
		acceptance.Blob{},
		acceptance.Promise{},
		acceptance.Await{},
	}

	if err := cbg.WriteMapEncodersToFile("../cbor_gen.go", "acceptance", models...); err != nil {
		panic(err)
	}
}
