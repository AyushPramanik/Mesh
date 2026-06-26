import { useWorkspaces } from "../hooks/useMeshStream";
import { Panel, Empty } from "./Panel";
import { StatusBadge } from "./StatusBadge";

export function WorkspaceList() {
  const workspaces = useWorkspaces();
  return (
    <Panel title="Workspaces" count={workspaces.length}>
      {workspaces.length === 0 ? (
        <Empty label="No active workspaces" />
      ) : (
        <ul className="space-y-2">
          {workspaces.map((ws) => (
            <li
              key={ws.id}
              className="flex items-center justify-between gap-4 rounded-lg bg-slate-50 px-3 py-2"
            >
              <div className="min-w-0">
                <p className="truncate font-mono text-sm text-slate-700">
                  {ws.branch}
                </p>
                <p className="text-xs text-slate-400">agent {ws.agentId}</p>
              </div>
              <StatusBadge status={ws.status} />
            </li>
          ))}
        </ul>
      )}
    </Panel>
  );
}
