// Package workspace manages the lifecycle of agent workspaces: ephemeral,
// isolated environments each backed by a git worktree and tracked by a row in
// the store. It enforces the core invariant from CLAUDE.md — a workspace is
// always on exactly one branch, owned by exactly one agent — and never shares a
// workspace between agents.
//
// Dependencies are expressed as interfaces declared here, at the point of use,
// so the manager can be exercised against fakes and so the dependency graph
// stays acyclic (see CLAUDE.md Go conventions).
package workspace

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/AyushPramanik/mesh/internal/git"
	"github.com/AyushPramanik/mesh/internal/store"
)

// Status is the lifecycle state of a workspace.
type Status string

const (
	StatusActive Status = "active"
	StatusDone   Status = "done"
	StatusError  Status = "error"
)

// Workspace is the domain view of a workspace, decoupled from the store's
// generated row type.
type Workspace struct {
	ID      string
	AgentID string
	Branch  string
	Path    string
	Status  Status
}

// worktrees is the slice of the git layer the manager needs: worktree
// lifecycle on the shared object store.
type worktrees interface {
	CreateWorktree(ctx context.Context, name, branch string) (*git.Worktree, error)
	RemoveWorktree(ctx context.Context, name string) error
	ListWorktrees(ctx context.Context) ([]*git.Worktree, error)
}

// workspaceStore is the slice of the store the manager needs. *store.Store
// satisfies it via its embedded *Queries.
type workspaceStore interface {
	CreateWorkspace(ctx context.Context, arg store.CreateWorkspaceParams) (store.Workspace, error)
	GetWorkspace(ctx context.Context, id string) (store.Workspace, error)
	ListWorkspaces(ctx context.Context) ([]store.Workspace, error)
	ListWorkspacesByStatus(ctx context.Context, status string) ([]store.Workspace, error)
	SetWorkspaceStatus(ctx context.Context, arg store.SetWorkspaceStatusParams) error
	DeleteWorkspace(ctx context.Context, id string) error
}

// Manager creates, tracks, and reclaims workspaces. It owns no mutable global
// state; all state lives in the git object store and the store database.
type Manager struct {
	wt    worktrees
	store workspaceStore
}

// NewManager constructs a Manager over the given git and store backends.
func NewManager(wt worktrees, st workspaceStore) *Manager {
	return &Manager{wt: wt, store: st}
}

// Create provisions a new workspace for agentID: it allocates an id and branch,
// materialises a git worktree, and records the workspace. The agent must
// already be registered (the store enforces this). If recording the workspace
// fails, the freshly created worktree is removed so no orphan is left on disk.
func (m *Manager) Create(ctx context.Context, agentID string) (*Workspace, error) {
	id, err := newID()
	if err != nil {
		return nil, fmt.Errorf("workspace.Create: %w", err)
	}
	branch := fmt.Sprintf("mesh/%s/%s", agentID, id)

	wt, err := m.wt.CreateWorktree(ctx, id, branch)
	if err != nil {
		return nil, fmt.Errorf("workspace.Create: %w", err)
	}

	row, err := m.store.CreateWorkspace(ctx, store.CreateWorkspaceParams{
		ID:      id,
		AgentID: agentID,
		Branch:  branch,
		Path:    wt.Path,
	})
	if err != nil {
		// Roll back the worktree so disk state matches the store.
		if rmErr := m.wt.RemoveWorktree(ctx, id); rmErr != nil {
			return nil, fmt.Errorf("workspace.Create: %w (worktree cleanup also failed: %v)", err, rmErr)
		}
		return nil, fmt.Errorf("workspace.Create: %w", err)
	}
	return fromRow(row), nil
}

// Get returns the workspace with the given id.
func (m *Manager) Get(ctx context.Context, id string) (*Workspace, error) {
	row, err := m.store.GetWorkspace(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("workspace.Get: %w", err)
	}
	return fromRow(row), nil
}

// List returns all workspaces in creation order.
func (m *Manager) List(ctx context.Context) ([]*Workspace, error) {
	rows, err := m.store.ListWorkspaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("workspace.List: %w", err)
	}
	out := make([]*Workspace, len(rows))
	for i, row := range rows {
		out[i] = fromRow(row)
	}
	return out, nil
}

// Finish marks a workspace terminal (done or error) and reclaims its worktree.
// The workspace row is kept for history; use Delete to remove it entirely.
// Finishing a workspace that is already terminal is not an error.
func (m *Manager) Finish(ctx context.Context, id string, status Status) error {
	if status != StatusDone && status != StatusError {
		return fmt.Errorf("workspace.Finish: status %q is not terminal", status)
	}
	if err := m.store.SetWorkspaceStatus(ctx, store.SetWorkspaceStatusParams{
		Status: string(status),
		ID:     id,
	}); err != nil {
		return fmt.Errorf("workspace.Finish: %w", err)
	}
	if err := m.wt.RemoveWorktree(ctx, id); err != nil {
		return fmt.Errorf("workspace.Finish: reclaim worktree: %w", err)
	}
	return nil
}

// Delete removes a workspace entirely: its store row and, if still present, its
// worktree.
func (m *Manager) Delete(ctx context.Context, id string) error {
	// Best-effort worktree removal; a missing worktree is fine since Delete is
	// also used to clean up already-finished workspaces.
	_ = m.wt.RemoveWorktree(ctx, id)
	if err := m.store.DeleteWorkspace(ctx, id); err != nil {
		return fmt.Errorf("workspace.Delete: %w", err)
	}
	return nil
}

// GC reclaims orphaned worktrees: those present on disk with no corresponding
// active workspace. This recovers disk after a daemon crash, where a worktree
// may outlive the process that was tracking it. It returns the names reclaimed.
func (m *Manager) GC(ctx context.Context) ([]string, error) {
	active, err := m.store.ListWorkspacesByStatus(ctx, string(StatusActive))
	if err != nil {
		return nil, fmt.Errorf("workspace.GC: %w", err)
	}
	live := make(map[string]struct{}, len(active))
	for _, ws := range active {
		live[ws.ID] = struct{}{}
	}

	worktrees, err := m.wt.ListWorktrees(ctx)
	if err != nil {
		return nil, fmt.Errorf("workspace.GC: %w", err)
	}

	var reclaimed []string
	for _, wt := range worktrees {
		if _, ok := live[wt.Name]; ok {
			continue
		}
		if err := m.wt.RemoveWorktree(ctx, wt.Name); err != nil {
			return reclaimed, fmt.Errorf("workspace.GC: reclaim %q: %w", wt.Name, err)
		}
		reclaimed = append(reclaimed, wt.Name)
	}
	return reclaimed, nil
}

// fromRow maps a store row to the domain type.
func fromRow(row store.Workspace) *Workspace {
	return &Workspace{
		ID:      row.ID,
		AgentID: row.AgentID,
		Branch:  row.Branch,
		Path:    row.Path,
		Status:  Status(row.Status),
	}
}

// newID returns a short, random, URL- and path-safe workspace id. It is also
// used as the worktree name, so it must contain no path separators.
func newID() (string, error) {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("newID: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
