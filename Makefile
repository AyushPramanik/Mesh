.DEFAULT_GOAL := test

# Run all Go tests.
.PHONY: test
test:
	go test ./...

# Static analysis. golangci-lint is added once it is wired into CI; until then
# `go vet` is the baseline that must always pass.
.PHONY: lint
lint:
	go vet ./...
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run || \
		echo "golangci-lint not installed; ran go vet only"

# Build the CLI and daemon into ./bin for the host platform.
.PHONY: build
build:
	go build -o bin/mesh ./cmd/mesh
	go build -o bin/meshd ./cmd/meshd

# Generate type-safe Go from the SQL in internal/store/queries.
# Requires: go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
.PHONY: sqlc
sqlc:
	sqlc generate

.PHONY: tidy
tidy:
	go mod tidy
