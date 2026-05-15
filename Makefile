VERSION=$(shell awk -F'"' '/"version":/ {print $$4}' version.json)
COMMIT=$(shell git rev-parse --short HEAD)
DATE=$(shell date -u -Iseconds)
GOFLAGS=-ldflags="-X github.com/fil-forge/piri/pkg/build.version=$(VERSION) -X github.com/fil-forge/piri/pkg/build.Commit=$(COMMIT) -X github.com/fil-forge/piri/pkg/build.Date=$(DATE) -X github.com/fil-forge/piri/pkg/build.BuiltBy=make"
TAGS?=

.PHONY: all build test clean fmt fmt-check

all: build

build: piri

# piri depends on Go sources - use shell to check if rebuild needed
piri: FORCE
	@if [ ! -f piri ] || \
	   [ -n "$$(find cmd pkg internal -name '*.go' -type f -newer piri 2>/dev/null)" ]; then \
		echo "Building piri..."; \
		go build $(GOFLAGS) $(TAGS) -o ./piri github.com/fil-forge/piri/cmd; \
	fi

FORCE:

test:
	go test ./...

clean:
	rm -f ./piri

fmt:
	@command -v golangci-lint >/dev/null || { echo "golangci-lint not installed: https://golangci-lint.run/usage/install/"; exit 1; }
	golangci-lint run --fix --enable-only=gci ./...

fmt-check:
	@command -v golangci-lint >/dev/null || { echo "golangci-lint not installed: https://golangci-lint.run/usage/install/"; exit 1; }
	golangci-lint run --enable-only=gci ./...