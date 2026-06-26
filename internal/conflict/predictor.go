// Package conflict predicts whether concurrent agent work will collide, before
// the work starts. This is the first cut: file-level overlap between registered
// intents. AST-level dependency and structural conflicts (a symbol one branch
// changes that another imports or calls) build on this foundation in a later
// layer — see CLAUDE.md "Conflict graph".
//
// Intents are best-effort predictions, not locks: a Warn does not block an
// agent, and actual conflicts are still caught downstream at diff time.
package conflict

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path"
	"sort"

	"github.com/AyushPramanik/mesh/internal/store"
)

// Verdict is the outcome of checking an intent against active intents.
type Verdict string

const (
	// VerdictClear means no other active intent claims any of the same files.
	VerdictClear Verdict = "clear"
	// VerdictWarn means at least one file overlaps another active intent.
	VerdictWarn Verdict = "warn"
)

// Conflict is an overlap with one other workspace's active intent.
type Conflict struct {
	WorkspaceID string
	// Paths are the overlapping files, normalised and sorted.
	Paths []string
}

// Decision is the result of a Check or Register.
type Decision struct {
	Verdict   Verdict
	Conflicts []Conflict
}

// Clear reports whether the decision found no overlaps.
func (d Decision) Clear() bool { return d.Verdict == VerdictClear }

// Intent is a registered, persisted declaration of files a workspace will touch.
type Intent struct {
	ID          string
	WorkspaceID string
	Files       []string
}

// intentStore is the slice of the store the predictor needs, declared at the
// point of use. *store.Store satisfies it.
type intentStore interface {
	CreateIntent(ctx context.Context, arg store.CreateIntentParams) (store.Intent, error)
	AddIntentFile(ctx context.Context, arg store.AddIntentFileParams) error
	FindOverlappingIntents(ctx context.Context, arg store.FindOverlappingIntentsParams) ([]store.FindOverlappingIntentsRow, error)
	ReleaseWorkspaceIntents(ctx context.Context, workspaceID string) error
}

// Predictor checks and registers intents.
type Predictor struct {
	store intentStore
}

// New constructs a Predictor over the given store.
func New(st intentStore) *Predictor {
	return &Predictor{store: st}
}

// Check reports, without recording anything, whether the given files overlap
// any active intent owned by another workspace. An empty file set is trivially
// clear.
func (p *Predictor) Check(ctx context.Context, workspaceID string, files []string) (Decision, error) {
	files = normalize(files)
	if len(files) == 0 {
		return Decision{Verdict: VerdictClear}, nil
	}

	rows, err := p.store.FindOverlappingIntents(ctx, store.FindOverlappingIntentsParams{
		WorkspaceID: workspaceID,
		Paths:       files,
	})
	if err != nil {
		return Decision{}, fmt.Errorf("conflict.Check: %w", err)
	}
	return decisionFromRows(rows), nil
}

// Register records the workspace's intent over files and returns the decision
// computed against the intents that were already active. Because intents are
// best-effort, a warning does not prevent registration: the intent is recorded
// regardless so later checks see it.
func (p *Predictor) Register(ctx context.Context, workspaceID string, files []string) (*Intent, Decision, error) {
	files = normalize(files)

	// Compute the decision against the prior state, before this intent exists.
	decision, err := p.Check(ctx, workspaceID, files)
	if err != nil {
		return nil, Decision{}, err
	}

	id, err := newID()
	if err != nil {
		return nil, Decision{}, fmt.Errorf("conflict.Register: %w", err)
	}
	intent, err := p.store.CreateIntent(ctx, store.CreateIntentParams{ID: id, WorkspaceID: workspaceID})
	if err != nil {
		return nil, Decision{}, fmt.Errorf("conflict.Register: %w", err)
	}
	for _, f := range files {
		if err := p.store.AddIntentFile(ctx, store.AddIntentFileParams{IntentID: id, Path: f}); err != nil {
			return nil, Decision{}, fmt.Errorf("conflict.Register: add file %q: %w", f, err)
		}
	}

	return &Intent{ID: intent.ID, WorkspaceID: intent.WorkspaceID, Files: files}, decision, nil
}

// Release marks all of a workspace's intents inactive once its work is done, so
// they no longer count against future checks.
func (p *Predictor) Release(ctx context.Context, workspaceID string) error {
	if err := p.store.ReleaseWorkspaceIntents(ctx, workspaceID); err != nil {
		return fmt.Errorf("conflict.Release: %w", err)
	}
	return nil
}

// decisionFromRows groups overlap rows by workspace into a sorted Decision.
func decisionFromRows(rows []store.FindOverlappingIntentsRow) Decision {
	if len(rows) == 0 {
		return Decision{Verdict: VerdictClear}
	}
	byWorkspace := make(map[string][]string)
	for _, r := range rows {
		byWorkspace[r.WorkspaceID] = append(byWorkspace[r.WorkspaceID], r.Path)
	}

	conflicts := make([]Conflict, 0, len(byWorkspace))
	for ws, paths := range byWorkspace {
		sort.Strings(paths)
		conflicts = append(conflicts, Conflict{WorkspaceID: ws, Paths: paths})
	}
	sort.Slice(conflicts, func(i, j int) bool {
		return conflicts[i].WorkspaceID < conflicts[j].WorkspaceID
	})
	return Decision{Verdict: VerdictWarn, Conflicts: conflicts}
}

// normalize cleans, de-duplicates, and sorts paths so overlap comparison is
// stable regardless of how an agent spelled them (./a, a, dir/../a all collapse).
func normalize(files []string) []string {
	seen := make(map[string]struct{}, len(files))
	out := make([]string, 0, len(files))
	for _, f := range files {
		if f == "" {
			continue
		}
		c := path.Clean(f)
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// newID returns a short random hex id for an intent.
func newID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
