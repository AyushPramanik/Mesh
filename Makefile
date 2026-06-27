.DEFAULT_GOAL := test

# Run all Go tests.
.PHONY: test
test:
	go test ./...

# Static analysis. golangci-lint (config in .golangci.yml) is the standard gate
# and runs in CI; it includes go vet. Locally it falls back to `go vet` if the
# tool is not installed — install it with:
#   go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
.PHONY: lint
lint:
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run ./... || \
		{ echo "golangci-lint not installed; running go vet only"; go vet ./...; }

# Build the CLI and daemon into ./bin for the host platform.
.PHONY: build
build:
	go build -o bin/mesh ./cmd/mesh
	go build -o bin/meshd ./cmd/meshd
	go build -o bin/mesh-mcp ./cmd/mesh-mcp

# Cross-compile release artifacts into ./dist (archives + SHA256SUMS).
# Pass VERSION to stamp the binaries: make release VERSION=v0.1.0
.PHONY: release
release:
	./scripts/release.sh $(VERSION)

# Generate protobuf + gRPC bindings from proto/mesh/v1.
.PHONY: proto
proto:
	./scripts/gen-proto.sh

# Generate type-safe Go from the SQL in internal/store/queries.
# Requires: go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
.PHONY: sqlc
sqlc:
	sqlc generate

.PHONY: tidy
tidy:
	go mod tidy
