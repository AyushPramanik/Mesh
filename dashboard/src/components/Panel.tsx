import type { ReactNode } from "react";

// Panel is the card chrome shared by every dashboard section.
export function Panel({
  title,
  count,
  children,
}: {
  title: string;
  count: number;
  children: ReactNode;
}) {
  return (
    <section className="rounded-xl border border-slate-200 bg-white shadow-sm">
      <header className="flex items-center justify-between border-b border-slate-100 px-4 py-3">
        <h2 className="text-sm font-semibold text-slate-700">{title}</h2>
        <span className="rounded-full bg-slate-100 px-2 py-0.5 text-xs text-slate-500">
          {count}
        </span>
      </header>
      <div className="p-4">{children}</div>
    </section>
  );
}

export function Empty({ label }: { label: string }) {
  return <p className="text-sm text-slate-400">{label}</p>;
}
