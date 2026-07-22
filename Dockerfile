# Build stage - use native platform for faster cross-compilation
FROM --platform=$BUILDPLATFORM golang:1.26-bookworm AS build

ARG TARGETARCH
ARG TARGETOS=linux

WORKDIR /src

# Copy dependency files first for better layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build with cross-compilation and stripped binary
# skiff selects Curio's FFI-free variants (CGO-free build); see Makefile for
# why no other tags are needed.
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -tags "skiff" \
    -ldflags="-s -w" \
    -o /app \
    ./cmd

# Runtime stage - alpine for wget healthchecks per RFC
FROM alpine:latest AS prod

USER nobody

# Copy binary from build stage
COPY --from=build /app /usr/bin/piri

ENTRYPOINT ["/usr/bin/piri"]
