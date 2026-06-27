# Contributing to Mesh

Thanks for your interest in Mesh. This is a pre-v0.1 project moving fast, so a
quick issue before a big change saves everyone time.

## Before you start

- **Open an issue** before starting any non-trivial feature, so we can agree on
  the approach before you write code.
- Read [CLAUDE.md](CLAUDE.md) for the architecture, core concepts, and the
  conventions every PR is held to.

## Development setup

Requires **Go 1.25+**, plus Node 20+ for the dashboard.

```sh
go mod tidy
cd dashboard && npm install && cd ..

make build      # builds bin/mesh, bin/meshd, bin/mesh-mcp
make test       # all Go tests
make lint       # go vet (+ golangci-lint if installed)
```

If you change the proto or SQL, regenerate the bindings:

```sh
make proto      # protobuf + gRPC from proto/mesh/v1
make sqlc       # type-safe Go from internal/store/queries (requires sqlc)
```

## What every PR needs

- **Unit tests** for new behaviour (table-driven, `testify/assert`).
- **Updated docs** if behaviour changes.
- A green CI run: `make build`, `make test`, `make lint`, `gofmt`, and a tidy
  `go.mod` all pass (see `.github/workflows/ci.yml`).

## Conventions

- **Commits**: imperative mood, ≤72-char subject, blank line before body.
  Example: `workspace: recycle stale worktrees on daemon startup`.
- **Go**: wrap errors with the function name (`fmt.Errorf("workspace.Create: %w", err)`),
  pass `ctx` first on any I/O function, define interfaces at the point of use,
  no package-level mutable state. Full list in [CLAUDE.md](CLAUDE.md#code-conventions).
- **Branches**: feature work goes on a branch and is squash-merged to `main`.
  `main` is always releasable.

## License

By contributing, you agree that your contributions are licensed under the
**GNU AGPL-3.0**, the same license that covers the project (see [LICENSE](LICENSE)).
