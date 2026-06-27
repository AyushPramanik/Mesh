# Mesh

> Agent-native version control. Git was built for humans committing a few times a day.
> Mesh is built for AI agents committing thousands of times a day, in parallel.

---

## What Mesh is

Mesh is an open-source orchestration layer that sits between AI coding agents and git hosts
(GitHub, GitLab). It solves three problems that don't exist in human-paced development but
become critical at agent scale:

1. **Workspace explosion** — agents each clone the full repo. Mesh replaces N clones with one
   shared git object store and N lightweight worktrees (~10ms to spin up vs ~60s clone).

2. **PR throughput ceiling** — agents saturate CI queues and GitHub rate limits. Mesh queues,
   batches, and schedules PRs using merge trains so throughput is bounded by compute, not tooling.

3. **Silent semantic conflicts** — two agents edit different files that import each other. Git
   sees no conflict; Mesh's AST-level conflict predictor catches it before work even starts.

The long-term vision is bigger: Mesh is the foundation for a new VCS primitive designed
around parallelism as the default, structured machine-readable provenance, and continuous
merge instead of the PR-gated model.

---

## Architecture

```
agents / IDE plugins / CLI
         │
    gRPC + protobuf          ← typed agent protocol, language-agnostic
         │
   mesh daemon (Go)          ← Unix socket, manages all local state
    ┌────┴─────────────────────────────────────────────┐
    │  workspace manager     conflict predictor         │
    │  (go-git worktrees)    (Tree-sitter AST graphs)   │
    │                                                   │
    │  PR orchestrator       GitHub/GitLab client       │
    │  (queue + merge train) (go-github + GraphQL)      │
    └────┬─────────────────────────────────────────────┘
         │
    SQLite (local)           ← workspace state, PR queue, agent registry
    git object store         ← single shared clone on disk
         │
    Postgres (hosted)        ← multi-repo, multi-user SaaS tier
```

---

## Repository layout

```
mesh/
├── cmd/
│   ├── mesh/               # CLI entrypoint (cobra)
│   └── meshd/              # Daemon entrypoint
├── internal/
│   ├── workspace/          # Worktree lifecycle (create, gc, snapshot)
│   ├── conflict/           # Tree-sitter AST diff + dependency graph
│   ├── queue/              # PR queue, merge train scheduler
│   ├── git/                # go-git wrapper (thin, keep side-effect-free)
│   ├── github/             # go-github + GraphQL client
│   ├── daemon/             # gRPC server, request handlers
│   └── store/              # SQLite schema + queries (sqlc generated)
├── proto/
│   └── mesh/v1/            # .proto definitions for agent protocol
├── dashboard/              # React + TypeScript web UI
│   ├── src/
│   │   ├── components/
│   │   ├── hooks/
│   │   └── pages/
│   └── package.json
├── docs/
├── scripts/
│   ├── dev-setup.sh
│   └── gen-proto.sh
├── CLAUDE.md               # this file
├── go.mod
└── Makefile
```

---

## Tech stack

| Layer | Technology | Why |
|---|---|---|
| Daemon + CLI | Go 1.25+ | Single static binary, fast startup, goroutine concurrency, large OSS contributor pool |
| Git operations | go-git | Pure Go, no CGO, no system git version dependency |
| Agent protocol | gRPC + protobuf | Typed, versioned, streaming, generated clients for Python/TS/Go |
| AST conflict detection | Tree-sitter (Go bindings) | 50+ languages, incremental, fast, used by VS Code/Neovim |
| Local state | SQLite via sqlc | Zero-config, embedded, sqlc generates type-safe Go from SQL |
| GitHub/GitLab API | go-github + GraphQL | go-github for REST, GraphQL for batch operations and merge queue |
| Dashboard | React 18 + TypeScript | — |
| Dashboard styling | Tailwind CSS | — |
| Live updates | Server-Sent Events | Dashboard is read-heavy; SSE is enough, no WebSocket complexity |
| Hosted DB | Postgres | SQLite → Postgres is a driver swap when sqlc targets both |

---

## Core concepts

### Workspace
An ephemeral, isolated environment for one agent to complete one task. Backed by a
`git worktree` — shares the `.git` object store, has its own working tree and branch.
Created in ~10ms. Automatically cleaned up when the task finishes or errors.

Key invariant: **a workspace is always on exactly one branch, owned by exactly one agent**.
Never share workspaces between agents.

### Intent
Before an agent starts work, it registers an **intent**: a structured declaration of which
files, packages, or symbols it expects to modify. The conflict predictor cross-references all
active intents and either clears the new one, delays it, or surfaces a warning.

Intents live in SQLite and are keyed by workspace ID. They are best-effort predictions, not
locks. The system will still catch actual conflicts at AST diff time.

### PR queue
Agents do not call the GitHub API directly. They submit to the Mesh PR queue. The queue:

- Deduplicates (same branch submitted twice → no-op)
- Orders by priority (user-set) then submission time
- Groups non-conflicting PRs into merge train batches
- Rate-shapes submissions to avoid GitHub abuse detection
- Retries on transient errors with exponential backoff

The queue is persisted in SQLite. On daemon restart, in-flight submissions are retried.

### Merge train
A merge train is an ordered sequence of PRs that can land without conflicts. Mesh builds
trains by walking the conflict graph: if PR A and PR B touch disjoint file sets (and their
AST dependency graphs don't intersect), they go in the same train. The train is submitted
to GitHub's native merge queue where available; otherwise Mesh serialises submissions itself.

### Conflict graph
Built by Tree-sitter. For each workspace branch, Mesh parses changed files and extracts:

- **Direct conflicts**: both branches modify the same AST node (function body, struct field)
- **Dependency conflicts**: branch A modifies a symbol that branch B imports or calls
- **Structural conflicts**: branch A adds a parameter to a function that branch B calls

The graph is stored in SQLite and rebuilt on each commit. Edges are weighted by conflict
probability. The scheduler uses edge weights when ordering the merge train.

---

## Development commands

```bash
# Install dependencies
go mod tidy
cd dashboard && npm install

# Generate protobuf bindings
make proto          # runs scripts/gen-proto.sh

# Generate sqlc query code
make sqlc           # requires sqlc installed: go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest

# Run daemon (dev mode, verbose logging)
go run ./cmd/meshd --dev

# Run CLI against local daemon
go run ./cmd/mesh workspace list

# Run all tests
make test

# Run tests for a single package
go test ./internal/conflict/...

# Run dashboard dev server
cd dashboard && npm run dev

# Lint (golangci-lint + eslint)
make lint

# Build release binaries (cross-compile linux/darwin/windows)
make build
```

---

## Code conventions

### Go

- **Package names** are single lowercase words matching the directory name. No underscores.
- **Error wrapping**: always `fmt.Errorf("workspace.Create: %w", err)` — include the
  function name so stack traces are readable without a debugger.
- **Context**: every function that does I/O takes `ctx context.Context` as its first argument.
  Never store a context in a struct.
- **Interfaces**: define interfaces at the point of use (in the consumer package), not in the
  provider package. This keeps the dependency graph acyclic and makes mocking trivial.
- **No global state**: daemon state lives in a `*Daemon` struct passed through the call tree.
  No package-level `var` for mutable state.
- **SQLite queries**: all queries live in `internal/store/queries/`. Write SQL first, run
  `make sqlc` to generate the Go. Never hand-write SQL string concatenation.
- **go-git side effects**: all go-git calls that mutate disk state live in `internal/git/`.
  Functions in other packages may read from the git object store but must not write to it
  directly — they call `internal/git` functions.
- **Tests**: table-driven, one `_test.go` file per package, `testify/assert` for assertions.
  Integration tests that need a real git repo use `t.TempDir()` and `git.InitMemory()`.

### Proto / gRPC

- Message and service names are PascalCase. Field names are snake_case.
- Every RPC that can produce more than one result uses server streaming, not repeated fields
  in a single response. This lets the dashboard render progressively.
- Breaking proto changes get a new major version (`mesh/v2/`). Never break `v1` in place.

### TypeScript / React

- Functional components only. No class components.
- State that is derived from server data lives in a React Query cache, not `useState`.
- SSE connection is managed by a single `useMeshStream()` hook in `hooks/useMeshStream.ts`.
  Components subscribe to slices of the stream; they do not open their own connections.
- Tailwind only for styling. No inline `style={{}}` except for dynamic values that cannot
  be expressed as Tailwind classes (e.g. `width: ${pct}%`).

---

## Key files to read before touching an area

| Area | Files |
|---|---|
| Workspace lifecycle | `internal/workspace/manager.go`, `internal/git/worktree.go` |
| Conflict detection | `internal/conflict/predictor.go`, `internal/conflict/graph.go`, `internal/conflict/parser.go`, `internal/conflict/languages.go` |
| PR queue + merge train | `internal/queue/queue.go`, `internal/queue/train.go` |
| gRPC server | `internal/daemon/server.go`, `proto/mesh/v1/mesh.proto` |
| SQLite schema | `internal/store/schema.sql`, `internal/store/queries/` |
| GitHub integration | `internal/github/client.go`, `internal/github/mergequeue.go` |
| Dashboard live stream | `dashboard/src/hooks/useMeshStream.ts` |

---

## What Mesh is not (scope boundaries)

- **Not a git replacement.** Mesh orchestrates git; the underlying storage format is standard
  git. Agents can still use raw git commands inside their workspace if needed.
- **Not a CI system.** Mesh submits PRs and respects CI results; it does not run tests itself.
- **Not an agent framework.** Mesh does not decide what code to write. It manages where agents
  work and how their output reaches the main branch.
- **Not a code review tool.** The PR review workflow for human reviewers is unchanged.
  Mesh only accelerates the agent-authored, CI-validated portion of the pipeline.

---

## Current status

Pre-v0.1. The full core loop is built and the layers below were shipped in order, each
with integration tests validating the contract the next depends on:

1. ✅ `internal/git` — worktree create/delete/list wrapping go-git
2. ✅ `internal/store` — SQLite schema + sqlc for workspace and queue tables
3. ✅ `internal/workspace` — workspace manager using (1) and (2)
4. ✅ `cmd/mesh` + `cmd/meshd` — CLI and daemon
5. ✅ `internal/conflict` — file-level overlap **and** multi-language symbol-graph
   detection (Go via `go/ast`; Python/JS/TS/Java/Rust/Ruby/C/C++/C#/PHP/Swift/Kotlin/Scala/Shell
   via pure-Go heuristic parsers)
6. ✅ `internal/queue` — PR queue with GitHub REST submission and backoff
7. ✅ `proto/` + gRPC wiring — typed agent protocol; CLI and MCP server are clients
8. ✅ Merge train scheduler over file footprints, plus local train landing
9. ✅ `dashboard/` — React + SSE UI

Known gaps / next up:

- **Tree-sitter parse fidelity** — multi-language conflict detection ships today via a
  pluggable parser registry (`internal/conflict/parser.go`): Go uses a real `go/ast` parse,
  while Python/JS/TS/Java/Rust/Ruby use pure-Go heuristic parsers (declaration-pattern
  matching after comment/string stripping — `internal/conflict/languages.go`). This keeps the
  no-CGO, single-static-binary, cross-compile invariants. The tech-stack table's Tree-sitter
  backend is the next fidelity upgrade: it can replace any language behind the `symbolParser`
  interface — and reuses the same `SymbolGraph` shape — without touching callers. Add a new
  heuristic language by appending one `langSpec` to `heuristicLangs`.
- **Native merge-queue integration** — trains land locally; GitHub merge-queue submission is
  wired for PRs but not yet driving the train end-to-end.
- **Hosted tier** — Postgres backend and multi-repo/multi-user SaaS are design-only.

---

## Contributing

- Open an issue before starting any non-trivial feature.
- Every PR needs: unit tests, updated docs if behaviour changes, and a passing `make lint`.
- Commit messages: imperative mood, 72-char subject, blank line before body.
  Example: `workspace: recycle stale worktrees on daemon startup`
- The `main` branch is always releasable. Feature work goes on branches; squash-merge to main.
  also dont co author commit with claude
