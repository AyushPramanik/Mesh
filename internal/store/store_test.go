package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newStore opens an isolated in-memory store for a test.
func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	return s
}

// seedWorkspace registers an agent and creates a workspace, returning the
// workspace id. Most queue tests need a workspace to satisfy the foreign key.
func seedWorkspace(t *testing.T, s *Store) string {
	t.Helper()
	ctx := context.Background()
	_, err := s.RegisterAgent(ctx, RegisterAgentParams{ID: "agent-1", Name: "claude"})
	require.NoError(t, err)
	ws, err := s.CreateWorkspace(ctx, CreateWorkspaceParams{
		ID:      "ws-1",
		AgentID: "agent-1",
		Branch:  "feature/x",
		Path:    "/tmp/ws-1",
	})
	require.NoError(t, err)
	return ws.ID
}

func TestRegisterAgent_Idempotent(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	a1, err := s.RegisterAgent(ctx, RegisterAgentParams{ID: "a", Name: "first"})
	require.NoError(t, err)
	assert.Equal(t, "first", a1.Name)

	// Re-registering the same id updates the name instead of erroring.
	a2, err := s.RegisterAgent(ctx, RegisterAgentParams{ID: "a", Name: "second"})
	require.NoError(t, err)
	assert.Equal(t, "second", a2.Name)

	agents, err := s.ListAgents(ctx)
	require.NoError(t, err)
	assert.Len(t, agents, 1)
}

func TestWorkspaceLifecycle(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	id := seedWorkspace(t, s)

	ws, err := s.GetWorkspace(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "active", ws.Status)
	assert.NotEmpty(t, ws.CreatedAt)

	require.NoError(t, s.SetWorkspaceStatus(ctx, SetWorkspaceStatusParams{
		Status: "done", ID: id,
	}))

	active, err := s.ListWorkspacesByStatus(ctx, "active")
	require.NoError(t, err)
	assert.Empty(t, active)

	done, err := s.ListWorkspacesByStatus(ctx, "done")
	require.NoError(t, err)
	require.Len(t, done, 1)
	assert.Equal(t, id, done[0].ID)
}

func TestForeignKey_Enforced(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	// A workspace referencing a non-existent agent must be rejected, proving
	// foreign_keys is on for this connection.
	_, err := s.CreateWorkspace(ctx, CreateWorkspaceParams{
		ID: "ws-x", AgentID: "ghost", Branch: "b", Path: "/p",
	})
	assert.Error(t, err)
}

func TestEnqueuePR_DedupesByBranch(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	wsID := seedWorkspace(t, s)

	first, err := s.EnqueuePR(ctx, EnqueuePRParams{
		ID: "pr-1", WorkspaceID: wsID, Branch: "feature/x", Title: "add x", Priority: 5,
	})
	require.NoError(t, err)
	assert.Equal(t, "pr-1", first.ID)

	// Same branch again -> no-op returning the existing row, not a new one.
	dup, err := s.EnqueuePR(ctx, EnqueuePRParams{
		ID: "pr-2", WorkspaceID: wsID, Branch: "feature/x", Title: "add x again", Priority: 9,
	})
	require.NoError(t, err)
	assert.Equal(t, "pr-1", dup.ID, "duplicate branch should return original row")
	assert.Equal(t, "add x", dup.Title, "original row is left unchanged")

	queued, err := s.ListPRsByStatus(ctx, "queued")
	require.NoError(t, err)
	assert.Len(t, queued, 1)
}

func TestListPRsByStatus_OrdersByPriorityThenAge(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	wsID := seedWorkspace(t, s)

	// Insert low priority first, then high; high must come out first.
	_, err := s.EnqueuePR(ctx, EnqueuePRParams{ID: "low", WorkspaceID: wsID, Branch: "a", Title: "a", Priority: 1})
	require.NoError(t, err)
	_, err = s.EnqueuePR(ctx, EnqueuePRParams{ID: "high", WorkspaceID: wsID, Branch: "b", Title: "b", Priority: 10})
	require.NoError(t, err)

	queued, err := s.ListPRsByStatus(ctx, "queued")
	require.NoError(t, err)
	require.Len(t, queued, 2)
	assert.Equal(t, "high", queued[0].ID)
	assert.Equal(t, "low", queued[1].ID)
}

func TestRecordPRFailure_IncrementsAttempts(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	wsID := seedWorkspace(t, s)
	_, err := s.EnqueuePR(ctx, EnqueuePRParams{ID: "pr", WorkspaceID: wsID, Branch: "b", Title: "t"})
	require.NoError(t, err)

	msg := "github 503"
	require.NoError(t, s.RecordPRFailure(ctx, RecordPRFailureParams{LastError: &msg, ID: "pr"}))

	pr, err := s.GetPR(ctx, "pr")
	require.NoError(t, err)
	assert.Equal(t, "failed", pr.Status)
	assert.Equal(t, int64(1), pr.Attempts)
	require.NotNil(t, pr.LastError)
	assert.Equal(t, msg, *pr.LastError)
}

func TestTx_RollsBackOnError(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	_, err := s.RegisterAgent(ctx, RegisterAgentParams{ID: "a", Name: "n"})
	require.NoError(t, err)

	sentinel := assert.AnError
	err = s.Tx(ctx, func(q *Queries) error {
		_, err := q.CreateWorkspace(ctx, CreateWorkspaceParams{
			ID: "ws", AgentID: "a", Branch: "b", Path: "/p",
		})
		require.NoError(t, err)
		return sentinel // force rollback
	})
	assert.ErrorIs(t, err, sentinel)

	// The workspace created inside the rolled-back tx must not persist.
	all, err := s.ListWorkspaces(ctx)
	require.NoError(t, err)
	assert.Empty(t, all)
}

func TestCascadeDelete_WorkspaceRemovesQueuedPR(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	wsID := seedWorkspace(t, s)
	_, err := s.EnqueuePR(ctx, EnqueuePRParams{ID: "pr", WorkspaceID: wsID, Branch: "b", Title: "t"})
	require.NoError(t, err)

	require.NoError(t, s.DeleteWorkspace(ctx, wsID))

	_, err = s.GetPR(ctx, "pr")
	assert.Error(t, err, "queued PR should be cascade-deleted with its workspace")
}
