// Command meshd is the Mesh daemon. It owns all local state and, once the
// typed protocol lands (build-order step 7), serves agents over its Unix
// socket. For now it initialises state, reclaims stale worktrees, and holds the
// socket until interrupted.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/AyushPramanik/mesh/internal/daemon"
	"github.com/AyushPramanik/mesh/internal/version"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "meshd:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		dev     = flag.Bool("dev", false, "verbose logging for development")
		repo    = flag.String("repo", "", "git repository to orchestrate (default: cwd)")
		showVer = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *showVer {
		fmt.Println("meshd", version.Version)
		return nil
	}

	cfg, err := daemon.DefaultConfig(*repo)
	if err != nil {
		return err
	}
	cfg.Dev = *dev

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	d, err := daemon.New(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = d.Close() }()

	return d.Run(ctx)
}
