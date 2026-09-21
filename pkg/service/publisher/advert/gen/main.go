//go:generate go run -tags codegen .

package main

import (
	"os"

	cbg "github.com/whyrusleeping/cbor-gen"

	"github.com/fil-forge/piri/pkg/service/publisher/advert"
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
		advert.Spec{},
	}
	const cborFile = "../cbor_gen.go"
	if err := cbg.WriteMapEncodersToFile(cborFile, "advert", models...); err != nil {
		panic(err)
	}
	tag(cborFile)
}
