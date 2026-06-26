-- name: CreateIntent :one
INSERT INTO intents (id, workspace_id)
VALUES (?, ?)
RETURNING *;

-- name: AddIntentFile :exec
INSERT INTO intent_files (intent_id, path)
VALUES (?, ?)
ON CONFLICT DO NOTHING;

-- name: ReleaseIntent :exec
UPDATE intents SET status = 'released' WHERE id = ?;

-- name: ReleaseWorkspaceIntents :exec
UPDATE intents SET status = 'released' WHERE workspace_id = ?;

-- name: FindOverlappingIntents :many
-- Active intents owned by other workspaces that claim any of the given paths.
-- This is the file-level overlap check at the heart of the conflict predictor.
SELECT i.workspace_id, f.path
FROM intent_files f
JOIN intents i ON i.id = f.intent_id
WHERE i.status = 'active'
  AND i.workspace_id != sqlc.arg(workspace_id)
  AND f.path IN (sqlc.slice('paths'));
