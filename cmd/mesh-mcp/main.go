// Command mesh-mcp is a Model Context Protocol server that exposes Mesh to LLM
// agents as callable tools. It is the agent-native front door: an agent (Claude
// Code or any MCP client) drives Mesh — workspaces, intents, commit/push, the PR
// queue, merge trains — entirely through tool calls, with no bespoke SDK glue.
//
// It is a thin client of a running meshd: every tool maps to a gRPC call on the
// daemon's Unix socket, so all state and orchestration stay in one place.
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/AyushPramanik/mesh/internal/daemon"
	meshv1 "github.com/AyushPramanik/mesh/proto/mesh/v1"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("mesh-mcp: %v", err)
	}
}

func run() error {
	// The repo (and thus which daemon to talk to) comes from MESH_REPO or the
	// working directory, matching how an MCP host launches the server.
	cfg, err := daemon.DefaultConfig(os.Getenv("MESH_REPO"))
	if err != nil {
		return err
	}
	conn, err := grpc.NewClient("unix://"+cfg.SocketPath, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("connecting to meshd: %w", err)
	}
	defer conn.Close()
	client := meshv1.NewMeshServiceClient(conn)

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "mesh",
		Title:   "Mesh — agent-native version control",
		Version: "0.1.0",
	}, nil)
	registerTools(server, client)

	return server.Run(context.Background(), &mcp.StdioTransport{})
}

// text wraps a plain string as a tool result.
func text(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

// registerTools wires every Mesh operation an agent needs as an MCP tool.
func registerTools(s *mcp.Server, c meshv1.MeshServiceClient) {
	type empty struct{}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "mesh_register_agent",
		Description: "Register (or re-register) an agent by id. Idempotent; call once before creating workspaces.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		ID   string `json:"id" jsonschema:"stable agent id"`
		Name string `json:"name" jsonschema:"human-readable agent name"`
	}) (*mcp.CallToolResult, any, error) {
		a, err := c.RegisterAgent(ctx, &meshv1.RegisterAgentRequest{Id: in.ID, Name: in.Name})
		if err != nil {
			return nil, nil, err
		}
		return text(fmt.Sprintf("registered agent %s", a.GetId())), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "mesh_create_workspace",
		Description: "Provision an isolated, worktree-backed workspace on a fresh branch for an agent. Returns the workspace id, branch, and path the agent should edit in.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		AgentID string `json:"agent_id" jsonschema:"id of the owning agent"`
	}) (*mcp.CallToolResult, any, error) {
		ws, err := c.CreateWorkspace(ctx, &meshv1.CreateWorkspaceRequest{AgentId: in.AgentID})
		if err != nil {
			return nil, nil, err
		}
		return text(fmt.Sprintf("workspace %s\nbranch: %s\npath: %s", ws.GetId(), ws.GetBranch(), ws.GetPath())), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "mesh_list_workspaces",
		Description: "List all workspaces with their agent, branch, and status.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ empty) (*mcp.CallToolResult, any, error) {
		stream, err := c.ListWorkspaces(ctx, &meshv1.ListWorkspacesRequest{})
		if err != nil {
			return nil, nil, err
		}
		var b strings.Builder
		for {
			ws, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, nil, err
			}
			fmt.Fprintf(&b, "%s  %s  [%s]\n", ws.GetId(), ws.GetBranch(), ws.GetStatus())
		}
		return text(orNone(b.String())), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "mesh_register_intent",
		Description: "Declare the files an agent is about to modify, BEFORE editing. Returns a conflict decision (clear or warn) against other agents' active intents, including overlapping paths. Best-effort prediction, not a lock.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		WorkspaceID string   `json:"workspace_id"`
		Files       []string `json:"files" jsonschema:"repository-relative paths the agent will touch"`
	}) (*mcp.CallToolResult, any, error) {
		res, err := c.RegisterIntent(ctx, &meshv1.RegisterIntentRequest{WorkspaceId: in.WorkspaceID, Files: in.Files})
		if err != nil {
			return nil, nil, err
		}
		return text(formatDecision(res.GetDecision())), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "mesh_check_intent",
		Description: "Check whether the given files overlap other agents' active intents, WITHOUT recording an intent.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		WorkspaceID string   `json:"workspace_id"`
		Files       []string `json:"files"`
	}) (*mcp.CallToolResult, any, error) {
		d, err := c.CheckIntent(ctx, &meshv1.CheckIntentRequest{WorkspaceId: in.WorkspaceID, Files: in.Files})
		if err != nil {
			return nil, nil, err
		}
		return text(formatDecision(d)), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "mesh_commit_workspace",
		Description: "Stage and commit all changes in a workspace's worktree. Returns the commit hash.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		ID      string `json:"id" jsonschema:"workspace id"`
		Message string `json:"message" jsonschema:"commit message"`
	}) (*mcp.CallToolResult, any, error) {
		res, err := c.CommitWorkspace(ctx, &meshv1.CommitWorkspaceRequest{Id: in.ID, Message: in.Message})
		if err != nil {
			return nil, nil, err
		}
		return text("committed " + res.GetCommitHash()), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "mesh_push_workspace",
		Description: "Push a workspace's branch to its remote (default origin).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		ID     string `json:"id"`
		Remote string `json:"remote,omitempty"`
	}) (*mcp.CallToolResult, any, error) {
		if _, err := c.PushWorkspace(ctx, &meshv1.PushWorkspaceRequest{Id: in.ID, Remote: in.Remote}); err != nil {
			return nil, nil, err
		}
		return text("pushed " + in.ID), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "mesh_submit_pr",
		Description: "Enqueue a pull request for a workspace branch. The queue dedupes by branch and submits in priority order.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		WorkspaceID string `json:"workspace_id"`
		Branch      string `json:"branch"`
		Title       string `json:"title"`
		Priority    int32  `json:"priority,omitempty" jsonschema:"higher submits first"`
	}) (*mcp.CallToolResult, any, error) {
		pr, err := c.SubmitPR(ctx, &meshv1.SubmitPRRequest{
			WorkspaceId: in.WorkspaceID, Branch: in.Branch, Title: in.Title, Priority: in.Priority,
		})
		if err != nil {
			return nil, nil, err
		}
		return text(fmt.Sprintf("queued PR %s for %s [%s]", pr.GetId(), pr.GetBranch(), pr.GetStatus())), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "mesh_plan_trains",
		Description: "Show the merge trains planned from queued PRs: each train is a batch of PRs that can land together without conflicting.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ empty) (*mcp.CallToolResult, any, error) {
		stream, err := c.PlanTrains(ctx, &meshv1.PlanTrainsRequest{})
		if err != nil {
			return nil, nil, err
		}
		var b strings.Builder
		n := 0
		for {
			train, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, nil, err
			}
			n++
			branches := make([]string, len(train.GetPrs()))
			for i, pr := range train.GetPrs() {
				branches[i] = pr.GetBranch()
			}
			fmt.Fprintf(&b, "train %d: %s\n", n, strings.Join(branches, ", "))
		}
		return text(orNone(b.String())), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "mesh_analyze_conflicts",
		Description: "Detect symbol-level (AST) conflicts between two branches' changes, including dependency conflicts where the branches share no files but one defines a Go symbol the other references. Use before parallelizing work to avoid silent semantic clashes.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		BranchA string `json:"branch_a"`
		BranchB string `json:"branch_b"`
	}) (*mcp.CallToolResult, any, error) {
		stream, err := c.AnalyzeConflicts(ctx, &meshv1.AnalyzeConflictsRequest{BranchA: in.BranchA, BranchB: in.BranchB})
		if err != nil {
			return nil, nil, err
		}
		var b strings.Builder
		for {
			cf, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, nil, err
			}
			fmt.Fprintf(&b, "%s: %s\n", cf.GetKind(), cf.GetSymbol())
		}
		if b.Len() == 0 {
			return text("no semantic conflicts"), nil, nil
		}
		return text(b.String()), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "mesh_land_train",
		Description: "Merge the next merge train (a conflict-free batch of queued PRs) into the base branch and return what landed.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ empty) (*mcp.CallToolResult, any, error) {
		stream, err := c.LandTrain(ctx, &meshv1.LandTrainRequest{})
		if err != nil {
			return nil, nil, err
		}
		var b strings.Builder
		for {
			l, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, nil, err
			}
			fmt.Fprintf(&b, "landed %s (%s)\n", l.GetBranch(), l.GetCommit())
		}
		return text(orNone(b.String())), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "mesh_finish_workspace",
		Description: "Mark a workspace done (or errored) and reclaim its worktree.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		ID      string `json:"id"`
		Errored bool   `json:"errored,omitempty"`
	}) (*mcp.CallToolResult, any, error) {
		ws, err := c.FinishWorkspace(ctx, &meshv1.FinishWorkspaceRequest{Id: in.ID, Errored: in.Errored})
		if err != nil {
			return nil, nil, err
		}
		return text(fmt.Sprintf("finished %s [%s]", ws.GetId(), ws.GetStatus())), nil, nil
	})
}

func formatDecision(d *meshv1.Decision) string {
	if d.GetVerdict() == meshv1.Verdict_VERDICT_CLEAR {
		return "CLEAR — no overlap with other active intents"
	}
	var b strings.Builder
	b.WriteString("WARN — overlaps other active work:\n")
	for _, c := range d.GetConflicts() {
		fmt.Fprintf(&b, "  workspace %s: %s\n", c.GetWorkspaceId(), strings.Join(c.GetPaths(), ", "))
	}
	return b.String()
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(none)"
	}
	return s
}
