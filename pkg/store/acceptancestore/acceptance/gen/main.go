//go:generate go run -tags codegen .

package main

import (
	"os"

	cbg "github.com/whyrusleeping/cbor-gen"

	"github.com/fil-forge/piri/pkg/store/acceptancestore/acceptance"
)

const buildTag = "//go:build !codegen\n\n"

func tag(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile(path, append([]byte(buildTag), data...), 0644); err != nil {
		panic(err)
	}
}

func main() {
	models := []any{
		acceptance.Acceptance{},
		acceptance.Blob{},
		acceptance.Await{},
		acceptance.Promise{},
	}
	const cborFile = "../cbor_gen.go"
	if err := cbg.WriteMapEncodersToFile(cborFile, "acceptance", models...); err != nil {
		panic(err)
	}
	tag(cborFile)
}
