-- Mesh local state schema (SQLite).
--
-- This is the source of truth for the daemon's persistent state: the agent
-- registry, workspace lifecycle, and the PR queue. It is applied verbatim at
-- daemon startup (see store.go) and is also the schema sqlc reads to generate
-- type-safe Go in internal/store. Edit SQL here first, then run `make sqlc`.
--
-- Timestamps are stored as ISO-8601 text via SQLite's datetime('now') so the
-- schema ports cleanly to Postgres for the hosted tier (a driver swap, per
-- CLAUDE.md) without depending on integer epoch conventions.

PRAGMA foreign_keys = ON;

-- An agent is a single autonomous worker that owns workspaces and submits PRs.
CREATE TABLE IF NOT EXISTS agents (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    registered_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- A workspace is an ephemeral, isolated environment for one agent to complete
-- one task, backed by a git worktree. Invariant (CLAUDE.md): a workspace is
-- always on exactly one branch, owned by exactly one agent.
CREATE TABLE IF NOT EXISTS workspaces (
    id         TEXT PRIMARY KEY,
    agent_id   TEXT NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    branch     TEXT NOT NULL,
    path       TEXT NOT NULL,
    -- active | done | error
    status     TEXT NOT NULL DEFAULT 'active',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_workspaces_agent ON workspaces (agent_id);
CREATE INDEX IF NOT EXISTS idx_workspaces_status ON workspaces (status);

-- The PR queue: agents submit here instead of calling the git host directly.
-- The queue deduplicates by branch (same branch twice -> no-op), orders by
-- priority then submission time, and retries transient failures.
CREATE TABLE IF NOT EXISTS pr_queue (
    id           TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    -- Dedupe key: a branch may sit in the queue at most once.
    branch       TEXT NOT NULL UNIQUE,
    title        TEXT NOT NULL,
    priority     INTEGER NOT NULL DEFAULT 0,
    -- queued | submitting | submitted | merged | failed
    status       TEXT NOT NULL DEFAULT 'queued',
    attempts     INTEGER NOT NULL DEFAULT 0,
    last_error   TEXT,
    -- Earliest time a requeued PR may be retried; NULL means immediately. Drives
    -- exponential backoff without a separate scheduler.
    next_retry_at TEXT,
    submitted_at TEXT,
    created_at   TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Scheduler scan order: highest priority first, then oldest submission.
CREATE INDEX IF NOT EXISTS idx_pr_queue_scan ON pr_queue (status, priority DESC, created_at ASC);

-- An intent is an agent's structured, best-effort declaration of what it is
-- about to modify, registered before work starts. The conflict predictor
-- cross-references active intents to clear or warn a new one. Intents are
-- predictions, not locks (CLAUDE.md): actual conflicts are still caught later.
CREATE TABLE IF NOT EXISTS intents (
    id           TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    -- active | released
    status       TEXT NOT NULL DEFAULT 'active',
    created_at   TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_intents_status ON intents (status);

-- The files an intent expects to touch. Normalised out so overlap between
-- intents is a join, not a scan of serialised blobs. File-level overlap is the
-- first cut of conflict detection; AST-level edges arrive in a later layer.
CREATE TABLE IF NOT EXISTS intent_files (
    intent_id TEXT NOT NULL REFERENCES intents (id) ON DELETE CASCADE,
    path      TEXT NOT NULL,
    PRIMARY KEY (intent_id, path)
);

CREATE INDEX IF NOT EXISTS idx_intent_files_path ON intent_files (path);
