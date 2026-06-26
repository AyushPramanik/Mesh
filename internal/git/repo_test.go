package git

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitMemory_CommitAndHead(t *testing.T) {
	repo, err := InitMemory()
	require.NoError(t, err)

	hash, err := repo.Commit("initial", map[string][]byte{
		"README.md": []byte("# mesh\n"),
	})
	require.NoError(t, err)
	assert.False(t, hash.IsZero())

	head, err := repo.Head()
	require.NoError(t, err)
	assert.Equal(t, hash, head)
}

func TestInit_OnDisk(t *testing.T) {
	dir := t.TempDir()
	repo, err := Init(dir)
	require.NoError(t, err)
	assert.Equal(t, dir, repo.Dir())

	_, err = repo.Commit("initial", map[string][]byte{"a.txt": []byte("a")})
	require.NoError(t, err)

	reopened, err := Open(dir)
	require.NoError(t, err)
	head, err := reopened.Head()
	require.NoError(t, err)
	assert.False(t, head.IsZero())
}

func TestCreateBranch_AndList(t *testing.T) {
	repo, err := InitMemory()
	require.NoError(t, err)
	_, err = repo.Commit("initial", map[string][]byte{"a.txt": []byte("a")})
	require.NoError(t, err)

	require.NoError(t, repo.CreateBranch("feature/x"))

	branches, err := repo.Branches()
	require.NoError(t, err)
	assert.Contains(t, branches, "feature/x")
}

func TestInMemory_NoWorktrees(t *testing.T) {
	repo, err := InitMemory()
	require.NoError(t, err)
	_, err = repo.CreateWorktree(t.Context(), "w1", "b1")
	assert.Error(t, err)
}
