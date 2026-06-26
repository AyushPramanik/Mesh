// Mirrors the daemon's dashboard read-model (internal/daemon/http.go).

export interface Workspace {
  id: string;
  agentId: string;
  branch: string;
  status: "active" | "done" | "error";
}

export interface PR {
  id: string;
  branch: string;
  title: string;
  priority: number;
  status: "queued" | "submitted" | "merged" | "failed";
  attempts: number;
}

export interface Snapshot {
  workspaces: Workspace[];
  prs: PR[];
  trains: PR[][];
}
