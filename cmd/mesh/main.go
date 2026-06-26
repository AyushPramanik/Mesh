// Command mesh is the Mesh CLI. As of build-order step 7 it is a thin client of
// a running meshd, talking the typed gRPC protocol over the daemon's Unix
// socket. The command surface is unchanged from the earlier in-process version.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/AyushPramanik/mesh/internal/daemon"
	meshv1 "github.com/AyushPramanik/mesh/proto/mesh/v1"
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
		Use:          "mesh",
		Short:        "Agent-native version control",
		SilenceUsage: true,
	}
	root.PersistentFlags().StringVar(&flagRepo, "repo", "", "repository whose daemon to talk to (default: cwd)")
	root.PersistentFlags().BoolVar(&flagDev, "dev", false, "verbose logging")

	root.AddCommand(workspaceCmd(), prCmd(), gcCmd())
	return root
}

// dial connects to the daemon's Unix socket for the repository. The daemon
// (meshd) must be running.
func dial(cmd *cobra.Command) (meshv1.MeshServiceClient, func(), error) {
	cfg, err := daemon.DefaultConfig(flagRepo)
	if err != nil {
		return nil, nil, err
	}
	conn, err := grpc.NewClient("unix://"+cfg.SocketPath, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, fmt.Errorf("connecting to meshd: %w (is the daemon running?)", err)
	}
	return meshv1.NewMeshServiceClient(conn), func() { conn.Close() }, nil
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
			client, closeConn, err := dial(cmd)
			if err != nil {
				return err
			}
			defer closeConn()

			stream, err := client.ListWorkspaces(cmd.Context(), &meshv1.ListWorkspacesRequest{})
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tAGENT\tBRANCH\tSTATUS")
			n := 0
			for {
				ws, err := stream.Recv()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					return err
				}
				n++
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", ws.GetId(), ws.GetAgentId(), ws.GetBranch(), workspaceStatusName(ws.GetStatus()))
			}
			if n == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no workspaces")
				return nil
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
			client, closeConn, err := dial(cmd)
			if err != nil {
				return err
			}
			defer closeConn()

			// Register the agent first so a first-time agent can create a
			// workspace in one step (registration is idempotent).
			if _, err := client.RegisterAgent(cmd.Context(), &meshv1.RegisterAgentRequest{
				Id:   agentID,
				Name: orDefault(agentName, agentID),
			}); err != nil {
				return err
			}
			ws, err := client.CreateWorkspace(cmd.Context(), &meshv1.CreateWorkspaceRequest{AgentId: agentID})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "created workspace %s on branch %s\n%s\n", ws.GetId(), ws.GetBranch(), ws.GetPath())
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
			client, closeConn, err := dial(cmd)
			if err != nil {
				return err
			}
			defer closeConn()

			ws, err := client.FinishWorkspace(cmd.Context(), &meshv1.FinishWorkspaceRequest{
				Id:      args[0],
				Errored: asError,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "finished %s (%s)\n", ws.GetId(), workspaceStatusName(ws.GetStatus()))
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
			client, closeConn, err := dial(cmd)
			if err != nil {
				return err
			}
			defer closeConn()
			if _, err := client.DeleteWorkspace(cmd.Context(), &meshv1.DeleteWorkspaceRequest{Id: args[0]}); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "deleted %s\n", args[0])
			return nil
		},
	}
}

func prCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pr",
		Short: "Submit and inspect pull requests in the queue",
	}
	cmd.AddCommand(prSubmitCmd(), prListCmd())
	return cmd
}

func prSubmitCmd() *cobra.Command {
	var workspaceID, branch, title string
	var priority int32
	cmd := &cobra.Command{
		Use:   "submit",
		Short: "Queue a pull request for a workspace branch",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, closeConn, err := dial(cmd)
			if err != nil {
				return err
			}
			defer closeConn()

			pr, err := client.SubmitPR(cmd.Context(), &meshv1.SubmitPRRequest{
				WorkspaceId: workspaceID,
				Branch:      branch,
				Title:       title,
				Priority:    priority,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "queued PR %s for branch %s (%s)\n", pr.GetId(), pr.GetBranch(), prStatusName(pr.GetStatus()))
			return nil
		},
	}
	cmd.Flags().StringVar(&workspaceID, "workspace", "", "owning workspace id (required)")
	cmd.Flags().StringVar(&branch, "branch", "", "branch to open the PR from (required)")
	cmd.Flags().StringVar(&title, "title", "", "PR title (required)")
	cmd.Flags().Int32Var(&priority, "priority", 0, "higher submits first")
	_ = cmd.MarkFlagRequired("workspace")
	_ = cmd.MarkFlagRequired("branch")
	_ = cmd.MarkFlagRequired("title")
	return cmd
}

func prListCmd() *cobra.Command {
	var status string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List queued PRs by status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, closeConn, err := dial(cmd)
			if err != nil {
				return err
			}
			defer closeConn()

			stream, err := client.ListPRs(cmd.Context(), &meshv1.ListPRsRequest{Status: prStatusValue(status)})
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tBRANCH\tPRIORITY\tSTATUS\tATTEMPTS")
			n := 0
			for {
				pr, err := stream.Recv()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					return err
				}
				n++
				fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%d\n", pr.GetId(), pr.GetBranch(), pr.GetPriority(), prStatusName(pr.GetStatus()), pr.GetAttempts())
			}
			if n == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "no %s PRs\n", status)
				return nil
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&status, "status", "queued", "queued | submitted | merged | failed")
	return cmd
}

func gcCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "gc",
		Short: "Reclaim orphaned worktrees with no active workspace",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, closeConn, err := dial(cmd)
			if err != nil {
				return err
			}
			defer closeConn()

			stream, err := client.ReclaimWorktrees(cmd.Context(), &meshv1.ReclaimWorktreesRequest{})
			if err != nil {
				return err
			}
			n := 0
			for {
				_, err := stream.Recv()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					return err
				}
				n++
			}
			fmt.Fprintf(cmd.OutOrStdout(), "reclaimed %d worktree(s)\n", n)
			return nil
		},
	}
}

// orDefault returns a if non-empty, otherwise b.
func orDefault(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func workspaceStatusName(s meshv1.WorkspaceStatus) string {
	switch s {
	case meshv1.WorkspaceStatus_WORKSPACE_STATUS_ACTIVE:
		return "active"
	case meshv1.WorkspaceStatus_WORKSPACE_STATUS_DONE:
		return "done"
	case meshv1.WorkspaceStatus_WORKSPACE_STATUS_ERROR:
		return "error"
	default:
		return "unknown"
	}
}

func prStatusName(s meshv1.PRStatus) string {
	switch s {
	case meshv1.PRStatus_PR_STATUS_QUEUED:
		return "queued"
	case meshv1.PRStatus_PR_STATUS_SUBMITTED:
		return "submitted"
	case meshv1.PRStatus_PR_STATUS_MERGED:
		return "merged"
	case meshv1.PRStatus_PR_STATUS_FAILED:
		return "failed"
	default:
		return "unknown"
	}
}

func prStatusValue(name string) meshv1.PRStatus {
	switch name {
	case "submitted":
		return meshv1.PRStatus_PR_STATUS_SUBMITTED
	case "merged":
		return meshv1.PRStatus_PR_STATUS_MERGED
	case "failed":
		return meshv1.PRStatus_PR_STATUS_FAILED
	default:
		return meshv1.PRStatus_PR_STATUS_QUEUED
	}
}
