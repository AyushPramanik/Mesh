package daemon

import (
	"fmt"
	"os"
	"path/filepath"
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
	}, nil
}

// DBPath is the SQLite database path inside the state directory.
func (c Config) DBPath() string {
	return filepath.Join(c.StateDir, "mesh.db")
}
