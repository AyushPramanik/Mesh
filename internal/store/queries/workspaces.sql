-- name: CreateWorkspace :one
INSERT INTO workspaces (id, agent_id, branch, path)
VALUES (?, ?, ?, ?)
RETURNING *;

-- name: GetWorkspace :one
SELECT * FROM workspaces WHERE id = ?;

-- name: ListWorkspaces :many
SELECT * FROM workspaces ORDER BY created_at ASC;

-- name: ListWorkspacesByStatus :many
SELECT * FROM workspaces WHERE status = ? ORDER BY created_at ASC;

-- name: ListWorkspacesByAgent :many
SELECT * FROM workspaces WHERE agent_id = ? ORDER BY created_at ASC;

-- name: SetWorkspaceStatus :exec
UPDATE workspaces
SET status = ?, updated_at = datetime('now')
WHERE id = ?;

-- name: DeleteWorkspace :exec
DELETE FROM workspaces WHERE id = ?;
