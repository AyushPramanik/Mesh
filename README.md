# Mesh

**Git wasn't built for agents.** If you're running more than three AI agents
against the same repo, you've already hit the wall: clones everywhere, CI queues
saturated, and "conflict-free" merges that quietly break because two agents
edited different files that depend on each other. Mesh fixes that.

```sh
curl -fsSL https://raw.githubusercontent.com/AyushPramanik/Mesh/main/scripts/install.sh | sh
cd your-repo && mesh init && meshd --repo .
```

Then, from a cold start, watch two agents work in parallel, a semantic conflict
get caught, and their PRs land together in a merge train — in five minutes:

**→ [Quickstart](docs/QUICKSTART.md)**

```
▶ Each agent edits a DIFFERENT file — but Beta references a symbol Alpha changed
mesh conflict mesh/agent-alpha/… mesh/agent-beta/…:
dependency  Login                         # git sees no conflict. Mesh does.

▶ Land the train into main
landed mesh/agent-alpha/… (384cc06)
landed mesh/agent-beta/…  (7bb11e0)
landed 2 PR(s) into the base branch
```

---

## What it does

Mesh is an orchestration layer between AI coding agents and your git host. It
solves three problems that only appear at agent scale:

- **Workspace explosion.** Agents each clone the whole repo. Mesh replaces N
  clones with one shared object store and N lightweight worktrees (~10ms to spin
  up, not ~60s to clone).
- **PR throughput ceiling.** Agents saturate CI and hit rate limits. Mesh
  queues, dedupes, and batches PRs into **merge trains** so throughput is bounded
  by compute, not tooling.
- **Silent semantic conflicts.** Two agents edit different files that import each
  other. Git sees no conflict; Mesh's **AST-level predictor** catches it — before
  the work even starts.

## How agents use it

The agent-native surface is a typed gRPC protocol; the CLI and an **MCP server**
are thin clients of it. An agent's loop looks like:

```
register intent  →  create workspace  →  edit + commit  →  submit PR
      │                                                        │
   conflict check (file + AST)                          queue → merge train → land
```

- **MCP** — point any LLM agent (Claude Code included) at `mesh-mcp`; it gets
  tools like `mesh_register_intent`, `mesh_commit_workspace`, `mesh_submit_pr`,
  `mesh_analyze_conflicts`, and `mesh_land_train`.
- **CLI** — `mesh workspace`, `mesh pr`, `mesh conflict`, `mesh land`, `mesh doctor`.
- **gRPC** — `proto/mesh/v1`, with streaming for progressive results.

Mesh is **not** a git replacement: storage stays standard git, and agents can run
raw git inside their workspace any time.

## Architecture

```
agents / MCP / CLI ──gRPC──► meshd (Go daemon, Unix socket)
                               ├─ workspace manager   (go-git worktrees)
                               ├─ conflict predictor  (intents + go/ast graph)
                               ├─ PR queue + merge train
                               ├─ GitHub client       (go-github)
                               └─ SQLite (local state) + HTTP/SSE (dashboard)
```

## Build from source

```sh
make build      # builds bin/mesh, bin/meshd, bin/mesh-mcp
make test       # all tests
make lint       # go vet (+ golangci-lint if installed)
```

See [CLAUDE.md](CLAUDE.md) for the full design, conventions, and build order.

## Status

Pre-v0.1. The core loop — isolated workspaces, file- and AST-level conflict
detection, the PR queue with backoff, merge-train planning, and local landing —
works end-to-end today (`bash scripts/quickstart-demo.sh`). GitHub PR submission
is wired; native merge-queue integration and multi-language AST (Tree-sitter)
are next.
