// StatusBadge renders a workspace or PR status as a coloured pill. Colours are
// Tailwind classes keyed by status; unknown statuses fall back to neutral.
const STATUS_STYLES: Record<string, string> = {
  active: "bg-emerald-100 text-emerald-700",
  done: "bg-slate-100 text-slate-600",
  error: "bg-red-100 text-red-700",
  queued: "bg-amber-100 text-amber-700",
  submitted: "bg-blue-100 text-blue-700",
  merged: "bg-emerald-100 text-emerald-700",
  failed: "bg-red-100 text-red-700",
};

export function StatusBadge({ status }: { status: string }) {
  const style = STATUS_STYLES[status] ?? "bg-slate-100 text-slate-600";
  return (
    <span
      className={`inline-block rounded-full px-2 py-0.5 text-xs font-medium ${style}`}
    >
      {status}
    </span>
  );
}
