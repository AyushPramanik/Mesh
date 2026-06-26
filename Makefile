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

# Cross-compile release binaries. Targets are added as cmd/mesh and cmd/meshd
# land (build order step 4).
.PHONY: build
build:
	@echo "no binaries yet; cmd/mesh and cmd/meshd land in build-order step 4"

# Generate type-safe Go from the SQL in internal/store/queries.
# Requires: go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
.PHONY: sqlc
sqlc:
	sqlc generate

.PHONY: tidy
tidy:
	go mod tidy
