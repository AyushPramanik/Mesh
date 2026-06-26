import { usePRs } from "../hooks/useMeshStream";
import { Panel, Empty } from "./Panel";
import { StatusBadge } from "./StatusBadge";

export function PRQueue() {
  const prs = usePRs();
  return (
    <Panel title="PR Queue" count={prs.length}>
      {prs.length === 0 ? (
        <Empty label="Queue is empty" />
      ) : (
        <ul className="space-y-2">
          {prs.map((pr) => (
            <li
              key={pr.id}
              className="flex items-center justify-between gap-4 rounded-lg bg-slate-50 px-3 py-2"
            >
              <div className="min-w-0">
                <p className="truncate text-sm text-slate-700">{pr.title}</p>
                <p className="truncate font-mono text-xs text-slate-400">
                  {pr.branch}
                </p>
              </div>
              <div className="flex shrink-0 items-center gap-2">
                <span className="text-xs text-slate-400">p{pr.priority}</span>
                {pr.attempts > 0 && (
                  <span className="text-xs text-red-400">
                    {pr.attempts} tries
                  </span>
                )}
                <StatusBadge status={pr.status} />
              </div>
            </li>
          ))}
        </ul>
      )}
    </Panel>
  );
}
