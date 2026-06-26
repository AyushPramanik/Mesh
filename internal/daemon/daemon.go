// Package daemon assembles Mesh's local state — the git object store, the
// SQLite store, and the workspace manager — into a single *Daemon that is
// passed through the call tree. Per CLAUDE.md there is no package-level mutable
// state; everything hangs off this struct.
//
// At this stage (build-order step 4) the daemon owns its Unix socket but speaks
// no protocol yet: the CLI drives the same *Daemon in-process. Step 7 attaches
// the gRPC server to the socket and the CLI becomes a thin client.
package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"

	"github.com/AyushPramanik/mesh/internal/git"
	"github.com/AyushPramanik/mesh/internal/store"
	"github.com/AyushPramanik/mesh/internal/workspace"
)

// Daemon holds all live local state. Construct it with New and release it with
// Close.
type Daemon struct {
	cfg        Config
	log        *slog.Logger
	Store      *store.Store
	Repo       *git.Repo
	Workspaces *workspace.Manager
}

// New opens the git repository and store described by cfg and wires the
// workspace manager. The repository must already exist; the state directory is
// created if necessary.
func New(ctx context.Context, cfg Config) (*Daemon, error) {
	log := newLogger(cfg.Dev)

	repo, err := git.Open(cfg.RepoDir)
	if err != nil {
		return nil, fmt.Errorf("daemon.New: open repo %q: %w", cfg.RepoDir, err)
	}

	if err := os.MkdirAll(cfg.StateDir, 0o755); err != nil {
		return nil, fmt.Errorf("daemon.New: state dir: %w", err)
	}
	st, err := store.Open(ctx, cfg.DBPath())
	if err != nil {
		return nil, fmt.Errorf("daemon.New: %w", err)
	}

	d := &Daemon{
		cfg:        cfg,
		log:        log,
		Store:      st,
		Repo:       repo,
		Workspaces: workspace.NewManager(repo, st),
	}
	return d, nil
}

// Run starts the daemon: it reclaims orphaned worktrees left by a previous
// process, binds the Unix socket, and blocks until ctx is cancelled. No
// protocol is served yet (step 7 attaches gRPC here).
func (d *Daemon) Run(ctx context.Context) error {
	reclaimed, err := d.Workspaces.GC(ctx)
	if err != nil {
		return fmt.Errorf("daemon.Run: startup gc: %w", err)
	}
	if len(reclaimed) > 0 {
		d.log.Info("reclaimed stale worktrees", "count", len(reclaimed), "names", reclaimed)
	}

	// Remove a stale socket from a crashed predecessor before binding.
	if err := os.Remove(d.cfg.SocketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("daemon.Run: clear stale socket: %w", err)
	}
	lis, err := net.Listen("unix", d.cfg.SocketPath)
	if err != nil {
		return fmt.Errorf("daemon.Run: listen: %w", err)
	}
	defer lis.Close()
	defer os.Remove(d.cfg.SocketPath)

	d.log.Info("mesh daemon ready",
		"repo", d.cfg.RepoDir,
		"socket", d.cfg.SocketPath,
		"db", d.cfg.DBPath(),
	)

	<-ctx.Done()
	d.log.Info("mesh daemon shutting down")
	return nil
}

// Close releases the daemon's resources.
func (d *Daemon) Close() error {
	if d.Store != nil {
		return d.Store.Close()
	}
	return nil
}

// newLogger returns a structured logger; --dev raises verbosity to debug.
func newLogger(dev bool) *slog.Logger {
	level := slog.LevelInfo
	if dev {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}
