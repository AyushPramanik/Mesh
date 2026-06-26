// Package queue is the PR queue: agents submit pull requests here instead of
// calling the git host directly. The queue deduplicates by branch, orders by
// priority then submission time, and retries transient failures with
// exponential backoff. It is persisted in the store, so in-flight work survives
// a daemon restart (CLAUDE.md "PR queue").
//
// This layer lands the FIFO queue with single-PR submission. The merge-train
// scheduler that batches non-conflicting PRs builds on it in a later layer.
package queue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/AyushPramanik/mesh/internal/store"
)

// Status is the lifecycle state of a queued PR.
type Status string

const (
	StatusQueued    Status = "queued"
	StatusSubmitted Status = "submitted"
	StatusMerged    Status = "merged"
	StatusFailed    Status = "failed"
)

// PR is the domain view of a queued pull request.
type PR struct {
	ID          string
	WorkspaceID string
	Branch      string
	Title       string
	Priority    int
	Status      Status
	Attempts    int
}

// Submitter opens (or updates) a pull request on the git host. It is declared
// here, at the point of use; internal/github provides the production
// implementation. Returning an error wrapped with Transient asks the queue to
// retry with backoff; any other error fails the PR permanently.
type Submitter interface {
	Submit(ctx context.Context, pr PR) error
}

// prStore is the slice of the store the queue needs. *store.Store satisfies it.
type prStore interface {
	EnqueuePR(ctx context.Context, arg store.EnqueuePRParams) (store.PrQueue, error)
	GetPR(ctx context.Context, id string) (store.PrQueue, error)
	ListPRsByStatus(ctx context.Context, status string) ([]store.PrQueue, error)
	ListDuePRs(ctx context.Context) ([]store.PrQueue, error)
	MarkPRSubmitted(ctx context.Context, id string) error
	RequeuePR(ctx context.Context, arg store.RequeuePRParams) error
	RecordPRFailure(ctx context.Context, arg store.RecordPRFailureParams) error
}

// Queue persists and processes PR submissions.
type Queue struct {
	store       prStore
	submitter   Submitter
	maxAttempts int
	baseBackoff time.Duration
}

// Option configures a Queue.
type Option func(*Queue)

// WithMaxAttempts sets how many times a transient failure is retried before the
// PR is failed permanently. Default 5.
func WithMaxAttempts(n int) Option { return func(q *Queue) { q.maxAttempts = n } }

// WithBaseBackoff sets the base delay for exponential backoff. Default 30s.
func WithBaseBackoff(d time.Duration) Option { return func(q *Queue) { q.baseBackoff = d } }

// New constructs a Queue over the store and submitter.
func New(st prStore, submitter Submitter, opts ...Option) *Queue {
	q := &Queue{
		store:       st,
		submitter:   submitter,
		maxAttempts: 5,
		baseBackoff: 30 * time.Second,
	}
	for _, opt := range opts {
		opt(q)
	}
	return q
}

// SubmitParams describes a PR to enqueue.
type SubmitParams struct {
	WorkspaceID string
	Branch      string
	Title       string
	Priority    int
}

// Submit enqueues a PR. Submitting a branch already in the queue is a no-op that
// returns the existing entry (dedupe by branch).
func (q *Queue) Submit(ctx context.Context, p SubmitParams) (PR, error) {
	id, err := newID()
	if err != nil {
		return PR{}, fmt.Errorf("queue.Submit: %w", err)
	}
	row, err := q.store.EnqueuePR(ctx, store.EnqueuePRParams{
		ID:          id,
		WorkspaceID: p.WorkspaceID,
		Branch:      p.Branch,
		Title:       p.Title,
		Priority:    int64(p.Priority),
	})
	if err != nil {
		return PR{}, fmt.Errorf("queue.Submit: %w", err)
	}
	return fromRow(row), nil
}

// List returns every PR currently in the given status, in scan order.
func (q *Queue) List(ctx context.Context, status Status) ([]PR, error) {
	rows, err := q.store.ListPRsByStatus(ctx, string(status))
	if err != nil {
		return nil, fmt.Errorf("queue.List: %w", err)
	}
	out := make([]PR, len(rows))
	for i, r := range rows {
		out[i] = fromRow(r)
	}
	return out, nil
}

// ProcessOnce submits every PR that is due now, in priority-then-age order, and
// returns how many it attempted. Each PR is submitted via the Submitter:
//   - success           -> submitted
//   - transient failure  -> requeued with exponential backoff, until attempts
//     are exhausted, after which it fails permanently
//   - permanent failure  -> failed
func (q *Queue) ProcessOnce(ctx context.Context) (int, error) {
	due, err := q.store.ListDuePRs(ctx)
	if err != nil {
		return 0, fmt.Errorf("queue.ProcessOnce: %w", err)
	}
	for _, row := range due {
		if err := q.processOne(ctx, row); err != nil {
			return 0, err
		}
	}
	return len(due), nil
}

func (q *Queue) processOne(ctx context.Context, row store.PrQueue) error {
	pr := fromRow(row)
	err := q.submitter.Submit(ctx, pr)
	if err == nil {
		if err := q.store.MarkPRSubmitted(ctx, pr.ID); err != nil {
			return fmt.Errorf("queue.ProcessOnce: mark submitted: %w", err)
		}
		return nil
	}

	msg := err.Error()
	attempts := pr.Attempts + 1

	// Transient failures are retried until the attempt budget is spent.
	if IsTransient(err) && attempts < q.maxAttempts {
		if rqErr := q.store.RequeuePR(ctx, store.RequeuePRParams{
			LastError: &msg,
			Backoff:   q.backoffModifier(attempts),
			ID:        pr.ID,
		}); rqErr != nil {
			return fmt.Errorf("queue.ProcessOnce: requeue: %w", rqErr)
		}
		return nil
	}

	// Permanent failure, or transient retries exhausted.
	if fErr := q.store.RecordPRFailure(ctx, store.RecordPRFailureParams{
		LastError: &msg,
		ID:        pr.ID,
	}); fErr != nil {
		return fmt.Errorf("queue.ProcessOnce: record failure: %w", fErr)
	}
	return nil
}

// Run processes the queue every interval until ctx is cancelled. It returns
// ctx.Err() on shutdown.
func (q *Queue) Run(ctx context.Context, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := q.ProcessOnce(ctx); err != nil {
				return err
			}
		}
	}
}

// backoffModifier returns a SQLite datetime modifier (e.g. "+120 seconds") for
// the given attempt number, doubling the base delay each attempt.
func (q *Queue) backoffModifier(attempt int) string {
	delay := q.baseBackoff << (attempt - 1) // base * 2^(attempt-1)
	return fmt.Sprintf("+%d seconds", int(delay.Seconds()))
}

// fromRow maps a store row to the domain type.
func fromRow(r store.PrQueue) PR {
	return PR{
		ID:          r.ID,
		WorkspaceID: r.WorkspaceID,
		Branch:      r.Branch,
		Title:       r.Title,
		Priority:    int(r.Priority),
		Status:      Status(r.Status),
		Attempts:    int(r.Attempts),
	}
}

func newID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// transient wraps an error the queue should retry.
type transient struct{ err error }

func (t *transient) Error() string { return t.err.Error() }
func (t *transient) Unwrap() error { return t.err }

// Transient marks err as a retryable failure. A Submitter returns this for
// rate-limit or server errors; anything else fails the PR permanently.
func Transient(err error) error {
	if err == nil {
		return nil
	}
	return &transient{err: err}
}

// IsTransient reports whether err was marked retryable with Transient.
func IsTransient(err error) bool {
	var t *transient
	return errors.As(err, &t)
}
