import { useTrains } from "../hooks/useMeshStream";
import { Panel, Empty } from "./Panel";

export function MergeTrains() {
  const trains = useTrains();
  return (
    <Panel title="Merge Trains" count={trains.length}>
      {trains.length === 0 ? (
        <Empty label="Nothing to schedule" />
      ) : (
        <ol className="space-y-3">
          {trains.map((train, i) => (
            <li key={i} className="rounded-lg border border-slate-100 p-3">
              <p className="mb-2 text-xs font-semibold uppercase tracking-wide text-slate-400">
                Train {i + 1} · {train.length} PR{train.length === 1 ? "" : "s"}
              </p>
              <div className="flex flex-wrap gap-2">
                {train.map((pr) => (
                  <span
                    key={pr.id}
                    className="rounded-md bg-indigo-50 px-2 py-1 font-mono text-xs text-indigo-700"
                  >
                    {pr.branch}
                  </span>
                ))}
              </div>
            </li>
          ))}
        </ol>
      )}
    </Panel>
  );
}
