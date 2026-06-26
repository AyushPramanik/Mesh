package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Config locates the repository Mesh orchestrates and the local state derived
// from it. All paths are absolute once resolved by DefaultConfig.
type Config struct {
	// RepoDir is the git repository Mesh orchestrates.
	RepoDir string
	// StateDir holds Mesh's local state (the SQLite database, the socket).
	// Defaults to <RepoDir>/.mesh.
	StateDir string
	// SocketPath is the Unix socket the daemon listens on.
	SocketPath string
	// Dev enables verbose logging.
	Dev bool
	// GitHub configures PR submission. When incomplete, the daemon enqueues
	// PRs but does not process them.
	GitHub GitHubConfig
	// ProcessInterval is how often the daemon drains the PR queue.
	ProcessInterval time.Duration
	// HTTPAddr is the address the dashboard HTTP/SSE API listens on. Empty
	// disables it.
	HTTPAddr string
}

// GitHubConfig is the credentials and target repository for PR submission. It
// is populated from the environment by DefaultConfig.
type GitHubConfig struct {
	Token string
	Owner string
	Repo  string
	Base  string
}

// Configured reports whether enough is set to submit PRs.
func (g GitHubConfig) Configured() bool {
	return g.Token != "" && g.Owner != "" && g.Repo != ""
}

// DefaultConfig resolves a Config for the repository at repoDir, filling in the
// conventional state directory and socket path. If repoDir is empty the current
// working directory is used.
func DefaultConfig(repoDir string) (Config, error) {
	if repoDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return Config{}, fmt.Errorf("daemon.DefaultConfig: %w", err)
		}
		repoDir = wd
	}
	repoDir, err := filepath.Abs(repoDir)
	if err != nil {
		return Config{}, fmt.Errorf("daemon.DefaultConfig: %w", err)
	}

	stateDir := filepath.Join(repoDir, ".mesh")
	return Config{
		RepoDir:    repoDir,
		StateDir:   stateDir,
		SocketPath: filepath.Join(stateDir, "mesh.sock"),
		Dev:        false,
		GitHub: GitHubConfig{
			Token: os.Getenv("GITHUB_TOKEN"),
			Owner: os.Getenv("GITHUB_OWNER"),
			Repo:  os.Getenv("GITHUB_REPO"),
			Base:  os.Getenv("GITHUB_BASE"),
		},
		ProcessInterval: 10 * time.Second,
		HTTPAddr:        "127.0.0.1:7777",
	}, nil
}

// DBPath is the SQLite database path inside the state directory.
func (c Config) DBPath() string {
	return filepath.Join(c.StateDir, "mesh.db")
}
