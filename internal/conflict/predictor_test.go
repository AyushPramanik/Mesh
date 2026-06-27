package conflict

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AyushPramanik/mesh/internal/store"
)

// setup opens an in-memory store and seeds one agent plus the named workspaces,
// returning a predictor wired to the real store.
func setup(t *testing.T, workspaceIDs ...string) (*Predictor, *store.Store) {
	t.Helper()
	ctx := context.Background()

	st, err := store.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	_, err = st.RegisterAgent(ctx, store.RegisterAgentParams{ID: "agent", Name: "claude"})
	require.NoError(t, err)
	for _, id := range workspaceIDs {
		_, err := st.CreateWorkspace(ctx, store.CreateWorkspaceParams{
			ID: id, AgentID: "agent", Branch: "b/" + id, Path: "/tmp/" + id,
		})
		require.NoError(t, err)
	}
	return New(st), st
}

func TestCheck_NoOverlapIsClear(t *testing.T) {
	p, _ := setup(t, "ws1", "ws2")
	ctx := context.Background()

	_, _, err := p.Register(ctx, "ws1", []string{"pkg/a.go"})
	require.NoError(t, err)

	d, err := p.Check(ctx, "ws2", []string{"pkg/b.go", "pkg/c.go"})
	require.NoError(t, err)
	assert.True(t, d.Clear())
	assert.Empty(t, d.Conflicts)
}

func TestCheck_EmptyFilesIsClear(t *testing.T) {
	p, _ := setup(t, "ws1")
	d, err := p.Check(context.Background(), "ws1", nil)
	require.NoError(t, err)
	assert.True(t, d.Clear())
}

func TestRegister_OverlapWarnsWithOffendingPaths(t *testing.T) {
	p, _ := setup(t, "ws1", "ws2")
	ctx := context.Background()

	_, _, err := p.Register(ctx, "ws1", []string{"pkg/a.go", "pkg/b.go"})
	require.NoError(t, err)

	_, d, err := p.Register(ctx, "ws2", []string{"pkg/b.go", "pkg/c.go"})
	require.NoError(t, err)
	require.Equal(t, VerdictWarn, d.Verdict)
	require.Len(t, d.Conflicts, 1)
	assert.Equal(t, "ws1", d.Conflicts[0].WorkspaceID)
	assert.Equal(t, []string{"pkg/b.go"}, d.Conflicts[0].Paths)
}

func TestRegister_RecordsEvenWhenWarned(t *testing.T) {
	p, _ := setup(t, "ws1", "ws2", "ws3")
	ctx := context.Background()

	_, _, err := p.Register(ctx, "ws1", []string{"pkg/a.go"})
	require.NoError(t, err)
	// ws2 overlaps ws1 but is still recorded (intents are not locks).
	_, d, err := p.Register(ctx, "ws2", []string{"pkg/a.go"})
	require.NoError(t, err)
	assert.Equal(t, VerdictWarn, d.Verdict)

	// ws3 now sees both ws1 and ws2 claiming the file.
	d3, err := p.Check(ctx, "ws3", []string{"pkg/a.go"})
	require.NoError(t, err)
	require.Equal(t, VerdictWarn, d3.Verdict)
	assert.Len(t, d3.Conflicts, 2)
}

func TestCheck_NoSelfConflict(t *testing.T) {
	p, _ := setup(t, "ws1")
	ctx := context.Background()

	_, _, err := p.Register(ctx, "ws1", []string{"pkg/a.go"})
	require.NoError(t, err)

	// A workspace never conflicts with its own active intent.
	d, err := p.Check(ctx, "ws1", []string{"pkg/a.go"})
	require.NoError(t, err)
	assert.True(t, d.Clear())
}

func TestRelease_ClearsFutureChecks(t *testing.T) {
	p, _ := setup(t, "ws1", "ws2")
	ctx := context.Background()

	_, _, err := p.Register(ctx, "ws1", []string{"pkg/a.go"})
	require.NoError(t, err)
	require.NoError(t, p.Release(ctx, "ws1"))

	d, err := p.Check(ctx, "ws2", []string{"pkg/a.go"})
	require.NoError(t, err)
	assert.True(t, d.Clear(), "released intents must not count against new checks")
}

func TestCheck_NormalizesPathVariants(t *testing.T) {
	p, _ := setup(t, "ws1", "ws2")
	ctx := context.Background()

	_, _, err := p.Register(ctx, "ws1", []string{"./pkg/a.go"})
	require.NoError(t, err)

	// Spelled differently but the same file: must still warn.
	d, err := p.Check(ctx, "ws2", []string{"pkg/sub/../a.go"})
	require.NoError(t, err)
	assert.Equal(t, VerdictWarn, d.Verdict)
}
