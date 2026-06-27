package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AyushPramanik/mesh/internal/git"
	"github.com/AyushPramanik/mesh/internal/store"
)

// setup wires a real on-disk git repo (with one commit, ready for worktrees) to
// a real in-memory store, and registers one agent. It returns the manager and
// the repo so tests can assert against actual worktrees on disk.
func setup(t *testing.T) (*Manager, *git.Repo, string) {
	t.Helper()
	ctx := context.Background()

	dir := filepath.Join(t.TempDir(), "repo")
	repo, err := git.Init(dir)
	require.NoError(t, err)
	_, err = repo.Commit("initial", map[string][]byte{"README.md": []byte("# mesh\n")})
	require.NoError(t, err)

	st, err := store.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	_, err = st.RegisterAgent(ctx, store.RegisterAgentParams{ID: "agent-1", Name: "claude"})
	require.NoError(t, err)

	return NewManager(repo, st), repo, "agent-1"
}

func TestCreate_MaterialisesWorktreeAndRow(t *testing.T) {
	m, repo, agent := setup(t)
	ctx := context.Background()

	ws, err := m.Create(ctx, agent)
	require.NoError(t, err)
	assert.Equal(t, agent, ws.AgentID)
	assert.Equal(t, StatusActive, ws.Status)
	assert.Contains(t, ws.Branch, "mesh/agent-1/")

	// The worktree exists on disk and shares the object store (README present).
	_, err = os.Stat(filepath.Join(ws.Path, "README.md"))
	require.NoError(t, err)

	// The branch is visible in the shared repository.
	branches, err := repo.Branches()
	require.NoError(t, err)
	assert.Contains(t, branches, ws.Branch)

	// And the row round-trips.
	got, err := m.Get(ctx, ws.ID)
	require.NoError(t, err)
	assert.Equal(t, ws.ID, got.ID)
}

func TestCreate_UnknownAgentRollsBackWorktree(t *testing.T) {
	m, repo, _ := setup(t)
	ctx := context.Background()

	// "ghost" is not registered; the store's foreign key rejects the row, and
	// the manager must clean up the worktree it already created.
	_, err := m.Create(ctx, "ghost")
	require.Error(t, err)

	list, err := repo.ListWorktrees(ctx)
	require.NoError(t, err)
	assert.Empty(t, list, "worktree must be rolled back when the row insert fails")
}

func TestCommit_CommitsWorktreeChanges(t *testing.T) {
	m, _, agent := setup(t)
	ctx := context.Background()

	ws, err := m.Create(ctx, agent)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(ws.Path, "agent-output.txt"), []byte("done"), 0o644))
	hash, err := m.Commit(ctx, ws.ID, "agent work")
	require.NoError(t, err)
	assert.NotEmpty(t, hash)
}

func TestFinish_ReclaimsWorktreeKeepsRow(t *testing.T) {
	m, repo, agent := setup(t)
	ctx := context.Background()

	ws, err := m.Create(ctx, agent)
	require.NoError(t, err)

	require.NoError(t, m.Finish(ctx, ws.ID, StatusDone))

	// Worktree gone from disk.
	_, err = os.Stat(ws.Path)
	assert.True(t, os.IsNotExist(err))
	list, err := repo.ListWorktrees(ctx)
	require.NoError(t, err)
	assert.Empty(t, list)

	// Row kept, status updated.
	got, err := m.Get(ctx, ws.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusDone, got.Status)
}

func TestFinish_RejectsNonTerminalStatus(t *testing.T) {
	m, _, agent := setup(t)
	ctx := context.Background()
	ws, err := m.Create(ctx, agent)
	require.NoError(t, err)

	assert.Error(t, m.Finish(ctx, ws.ID, StatusActive))
}

func TestGC_ReclaimsOrphanWorktrees(t *testing.T) {
	m, repo, agent := setup(t)
	ctx := context.Background()

	keep, err := m.Create(ctx, agent)
	require.NoError(t, err)

	// Simulate a crash: a worktree exists on disk with no active workspace row.
	orphan, err := repo.CreateWorktree(ctx, "orphan", "mesh/crashed")
	require.NoError(t, err)

	reclaimed, err := m.GC(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"orphan"}, reclaimed)

	// The orphan is gone; the live workspace's worktree survives.
	_, err = os.Stat(orphan.Path)
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(keep.Path)
	assert.NoError(t, err)
}
