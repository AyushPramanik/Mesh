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
	Analyzer   *conflict.Analyzer

	// processQueue is true when a PR submitter is configured; only then does
	// Run drain the queue (otherwise PRs enqueue and wait).
	processQueue bool
	// ghStatus describes the GitHub credential state for `mesh doctor`.
	ghStatus string
	ghOK     bool
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

	// Resolve GitHub: fill owner/repo from the git remote when unset, build the
	// client, and verify the credentials so failures surface as clear messages
	// here rather than as stack traces at submission time. On any problem the
	// daemon still starts, with queue processing disabled and a status message
	// the `mesh doctor` command can report.
	var submitter queue.Submitter = disabledSubmitter{}
	processQueue := false
	ghStatus := "not configured — set GITHUB_TOKEN to enable PR submission"

	if cfg.GitHub.Owner == "" || cfg.GitHub.Repo == "" {
		if url, err := repo.RemoteURL(ctx, "origin"); err == nil {
			if owner, name, ok := github.ParseRepoURL(url); ok {
				if cfg.GitHub.Owner == "" {
					cfg.GitHub.Owner = owner
				}
				if cfg.GitHub.Repo == "" {
					cfg.GitHub.Repo = name
				}
			}
		}
	}

	if cfg.GitHub.Token == "" {
		// leave ghStatus as the default "not configured" hint
	} else if cfg.GitHub.Owner == "" || cfg.GitHub.Repo == "" {
		ghStatus = "GITHUB_TOKEN set but owner/repo unknown — set GITHUB_OWNER and GITHUB_REPO (no GitHub 'origin' remote found)"
	} else {
		client, err := github.New(github.Config{
			Token: cfg.GitHub.Token,
			Owner: cfg.GitHub.Owner,
			Repo:  cfg.GitHub.Repo,
			Base:  cfg.GitHub.Base,
		})
		if err != nil {
			ghStatus = err.Error()
		} else if err := client.Verify(ctx); err != nil {
			ghStatus = err.Error()
			log.Warn("github credentials rejected; PR processing disabled", "error", err)
		} else {
			submitter = client
			processQueue = true
			ghStatus = fmt.Sprintf("ok — submitting PRs to %s/%s", cfg.GitHub.Owner, cfg.GitHub.Repo)
		}
	}

	base := cfg.GitHub.Base
	if base == "" {
		base = "main"
	}
	q := queue.New(st, submitter)
	files := gitFileSource{repo: repo, base: base}

	d := &Daemon{
		cfg:          cfg,
		log:          log,
		Store:        st,
		Repo:         repo,
		Workspaces:   workspace.NewManager(repo, st),
		Conflicts:    conflict.New(st),
		Queue:        q,
		Scheduler:    queue.NewScheduler(q, files),
		Analyzer:     conflict.NewAnalyzer(files),
		processQueue: processQueue,
		ghStatus:     ghStatus,
		ghOK:         processQueue,
	}
	return d, nil
}

// GitHubStatus returns a human-readable description of the GitHub credential
// state and whether PR submission is active.
func (d *Daemon) GitHubStatus() (status string, ok bool) {
	return d.ghStatus, d.ghOK
}

// Landed records a PR that landed in the base branch.
type Landed struct {
	Branch string
	Commit string
}

// LandNextTrain merges the next planned merge train into the base branch in the
// main working tree and marks each PR merged. This is local continuous merge:
// the train's PRs are conflict-free by construction, so the merges land cleanly
// without going through the PR gate. It returns what it landed.
func (d *Daemon) LandNextTrain(ctx context.Context) ([]Landed, error) {
	train, err := d.Scheduler.NextTrain(ctx)
	if err != nil {
		return nil, fmt.Errorf("daemon.LandNextTrain: %w", err)
	}
	var landed []Landed
	for _, pr := range train.PRs {
		commit, err := d.Repo.MergeBranch(ctx, pr.Branch, fmt.Sprintf("mesh: land %s", pr.Branch))
		if err != nil {
			return landed, fmt.Errorf("daemon.LandNextTrain: merge %s: %w", pr.Branch, err)
		}
		if err := d.Queue.MarkMerged(ctx, pr.ID); err != nil {
			return landed, fmt.Errorf("daemon.LandNextTrain: %w", err)
		}
		landed = append(landed, Landed{Branch: pr.Branch, Commit: commit})
	}
	return landed, nil
}

// gitFileSource adapts the git repo to both queue.FileSource and
// conflict.BranchSource: a branch's footprint is its diff against the configured
// base branch, and file contents are read at the branch tip.
type gitFileSource struct {
	repo *git.Repo
	base string
}

func (g gitFileSource) ChangedFiles(ctx context.Context, branch string) ([]string, error) {
	return g.repo.ChangedFiles(ctx, g.base, branch)
}

func (g gitFileSource) ReadFile(ctx context.Context, branch, path string) ([]byte, error) {
	return g.repo.ReadFileAtBranch(ctx, branch, path)
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
	defer func() { _ = lis.Close() }()
	defer func() { _ = os.Remove(d.cfg.SocketPath) }()

	// Restrict the socket to its owner. The gRPC protocol has no auth, so file
	// permissions are the access boundary: without this, the socket's mode
	// depends on the process umask and may be group/world-accessible, letting
	// any local user drive Mesh.
	if err := os.Chmod(d.cfg.SocketPath, 0o600); err != nil {
		return fmt.Errorf("daemon.Run: secure socket: %w", err)
	}

	// Serve the typed agent protocol over the socket. The interceptors log any
	// handler error with its method, so failures are diagnosable from the
	// daemon log even though the gRPC status returned to the caller is coarse.
	gs := grpc.NewServer(
		grpc.ChainUnaryInterceptor(d.logUnaryErr),
		grpc.ChainStreamInterceptor(d.logStreamErr),
	)
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
		d.log.Info("pr queue processing disabled; PRs will be queued only", "github", d.ghStatus)
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

// logUnaryErr is a gRPC unary interceptor that logs the method and error of any
// failed handler. Successful calls are not logged to keep the daemon quiet.
func (d *Daemon) logUnaryErr(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	resp, err := handler(ctx, req)
	if err != nil {
		d.log.Error("rpc failed", "method", info.FullMethod, "error", err)
	}
	return resp, err
}

// logStreamErr is the streaming counterpart to logUnaryErr.
func (d *Daemon) logStreamErr(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	err := handler(srv, ss)
	if err != nil {
		d.log.Error("rpc stream failed", "method", info.FullMethod, "error", err)
	}
	return err
}

// newLogger returns a structured logger; --dev raises verbosity to debug.
func newLogger(dev bool) *slog.Logger {
	level := slog.LevelInfo
	if dev {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}
