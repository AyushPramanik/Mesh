-- name: EnqueuePR :one
-- Submit a PR to the queue. The branch is the dedupe key: submitting a branch
-- that is already queued is a no-op that returns the existing row unchanged.
INSERT INTO pr_queue (id, workspace_id, branch, title, priority)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (branch) DO UPDATE SET branch = excluded.branch
RETURNING *;

-- name: GetPR :one
SELECT * FROM pr_queue WHERE id = ?;

-- name: GetPRByBranch :one
SELECT * FROM pr_queue WHERE branch = ?;

-- name: ListPRsByStatus :many
-- Scheduler scan order: highest priority first, then oldest submission.
SELECT * FROM pr_queue
WHERE status = ?
ORDER BY priority DESC, created_at ASC;

-- name: ListDuePRs :many
-- Queued PRs eligible to process now: never deferred, or past their backoff.
-- Same scan order as ListPRsByStatus.
SELECT * FROM pr_queue
WHERE status = 'queued'
  AND (next_retry_at IS NULL OR next_retry_at <= datetime('now'))
ORDER BY priority DESC, created_at ASC;

-- name: SetPRStatus :exec
UPDATE pr_queue SET status = ? WHERE id = ?;

-- name: MarkPRSubmitted :exec
UPDATE pr_queue
SET status = 'submitted', submitted_at = datetime('now')
WHERE id = ?;

-- name: RequeuePR :exec
-- Defer a PR after a transient failure: back to queued, attempt counted, and
-- not retried until now + the given SQLite modifier (e.g. '+8 seconds').
UPDATE pr_queue
SET status = 'queued',
    attempts = attempts + 1,
    last_error = sqlc.arg(last_error),
    next_retry_at = datetime('now', sqlc.arg(backoff))
WHERE id = sqlc.arg(id);

-- name: RecordPRFailure :exec
-- Terminally fail a PR (permanent error or retries exhausted).
UPDATE pr_queue
SET status = 'failed', attempts = attempts + 1, last_error = ?
WHERE id = ?;

-- name: DeletePR :exec
DELETE FROM pr_queue WHERE id = ?;
