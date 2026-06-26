-- name: RegisterAgent :one
-- Register an agent, or update its name if the id already exists. Idempotent so
-- a reconnecting agent does not error.
INSERT INTO agents (id, name)
VALUES (?, ?)
ON CONFLICT (id) DO UPDATE SET name = excluded.name
RETURNING *;

-- name: GetAgent :one
SELECT * FROM agents WHERE id = ?;

-- name: ListAgents :many
SELECT * FROM agents ORDER BY registered_at ASC;
