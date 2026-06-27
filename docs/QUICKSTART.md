# Quickstart

From a cold start to two agents working in parallel, a conflict caught, and PRs
landing in a merge train — in about five minutes. No GitHub account needed for
the local walkthrough.

## 1. Install

```sh
curl -fsSL https://raw.githubusercontent.com/AyushPramanik/Mesh/main/scripts/install.sh | sh
```

Requires `git` and Go 1.25+. This builds `mesh`, `meshd`, and `mesh-mcp` and
installs them to `~/.local/bin` (override with `MESH_BIN`). Make sure that
directory is on your `PATH`.

> Prefer to see it without installing anything? Clone the repo, run
> `make build`, then `bash scripts/quickstart-demo.sh` — it runs everything
> below in a throwaway sandbox.

## 2. Initialize a repo

```sh
cd your-repo          # any git repository
mesh init
```

```
✓ initialized Mesh in /path/to/your-repo
  state: /path/to/your-repo/.mesh
✓ detected GitHub repo: your-org/your-repo
```

`init` creates `.mesh/` for local state, adds it to `.gitignore`, and auto-detects
your GitHub `origin` so you never have to type owner/repo.

## 3. Start the daemon

```sh
meshd --repo .
```

Leave it running. In another terminal:

```sh
mesh doctor
```

```
✓ daemon: reachable
✗ github: not configured — set GITHUB_TOKEN to enable PR submission
```

That `✗` is expected — the local demo doesn't need GitHub. To enable real PR
submission, see [GitHub setup](#github-setup) below.

## 4. Spin up two agents

Each agent gets an isolated, worktree-backed workspace on its own branch:

```sh
mesh workspace create --agent agent-alpha
mesh workspace create --agent agent-beta
```

```
created workspace 761823099fd3 on branch mesh/agent-alpha/761823099fd3
/path/to/your-repo-worktrees/761823099fd3
```

Each agent edits inside its own worktree path — fully isolated, ~10ms to create,
sharing one object store instead of a full clone each.

## 5. Catch a conflict git can't see

Say `agent-alpha` changes the definition of a function in `auth/auth.go`, and
`agent-beta` adds a call to it in `api/handler.go`. **Different files** — plain
git, and file-level checks, see no conflict. Mesh does:

```sh
mesh conflict mesh/agent-alpha/<id> mesh/agent-beta/<id>
```

```
dependency  Login
```

That's a *silent semantic conflict*: the branches don't touch the same file, but
one changes a symbol the other depends on. (Agents can also call
`mesh_register_intent` up front to get this warning *before* they start editing.)

## 6. Queue PRs and land a merge train

```sh
mesh pr submit --workspace <alpha-id> --branch mesh/agent-alpha/<id> --title "auth: require scope"
mesh pr submit --workspace <beta-id>  --branch mesh/agent-beta/<id>  --title "api: call Login"
mesh pr trains
```

```
train 1: [mesh/agent-alpha/761823099fd3 mesh/agent-beta/4f0b35933b98]
```

Non-conflicting PRs are batched into one train. Land it:

```sh
mesh land
```

```
landed mesh/agent-alpha/761823099fd3 (384cc06)
landed mesh/agent-beta/4f0b35933b98 (7bb11e0)
landed 2 PR(s) into the base branch
```

Both features are now on `main` — merged in one batch, locally, no PR gate.

---

## GitHub setup

To submit PRs to GitHub (rather than landing locally), the daemon needs a token:

```sh
export GITHUB_TOKEN=ghp_…        # a fine-grained or classic token with 'repo' scope
meshd --repo .
mesh doctor
```

```
✓ github: ok — submitting PRs to your-org/your-repo
```

- **Owner/repo** are auto-detected from your `origin` remote. Override with
  `GITHUB_OWNER` / `GITHUB_REPO`, and set the base branch with `GITHUB_BASE`
  (default `main`).
- The daemon **verifies the token at startup**. Common failures get a clear
  message instead of a stack trace, e.g.:
  - `token is invalid or expired (401) — generate a new token with 'repo' scope`
  - `token lacks the 'repo' scope (has: read:org) — create a token with repo access`
  - `your-org/your-repo not found, or the token cannot access it (404)`
- If credentials are missing or rejected, the daemon still runs — PRs queue up
  and submit once you fix the token. Re-run `mesh doctor` to confirm.

## Agents via MCP

`mesh-mcp` is a Model Context Protocol server exposing all of the above as tools
(`mesh_create_workspace`, `mesh_register_intent`, `mesh_commit_workspace`,
`mesh_submit_pr`, `mesh_analyze_conflicts`, `mesh_land_train`, …). Point your MCP
client at it with `MESH_REPO` set to the repo, and any LLM agent can drive Mesh
directly.
