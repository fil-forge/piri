VERSION=$(shell awk -F'"' '/"version":/ {print $$4}' version.json)
COMMIT=$(shell git rev-parse --short HEAD)
DATE=$(shell date -u -Iseconds)
GOFLAGS=-ldflags="-X github.com/fil-forge/piri/pkg/build.version=$(VERSION) -X github.com/fil-forge/piri/pkg/build.Commit=$(COMMIT) -X github.com/fil-forge/piri/pkg/build.Date=$(DATE) -X github.com/fil-forge/piri/pkg/build.BuiltBy=make"
# Curio's vendored PDP code selects on-chain contract addresses by its build tag:
# `2k` (or `debug`) makes it resolve addresses from CURIO_DEVNET_* env vars
# (devnet/smelt); with NO network tag it uses hardcoded MAINNET addresses. Default
# to the 2k devnet build so `make build` works against smelt out of the box;
# `skiff` selects Curio's FFI-free variants (no filecoin-ffi, no supraseal),
# which lets Piri build CGO-free. Network build tags (2k/calibnet) are NOT
# needed: contract addresses are installed from config (curiopdp.SetContractAddresses)
# and the only other network-gated constant Piri's paths touch (BlockGasLimit)
# is network-invariant. Verified by the smelt proving-loop e2e on a skiff-only binary.
TAGS?=-tags "skiff"
# gosigar (via curio→lotus) has a pure-Go path on linux but needs cgo on darwin.
# Default to CGO-free builds on non-darwin platforms; override with CGO_ENABLED=1 if needed.
ifeq ($(shell uname -s),Darwin)
CGO_ENABLED?=1
else
CGO_ENABLED?=0
endif

.PHONY: all build test clean

all: build

build: piri

# piri depends on Go sources - use shell to check if rebuild needed
piri: FORCE
	@if [ ! -f piri ] || \
	   [ -n "$$(find cmd pkg internal -name '*.go' -type f -newer piri 2>/dev/null)" ]; then \
		echo "Building piri..."; \
		CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) $(TAGS) -o ./piri github.com/fil-forge/piri/cmd; \
	fi

FORCE:

test:
	CGO_ENABLED=$(CGO_ENABLED) go test $(TAGS) ./...

clean:
	rm -f ./piri