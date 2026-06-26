import { useMeshStream, useSnapshot } from "../hooks/useMeshStream";
import { WorkspaceList } from "../components/WorkspaceList";
import { PRQueue } from "../components/PRQueue";
import { MergeTrains } from "../components/MergeTrains";

export function Dashboard() {
  // The single SSE connection for the whole app lives here.
  const { connected } = useMeshStream();
  const { isLoading, isError } = useSnapshot();

  return (
    <div className="min-h-screen bg-slate-50 text-slate-900">
      <header className="border-b border-slate-200 bg-white">
        <div className="mx-auto flex max-w-6xl items-center justify-between px-6 py-4">
          <div>
            <h1 className="text-lg font-semibold">Mesh</h1>
            <p className="text-xs text-slate-400">Agent-native version control</p>
          </div>
          <ConnectionDot connected={connected} />
        </div>
      </header>

      <main className="mx-auto max-w-6xl px-6 py-6">
        {isError ? (
          <p className="text-sm text-red-500">
            Could not reach the daemon. Is <code>meshd</code> running?
          </p>
        ) : isLoading ? (
          <p className="text-sm text-slate-400">Loading…</p>
        ) : (
          <div className="grid grid-cols-1 gap-6 lg:grid-cols-3">
            <WorkspaceList />
            <PRQueue />
            <MergeTrains />
          </div>
        )}
      </main>
    </div>
  );
}

function ConnectionDot({ connected }: { connected: boolean }) {
  return (
    <span className="flex items-center gap-2 text-xs text-slate-500">
      <span
        className={`h-2 w-2 rounded-full ${
          connected ? "bg-emerald-500" : "bg-slate-300"
        }`}
      />
      {connected ? "live" : "offline"}
    </span>
  );
}
