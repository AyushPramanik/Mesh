// Command mesh is the Mesh CLI. At this stage (build-order step 4) it drives
// the daemon's core in-process; step 7 turns it into a thin gRPC client of a
// running meshd. The command surface is intended to stay stable across that
// switch.
package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/AyushPramanik/mesh/internal/daemon"
	"github.com/AyushPramanik/mesh/internal/store"
	"github.com/AyushPramanik/mesh/internal/workspace"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

// persistent flags shared by every subcommand.
var (
	flagRepo string
	flagDev  bool
)

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "mesh",
		Short:         "Agent-native version control",
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	root.PersistentFlags().StringVar(&flagRepo, "repo", "", "git repository to operate on (default: cwd)")
	root.PersistentFlags().BoolVar(&flagDev, "dev", false, "verbose logging")

	root.AddCommand(workspaceCmd(), gcCmd())
	return root
}

// openCore builds the daemon's in-process core for a single CLI invocation.
func openCore(cmd *cobra.Command) (*daemon.Daemon, error) {
	cfg, err := daemon.DefaultConfig(flagRepo)
	if err != nil {
		return nil, err
	}
	cfg.Dev = flagDev
	return daemon.New(cmd.Context(), cfg)
}

func workspaceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "workspace",
		Aliases: []string{"ws"},
		Short:   "Manage agent workspaces",
	}
	cmd.AddCommand(workspaceListCmd(), workspaceCreateCmd(), workspaceFinishCmd(), workspaceRmCmd())
	return cmd
}

func workspaceListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List workspaces",
		RunE: func(cmd *cobra.Command, _ []string) error {
			d, err := openCore(cmd)
			if err != nil {
				return err
			}
			defer d.Close()

			workspaces, err := d.Workspaces.List(cmd.Context())
			if err != nil {
				return err
			}
			if len(workspaces) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no workspaces")
				return nil
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tAGENT\tBRANCH\tSTATUS")
			for _, ws := range workspaces {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", ws.ID, ws.AgentID, ws.Branch, ws.Status)
			}
			return w.Flush()
		},
	}
}

func workspaceCreateCmd() *cobra.Command {
	var agentID, agentName string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a workspace for an agent",
		RunE: func(cmd *cobra.Command, _ []string) error {
			d, err := openCore(cmd)
			if err != nil {
				return err
			}
			defer d.Close()

			// Register the agent up front so a first-time agent can create a
			// workspace without a separate step (the store FK requires it).
			if _, err := d.Store.RegisterAgent(cmd.Context(), store.RegisterAgentParams{
				ID:   agentID,
				Name: cmp(agentName, agentID),
			}); err != nil {
				return err
			}

			ws, err := d.Workspaces.Create(cmd.Context(), agentID)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "created workspace %s on branch %s\n%s\n", ws.ID, ws.Branch, ws.Path)
			return nil
		},
	}
	cmd.Flags().StringVar(&agentID, "agent", "", "agent id (required)")
	cmd.Flags().StringVar(&agentName, "name", "", "agent display name (default: agent id)")
	_ = cmd.MarkFlagRequired("agent")
	return cmd
}

func workspaceFinishCmd() *cobra.Command {
	var asError bool
	cmd := &cobra.Command{
		Use:   "finish <id>",
		Short: "Finish a workspace and reclaim its worktree",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, err := openCore(cmd)
			if err != nil {
				return err
			}
			defer d.Close()

			status := workspace.StatusDone
			if asError {
				status = workspace.StatusError
			}
			if err := d.Workspaces.Finish(cmd.Context(), args[0], status); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "finished %s (%s)\n", args[0], status)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asError, "error", false, "mark the workspace as errored rather than done")
	return cmd
}

func workspaceRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <id>",
		Short: "Delete a workspace and its worktree",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, err := openCore(cmd)
			if err != nil {
				return err
			}
			defer d.Close()
			if err := d.Workspaces.Delete(cmd.Context(), args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "deleted %s\n", args[0])
			return nil
		},
	}
}

func gcCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "gc",
		Short: "Reclaim orphaned worktrees with no active workspace",
		RunE: func(cmd *cobra.Command, _ []string) error {
			d, err := openCore(cmd)
			if err != nil {
				return err
			}
			defer d.Close()
			reclaimed, err := d.Workspaces.GC(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "reclaimed %d worktree(s)\n", len(reclaimed))
			return nil
		},
	}
}

// cmp returns a if non-empty, otherwise b.
func cmp(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
