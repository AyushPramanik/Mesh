package queue

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AyushPramanik/mesh/internal/store"
)

// fakeSubmitter records calls and returns whatever respond yields.
type fakeSubmitter struct {
	calls   int
	respond func(pr PR) error
}

func (f *fakeSubmitter) Submit(_ context.Context, pr PR) error {
	f.calls++
	if f.respond != nil {
		return f.respond(pr)
	}
	return nil
}

// setup opens an in-memory store with one agent and workspace, returning the
// store so tests can inspect rows directly.
func setup(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { st.Close() })

	_, err = st.RegisterAgent(ctx, store.RegisterAgentParams{ID: "a", Name: "claude"})
	require.NoError(t, err)
	_, err = st.CreateWorkspace(ctx, store.CreateWorkspaceParams{
		ID: "ws", AgentID: "a", Branch: "b", Path: "/p",
	})
	require.NoError(t, err)
	return st
}

func enqueue(t *testing.T, q *Queue, branch string, priority int) PR {
	t.Helper()
	pr, err := q.Submit(context.Background(), SubmitParams{
		WorkspaceID: "ws", Branch: branch, Title: "t", Priority: priority,
	})
	require.NoError(t, err)
	return pr
}

func TestSubmit_DedupesByBranch(t *testing.T) {
	st := setup(t)
	q := New(st, &fakeSubmitter{})

	first := enqueue(t, q, "feature/x", 0)
	dup := enqueue(t, q, "feature/x", 0)
	assert.Equal(t, first.ID, dup.ID)

	queued, err := q.List(context.Background(), StatusQueued)
	require.NoError(t, err)
	assert.Len(t, queued, 1)
}

func TestProcessOnce_SuccessMarksSubmitted(t *testing.T) {
	st := setup(t)
	sub := &fakeSubmitter{}
	q := New(st, sub)
	enqueue(t, q, "feature/x", 0)

	n, err := q.ProcessOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, 1, sub.calls)

	submitted, err := q.List(context.Background(), StatusSubmitted)
	require.NoError(t, err)
	require.Len(t, submitted, 1)
	queued, err := q.List(context.Background(), StatusQueued)
	require.NoError(t, err)
	assert.Empty(t, queued)
}

func TestProcessOnce_PermanentFailureFails(t *testing.T) {
	st := setup(t)
	sub := &fakeSubmitter{respond: func(PR) error { return errors.New("422 invalid") }}
	q := New(st, sub)
	pr := enqueue(t, q, "feature/x", 0)

	_, err := q.ProcessOnce(context.Background())
	require.NoError(t, err)

	row, err := st.GetPR(context.Background(), pr.ID)
	require.NoError(t, err)
	assert.Equal(t, "failed", row.Status)
	assert.Equal(t, int64(1), row.Attempts)
	require.NotNil(t, row.LastError)
	assert.Contains(t, *row.LastError, "422")
}

func TestProcessOnce_TransientRequeuesAndDefers(t *testing.T) {
	st := setup(t)
	sub := &fakeSubmitter{respond: func(PR) error { return Transient(errors.New("503")) }}
	q := New(st, sub) // default 30s backoff
	pr := enqueue(t, q, "feature/x", 0)

	_, err := q.ProcessOnce(context.Background())
	require.NoError(t, err)

	row, err := st.GetPR(context.Background(), pr.ID)
	require.NoError(t, err)
	assert.Equal(t, "queued", row.Status, "transient failure stays queued for retry")
	assert.Equal(t, int64(1), row.Attempts)
	require.NotNil(t, row.NextRetryAt, "retry must be deferred")

	// Deferred PRs are not yet due, so a second pass does nothing.
	due, err := st.ListDuePRs(context.Background())
	require.NoError(t, err)
	assert.Empty(t, due)
}

func TestProcessOnce_TransientExhaustionFails(t *testing.T) {
	st := setup(t)
	sub := &fakeSubmitter{respond: func(PR) error { return Transient(errors.New("503")) }}
	// Zero backoff so retries are immediately due; budget of 3 attempts.
	q := New(st, sub, WithMaxAttempts(3), WithBaseBackoff(0))
	pr := enqueue(t, q, "feature/x", 0)

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		_, err := q.ProcessOnce(ctx)
		require.NoError(t, err)
	}

	row, err := st.GetPR(ctx, pr.ID)
	require.NoError(t, err)
	assert.Equal(t, "failed", row.Status, "should fail after exhausting retries")
	assert.Equal(t, int64(3), row.Attempts)
	assert.Equal(t, 3, sub.calls)
}

func TestProcessOnce_OrdersByPriorityThenAge(t *testing.T) {
	st := setup(t)
	var order []string
	sub := &fakeSubmitter{respond: func(pr PR) error { order = append(order, pr.Branch); return nil }}
	q := New(st, sub)

	enqueue(t, q, "low", 1)
	enqueue(t, q, "high", 10)
	enqueue(t, q, "mid", 5)

	_, err := q.ProcessOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"high", "mid", "low"}, order)
}

func TestBackoffModifier_Doubles(t *testing.T) {
	q := New(nil, nil, WithBaseBackoff(30*time.Second))
	assert.Equal(t, "+30 seconds", q.backoffModifier(1))
	assert.Equal(t, "+60 seconds", q.backoffModifier(2))
	assert.Equal(t, "+120 seconds", q.backoffModifier(3))
}
