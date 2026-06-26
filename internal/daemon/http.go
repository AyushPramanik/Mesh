package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/AyushPramanik/mesh/internal/queue"
	"github.com/AyushPramanik/mesh/internal/workspace"
)

// The dashboard read-model: a flat, camelCase projection of the domain types,
// not the protobuf shapes, since the dashboard is read-only and SSE-driven
// (CLAUDE.md: SSE, no WebSocket complexity).
type snapshot struct {
	Workspaces []workspaceView `json:"workspaces"`
	PRs        []prView        `json:"prs"`
	Trains     [][]prView      `json:"trains"`
}

type workspaceView struct {
	ID      string `json:"id"`
	AgentID string `json:"agentId"`
	Branch  string `json:"branch"`
	Status  string `json:"status"`
}

type prView struct {
	ID       string `json:"id"`
	Branch   string `json:"branch"`
	Title    string `json:"title"`
	Priority int    `json:"priority"`
	Status   string `json:"status"`
	Attempts int    `json:"attempts"`
}

func toWorkspaceView(w *workspace.Workspace) workspaceView {
	return workspaceView{ID: w.ID, AgentID: w.AgentID, Branch: w.Branch, Status: string(w.Status)}
}

func toPRView(p queue.PR) prView {
	return prView{ID: p.ID, Branch: p.Branch, Title: p.Title, Priority: p.Priority, Status: string(p.Status), Attempts: p.Attempts}
}

// httpHandler builds the dashboard's HTTP API: point-in-time reads plus a live
// Server-Sent Events stream.
func (d *Daemon) httpHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/snapshot", d.handleSnapshot)
	mux.HandleFunc("GET /api/stream", d.handleStream)
	return withCORS(mux)
}

func (d *Daemon) collect(ctx context.Context) (snapshot, error) {
	workspaces, err := d.Workspaces.List(ctx)
	if err != nil {
		return snapshot{}, err
	}
	prs, err := d.Queue.List(ctx, queue.StatusQueued)
	if err != nil {
		return snapshot{}, err
	}
	trains, err := d.Scheduler.Plan(ctx)
	if err != nil {
		return snapshot{}, err
	}

	snap := snapshot{
		Workspaces: make([]workspaceView, len(workspaces)),
		PRs:        make([]prView, len(prs)),
		Trains:     make([][]prView, len(trains)),
	}
	for i, w := range workspaces {
		snap.Workspaces[i] = toWorkspaceView(w)
	}
	for i, p := range prs {
		snap.PRs[i] = toPRView(p)
	}
	for i, t := range trains {
		batch := make([]prView, len(t.PRs))
		for j, p := range t.PRs {
			batch[j] = toPRView(p)
		}
		snap.Trains[i] = batch
	}
	return snap, nil
}

func (d *Daemon) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	snap, err := d.collect(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(snap)
}

// handleStream pushes a fresh snapshot on connect and then every two seconds
// until the client disconnects. This read-heavy, low-frequency stream is why
// SSE is sufficient.
func (d *Daemon) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	send := func() bool {
		snap, err := d.collect(r.Context())
		if err != nil {
			return false
		}
		payload, err := json.Marshal(snap)
		if err != nil {
			return false
		}
		if _, err := w.Write([]byte("event: snapshot\ndata: ")); err != nil {
			return false
		}
		if _, err := w.Write(payload); err != nil {
			return false
		}
		if _, err := w.Write([]byte("\n\n")); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	if !send() {
		return
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if !send() {
				return
			}
		}
	}
}

// withCORS allows the dashboard dev server (a different origin) to call the API.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
