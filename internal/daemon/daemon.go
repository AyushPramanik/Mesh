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
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"google.golang.org/grpc"

	"github.com/AyushPramanik/mesh/internal/conflict"
	"github.com/AyushPramanik/mesh/internal/git"
	"github.com/AyushPramanik/mesh/internal/github"
	"github.com/AyushPramanik/mesh/internal/queue"
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
	Conflicts  *conflict.Predictor
	Queue      *queue.Queue
	Scheduler  *queue.Scheduler

	// processQueue is true when a PR submitter is configured; only then does
	// Run drain the queue (otherwise PRs enqueue and wait).
	processQueue bool
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

	// A configured GitHub client processes the queue; otherwise PRs accumulate
	// until credentials are supplied, rather than failing on submission.
	var submitter queue.Submitter = disabledSubmitter{}
	processQueue := false
	if cfg.GitHub.Configured() {
		client, err := github.New(github.Config{
			Token: cfg.GitHub.Token,
			Owner: cfg.GitHub.Owner,
			Repo:  cfg.GitHub.Repo,
			Base:  cfg.GitHub.Base,
		})
		if err != nil {
			st.Close()
			return nil, fmt.Errorf("daemon.New: %w", err)
		}
		submitter = client
		processQueue = true
	}

	base := cfg.GitHub.Base
	if base == "" {
		base = "main"
	}
	q := queue.New(st, submitter)

	d := &Daemon{
		cfg:          cfg,
		log:          log,
		Store:        st,
		Repo:         repo,
		Workspaces:   workspace.NewManager(repo, st),
		Conflicts:    conflict.New(st),
		Queue:        q,
		Scheduler:    queue.NewScheduler(q, gitFileSource{repo: repo, base: base}),
		processQueue: processQueue,
	}
	return d, nil
}

// gitFileSource adapts the git repo to queue.FileSource, reporting a branch's
// footprint as its diff against the configured base branch.
type gitFileSource struct {
	repo *git.Repo
	base string
}

func (g gitFileSource) ChangedFiles(ctx context.Context, branch string) ([]string, error) {
	return g.repo.ChangedFiles(ctx, g.base, branch)
}

// disabledSubmitter is used when no GitHub credentials are configured. It is
// never invoked, because the queue processing loop only runs when a real
// submitter is present, but it satisfies queue.New's non-nil contract.
type disabledSubmitter struct{}

func (disabledSubmitter) Submit(context.Context, queue.PR) error {
	return errors.New("no PR submitter configured")
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

	// Serve the typed agent protocol over the socket.
	gs := grpc.NewServer()
	d.Register(gs)
	serveErr := make(chan error, 1)
	go func() { serveErr <- gs.Serve(lis) }()

	// Drain the PR queue in the background when a submitter is configured.
	if d.processQueue {
		go func() {
			if err := d.Queue.Run(ctx, d.cfg.ProcessInterval); err != nil && !errors.Is(err, context.Canceled) {
				d.log.Error("pr queue processing stopped", "error", err)
			}
		}()
		d.log.Info("pr queue processing enabled", "interval", d.cfg.ProcessInterval)
	} else {
		d.log.Info("pr queue processing disabled (no GitHub credentials); PRs will be queued only")
	}

	// Serve the dashboard's HTTP/SSE API when configured.
	var httpSrv *http.Server
	if d.cfg.HTTPAddr != "" {
		httpSrv = &http.Server{Addr: d.cfg.HTTPAddr, Handler: d.httpHandler()}
		go func() {
			if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				d.log.Error("dashboard http server stopped", "error", err)
			}
		}()
		d.log.Info("dashboard api listening", "addr", d.cfg.HTTPAddr)
	}

	d.log.Info("mesh daemon ready",
		"repo", d.cfg.RepoDir,
		"socket", d.cfg.SocketPath,
		"db", d.cfg.DBPath(),
	)

	select {
	case <-ctx.Done():
		d.log.Info("mesh daemon shutting down")
		if httpSrv != nil {
			shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = httpSrv.Shutdown(shutCtx)
			cancel()
		}
		gs.GracefulStop()
		return nil
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("daemon.Run: serve: %w", err)
		}
		return nil
	}
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
