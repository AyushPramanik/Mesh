#!/usr/bin/env bash
# Generate Go bindings for the agent protocol from proto/mesh/v1.
#
# Requires protoc on PATH plus the Go plugins:
#   go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
#   go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
set -euo pipefail

cd "$(dirname "$0")/.."

# Make sure the Go-installed plugins are discoverable.
export PATH="$PATH:$(go env GOPATH)/bin"

protoc \
  --go_out=. --go_opt=paths=source_relative \
  --go-grpc_out=. --go-grpc_opt=paths=source_relative \
  proto/mesh/v1/mesh.proto

echo "generated proto/mesh/v1/*.pb.go"
