package queue

import (
	"context"
	"fmt"
	"path"
)

// A Train is an ordered set of PRs that can land together without conflicting:
// their changed-file sets are pairwise disjoint. Trains are submitted to the
// host's native merge queue where available, or serialised by Mesh otherwise
// (CLAUDE.md "Merge train"). This layer plans trains; landing them is a
// follow-up.
type Train struct {
	PRs []PR
	// files is the union of every member PR's footprint, used to test whether a
	// candidate PR can join.
	files map[string]struct{}
}

// conflictsWith reports whether pr's files intersect the train's footprint.
func (t *Train) conflictsWith(files []string) bool {
	for _, f := range files {
		if _, ok := t.files[f]; ok {
			return true
		}
	}
	return false
}

func (t *Train) add(pr PR, files []string) {
	t.PRs = append(t.PRs, pr)
	for _, f := range files {
		t.files[f] = struct{}{}
	}
}

// FileSource reports the changed-file footprint of a PR's branch. It is
// declared here, at the point of use; the daemon backs it with the git repo
// (diff against the base branch). Declared as an interface so the scheduler is
// testable without a real repository.
type FileSource interface {
	ChangedFiles(ctx context.Context, branch string) ([]string, error)
}

// Scheduler plans merge trains from the queued PRs.
type Scheduler struct {
	queue *Queue
	files FileSource
}

// NewScheduler builds a Scheduler over a queue and a file source.
func NewScheduler(q *Queue, files FileSource) *Scheduler {
	return &Scheduler{queue: q, files: files}
}

// Plan partitions the currently queued PRs into merge trains. PRs are
// considered in queue scan order (priority then age); each is placed in the
// first existing train it does not conflict with, otherwise it starts a new
// train. The first returned train is therefore the highest-priority batch that
// can land together now.
//
// This is the file-level conflict walk. When the AST dependency graph lands,
// the overlap test gains symbol-level edges; the train-packing logic is
// unchanged.
func (s *Scheduler) Plan(ctx context.Context) ([]Train, error) {
	prs, err := s.queue.List(ctx, StatusQueued)
	if err != nil {
		return nil, fmt.Errorf("queue.Plan: %w", err)
	}

	var trains []Train
	for _, pr := range prs {
		files, err := s.files.ChangedFiles(ctx, pr.Branch)
		if err != nil {
			return nil, fmt.Errorf("queue.Plan: footprint of %s: %w", pr.Branch, err)
		}
		files = normalizePaths(files)

		placed := false
		for i := range trains {
			if !trains[i].conflictsWith(files) {
				trains[i].add(pr, files)
				placed = true
				break
			}
		}
		if !placed {
			t := Train{files: make(map[string]struct{})}
			t.add(pr, files)
			trains = append(trains, t)
		}
	}
	return trains, nil
}

// NextTrain returns the first planned train: the batch to submit now. It is
// empty when the queue has no queued PRs.
func (s *Scheduler) NextTrain(ctx context.Context) (Train, error) {
	trains, err := s.Plan(ctx)
	if err != nil {
		return Train{}, err
	}
	if len(trains) == 0 {
		return Train{files: map[string]struct{}{}}, nil
	}
	return trains[0], nil
}

// normalizePaths cleans paths so overlap comparison is stable regardless of
// spelling, matching the conflict predictor's normalisation.
func normalizePaths(files []string) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		if f != "" {
			out = append(out, path.Clean(f))
		}
	}
	return out
}
