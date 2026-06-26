package daemon

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/AyushPramanik/mesh/internal/conflict"
	"github.com/AyushPramanik/mesh/internal/queue"
	"github.com/AyushPramanik/mesh/internal/store"
	"github.com/AyushPramanik/mesh/internal/workspace"
	meshv1 "github.com/AyushPramanik/mesh/proto/mesh/v1"
)

// server adapts the daemon's domain managers to the generated gRPC service. It
// holds no state of its own; everything lives on the *Daemon, keeping the
// translation layer thin (CLAUDE.md: gRPC server in internal/daemon).
type server struct {
	meshv1.UnimplementedMeshServiceServer
	d *Daemon
}

// Register attaches the MeshService implementation to a gRPC server.
func (d *Daemon) Register(gs *grpc.Server) {
	meshv1.RegisterMeshServiceServer(gs, &server{d: d})
}

func (s *server) RegisterAgent(ctx context.Context, req *meshv1.RegisterAgentRequest) (*meshv1.Agent, error) {
	agent, err := s.d.Store.RegisterAgent(ctx, store.RegisterAgentParams{ID: req.GetId(), Name: req.GetName()})
	if err != nil {
		return nil, grpcErr(err)
	}
	return &meshv1.Agent{Id: agent.ID, Name: agent.Name}, nil
}

func (s *server) CreateWorkspace(ctx context.Context, req *meshv1.CreateWorkspaceRequest) (*meshv1.Workspace, error) {
	ws, err := s.d.Workspaces.Create(ctx, req.GetAgentId())
	if err != nil {
		return nil, grpcErr(err)
	}
	return workspaceProto(ws), nil
}

func (s *server) ListWorkspaces(_ *meshv1.ListWorkspacesRequest, stream grpc.ServerStreamingServer[meshv1.Workspace]) error {
	workspaces, err := s.d.Workspaces.List(stream.Context())
	if err != nil {
		return grpcErr(err)
	}
	for _, ws := range workspaces {
		if err := stream.Send(workspaceProto(ws)); err != nil {
			return err
		}
	}
	return nil
}

func (s *server) FinishWorkspace(ctx context.Context, req *meshv1.FinishWorkspaceRequest) (*meshv1.Workspace, error) {
	status := workspace.StatusDone
	if req.GetErrored() {
		status = workspace.StatusError
	}
	if err := s.d.Workspaces.Finish(ctx, req.GetId(), status); err != nil {
		return nil, grpcErr(err)
	}
	ws, err := s.d.Workspaces.Get(ctx, req.GetId())
	if err != nil {
		return nil, grpcErr(err)
	}
	return workspaceProto(ws), nil
}

func (s *server) DeleteWorkspace(ctx context.Context, req *meshv1.DeleteWorkspaceRequest) (*meshv1.DeleteWorkspaceResponse, error) {
	if err := s.d.Workspaces.Delete(ctx, req.GetId()); err != nil {
		return nil, grpcErr(err)
	}
	return &meshv1.DeleteWorkspaceResponse{}, nil
}

func (s *server) ReclaimWorktrees(_ *meshv1.ReclaimWorktreesRequest, stream grpc.ServerStreamingServer[meshv1.ReclaimedWorktree]) error {
	reclaimed, err := s.d.Workspaces.GC(stream.Context())
	if err != nil {
		return grpcErr(err)
	}
	for _, name := range reclaimed {
		if err := stream.Send(&meshv1.ReclaimedWorktree{Name: name}); err != nil {
			return err
		}
	}
	return nil
}

func (s *server) CheckIntent(ctx context.Context, req *meshv1.CheckIntentRequest) (*meshv1.Decision, error) {
	decision, err := s.d.Conflicts.Check(ctx, req.GetWorkspaceId(), req.GetFiles())
	if err != nil {
		return nil, grpcErr(err)
	}
	return decisionProto(decision), nil
}

func (s *server) RegisterIntent(ctx context.Context, req *meshv1.RegisterIntentRequest) (*meshv1.RegisterIntentResponse, error) {
	intent, decision, err := s.d.Conflicts.Register(ctx, req.GetWorkspaceId(), req.GetFiles())
	if err != nil {
		return nil, grpcErr(err)
	}
	return &meshv1.RegisterIntentResponse{
		IntentId: intent.ID,
		Decision: decisionProto(decision),
	}, nil
}

func (s *server) SubmitPR(ctx context.Context, req *meshv1.SubmitPRRequest) (*meshv1.PullRequest, error) {
	pr, err := s.d.Queue.Submit(ctx, queue.SubmitParams{
		WorkspaceID: req.GetWorkspaceId(),
		Branch:      req.GetBranch(),
		Title:       req.GetTitle(),
		Priority:    int(req.GetPriority()),
	})
	if err != nil {
		return nil, grpcErr(err)
	}
	return prProto(pr), nil
}

func (s *server) ListPRs(req *meshv1.ListPRsRequest, stream grpc.ServerStreamingServer[meshv1.PullRequest]) error {
	prs, err := s.d.Queue.List(stream.Context(), prStatusDomain(req.GetStatus()))
	if err != nil {
		return grpcErr(err)
	}
	for _, pr := range prs {
		if err := stream.Send(prProto(pr)); err != nil {
			return err
		}
	}
	return nil
}

func (s *server) PlanTrains(_ *meshv1.PlanTrainsRequest, stream grpc.ServerStreamingServer[meshv1.Train]) error {
	trains, err := s.d.Scheduler.Plan(stream.Context())
	if err != nil {
		return grpcErr(err)
	}
	for _, train := range trains {
		prs := make([]*meshv1.PullRequest, len(train.PRs))
		for i, pr := range train.PRs {
			prs[i] = prProto(pr)
		}
		if err := stream.Send(&meshv1.Train{Prs: prs}); err != nil {
			return err
		}
	}
	return nil
}

// grpcErr maps a domain error to a gRPC status. The mapping is coarse for now;
// typed sentinel errors can refine the codes as the domain grows.
func grpcErr(err error) error {
	if err == nil {
		return nil
	}
	return status.Error(codes.Internal, err.Error())
}

func workspaceProto(ws *workspace.Workspace) *meshv1.Workspace {
	return &meshv1.Workspace{
		Id:      ws.ID,
		AgentId: ws.AgentID,
		Branch:  ws.Branch,
		Path:    ws.Path,
		Status:  workspaceStatusProto(ws.Status),
	}
}

func workspaceStatusProto(s workspace.Status) meshv1.WorkspaceStatus {
	switch s {
	case workspace.StatusActive:
		return meshv1.WorkspaceStatus_WORKSPACE_STATUS_ACTIVE
	case workspace.StatusDone:
		return meshv1.WorkspaceStatus_WORKSPACE_STATUS_DONE
	case workspace.StatusError:
		return meshv1.WorkspaceStatus_WORKSPACE_STATUS_ERROR
	default:
		return meshv1.WorkspaceStatus_WORKSPACE_STATUS_UNSPECIFIED
	}
}

func decisionProto(d conflict.Decision) *meshv1.Decision {
	verdict := meshv1.Verdict_VERDICT_CLEAR
	if d.Verdict == conflict.VerdictWarn {
		verdict = meshv1.Verdict_VERDICT_WARN
	}
	conflicts := make([]*meshv1.Conflict, len(d.Conflicts))
	for i, c := range d.Conflicts {
		conflicts[i] = &meshv1.Conflict{WorkspaceId: c.WorkspaceID, Paths: c.Paths}
	}
	return &meshv1.Decision{Verdict: verdict, Conflicts: conflicts}
}

func prProto(pr queue.PR) *meshv1.PullRequest {
	return &meshv1.PullRequest{
		Id:          pr.ID,
		WorkspaceId: pr.WorkspaceID,
		Branch:      pr.Branch,
		Title:       pr.Title,
		Priority:    int32(pr.Priority),
		Status:      prStatusProto(pr.Status),
		Attempts:    int32(pr.Attempts),
	}
}

func prStatusProto(s queue.Status) meshv1.PRStatus {
	switch s {
	case queue.StatusQueued:
		return meshv1.PRStatus_PR_STATUS_QUEUED
	case queue.StatusSubmitted:
		return meshv1.PRStatus_PR_STATUS_SUBMITTED
	case queue.StatusMerged:
		return meshv1.PRStatus_PR_STATUS_MERGED
	case queue.StatusFailed:
		return meshv1.PRStatus_PR_STATUS_FAILED
	default:
		return meshv1.PRStatus_PR_STATUS_UNSPECIFIED
	}
}

// prStatusDomain maps a protocol status to the domain status, defaulting an
// unspecified filter to queued.
func prStatusDomain(s meshv1.PRStatus) queue.Status {
	switch s {
	case meshv1.PRStatus_PR_STATUS_SUBMITTED:
		return queue.StatusSubmitted
	case meshv1.PRStatus_PR_STATUS_MERGED:
		return queue.StatusMerged
	case meshv1.PRStatus_PR_STATUS_FAILED:
		return queue.StatusFailed
	default:
		return queue.StatusQueued
	}
}
