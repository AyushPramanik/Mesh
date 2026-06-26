package queue

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AyushPramanik/mesh/internal/store"
)

// mapFiles is a FileSource backed by an in-memory branch->files map.
type mapFiles map[string][]string

func (m mapFiles) ChangedFiles(_ context.Context, branch string) ([]string, error) {
	return m[branch], nil
}

// schedSetup returns a scheduler over an in-memory queue plus the store, so a
// test can enqueue PRs and a fake file source.
func schedSetup(t *testing.T, files mapFiles) (*Scheduler, *Queue) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { st.Close() })
	_, err = st.RegisterAgent(ctx, store.RegisterAgentParams{ID: "a", Name: "n"})
	require.NoError(t, err)
	_, err = st.CreateWorkspace(ctx, store.CreateWorkspaceParams{ID: "ws", AgentID: "a", Branch: "b", Path: "/p"})
	require.NoError(t, err)

	q := New(st, &fakeSubmitter{})
	return NewScheduler(q, files), q
}

func enqueueBranch(t *testing.T, q *Queue, branch string, priority int) {
	t.Helper()
	_, err := q.Submit(context.Background(), SubmitParams{
		WorkspaceID: "ws", Branch: branch, Title: branch, Priority: priority,
	})
	require.NoError(t, err)
}

func TestPlan_DisjointPRsShareOneTrain(t *testing.T) {
	files := mapFiles{
		"a": {"pkg/a.go"},
		"b": {"pkg/b.go"},
	}
	s, q := schedSetup(t, files)
	enqueueBranch(t, q, "a", 0)
	enqueueBranch(t, q, "b", 0)

	trains, err := s.Plan(context.Background())
	require.NoError(t, err)
	require.Len(t, trains, 1, "disjoint PRs land together")
	assert.Len(t, trains[0].PRs, 2)
}

func TestPlan_OverlappingPRsSplitIntoTrains(t *testing.T) {
	files := mapFiles{
		"a": {"pkg/shared.go"},
		"b": {"pkg/shared.go"},
	}
	s, q := schedSetup(t, files)
	enqueueBranch(t, q, "a", 0)
	enqueueBranch(t, q, "b", 0)

	trains, err := s.Plan(context.Background())
	require.NoError(t, err)
	require.Len(t, trains, 2, "conflicting PRs cannot share a train")
}

func TestPlan_PacksByPriorityThenDisjointness(t *testing.T) {
	// high and low share a file; mid is disjoint from both. Scan order is
	// high, mid, low. high opens train 1; mid is disjoint so joins train 1;
	// low conflicts with high so starts train 2.
	files := mapFiles{
		"high": {"pkg/x.go"},
		"mid":  {"pkg/y.go"},
		"low":  {"pkg/x.go"},
	}
	s, q := schedSetup(t, files)
	enqueueBranch(t, q, "high", 10)
	enqueueBranch(t, q, "mid", 5)
	enqueueBranch(t, q, "low", 1)

	trains, err := s.Plan(context.Background())
	require.NoError(t, err)
	require.Len(t, trains, 2)
	assert.Equal(t, []string{"high", "mid"}, branches(trains[0]))
	assert.Equal(t, []string{"low"}, branches(trains[1]))
}

func TestNextTrain_ReturnsHighestPriorityBatch(t *testing.T) {
	files := mapFiles{"high": {"a.go"}, "low": {"a.go"}}
	s, q := schedSetup(t, files)
	enqueueBranch(t, q, "low", 1)
	enqueueBranch(t, q, "high", 9)

	next, err := s.NextTrain(context.Background())
	require.NoError(t, err)
	require.Len(t, next.PRs, 1)
	assert.Equal(t, "high", next.PRs[0].Branch)
}

func TestNextTrain_EmptyQueue(t *testing.T) {
	s, _ := schedSetup(t, mapFiles{})
	next, err := s.NextTrain(context.Background())
	require.NoError(t, err)
	assert.Empty(t, next.PRs)
}

func branches(t Train) []string {
	out := make([]string, len(t.PRs))
	for i, pr := range t.PRs {
		out[i] = pr.Branch
	}
	return out
}
