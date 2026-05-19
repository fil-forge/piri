//go:generate go run -tags codegen .

package main

import (
	"os"

	cbg "github.com/whyrusleeping/cbor-gen"

	"github.com/fil-forge/piri/pkg/store/consolidationstore/consolidation"
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
		consolidation.Consolidation{},
	}
	const cborFile = "../cbor_gen.go"
	if err := cbg.WriteMapEncodersToFile(cborFile, "consolidation", models...); err != nil {
		panic(err)
	}
	tag(cborFile)
}
