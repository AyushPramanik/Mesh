package git

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newRepoWithCommit creates an on-disk repo with one commit, ready to host
// worktrees.
func newRepoWithCommit(t *testing.T) *Repo {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "repo")
	repo, err := Init(dir)
	require.NoError(t, err)
	_, err = repo.Commit("initial", map[string][]byte{"README.md": []byte("# mesh\n")})
	require.NoError(t, err)
	return repo
}

func TestCreateWorktree_RoundTrip(t *testing.T) {
	repo := newRepoWithCommit(t)
	ctx := t.Context()

	wt, err := repo.CreateWorktree(ctx, "agent-1", "feature/agent-1")
	require.NoError(t, err)
	assert.Equal(t, "agent-1", wt.Name)
	assert.Equal(t, "feature/agent-1", wt.Branch)

	// The working directory exists and shares the parent object store: the
	// committed file is materialised in the worktree.
	info, err := os.Stat(wt.Path)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
	_, err = os.Stat(filepath.Join(wt.Path, "README.md"))
	require.NoError(t, err)

	// The new branch is visible in the shared repository.
	branches, err := repo.Branches()
	require.NoError(t, err)
	assert.Contains(t, branches, "feature/agent-1")

	list, err := repo.ListWorktrees(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "agent-1", list[0].Name)
	assert.Equal(t, "feature/agent-1", list[0].Branch)
}

func TestCreateWorktree_DuplicateRejected(t *testing.T) {
	repo := newRepoWithCommit(t)
	ctx := t.Context()

	_, err := repo.CreateWorktree(ctx, "dup", "b1")
	require.NoError(t, err)
	_, err = repo.CreateWorktree(ctx, "dup", "b2")
	assert.Error(t, err)
}

func TestCreateWorktree_InvalidName(t *testing.T) {
	repo := newRepoWithCommit(t)
	for _, name := range []string{"", "a/b", "..", "."} {
		_, err := repo.CreateWorktree(t.Context(), name, "b")
		assert.Error(t, err, "name %q should be rejected", name)
	}
}

func TestRemoveWorktree(t *testing.T) {
	repo := newRepoWithCommit(t)
	ctx := t.Context()

	wt, err := repo.CreateWorktree(ctx, "temp", "feature/temp")
	require.NoError(t, err)

	require.NoError(t, repo.RemoveWorktree(ctx, "temp"))

	_, err = os.Stat(wt.Path)
	assert.True(t, os.IsNotExist(err), "worktree dir should be gone")

	list, err := repo.ListWorktrees(ctx)
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestListWorktrees_ExcludesMainTree(t *testing.T) {
	repo := newRepoWithCommit(t)
	list, err := repo.ListWorktrees(t.Context())
	require.NoError(t, err)
	assert.Empty(t, list, "main working tree must not be reported as a worktree")
}
