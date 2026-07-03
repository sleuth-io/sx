const HUES: Record<string, string> = {
  skill: "bg-indigo-50 text-indigo-700 dark:bg-indigo-950 dark:text-indigo-300",
  rule: "bg-amber-50 text-amber-700 dark:bg-amber-950 dark:text-amber-300",
  agent: "bg-emerald-50 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300",
  command: "bg-sky-50 text-sky-700 dark:bg-sky-950 dark:text-sky-300",
  mcp: "bg-violet-50 text-violet-700 dark:bg-violet-950 dark:text-violet-300",
  hook: "bg-rose-50 text-rose-700 dark:bg-rose-950 dark:text-rose-300",
};

export default function TypeBadge({
  type,
  label,
}: {
  type: string;
  label: string;
}) {
  const hue =
    HUES[type] ?? "bg-stone-100 text-stone-600 dark:bg-stone-800 dark:text-stone-300";
  return (
    <span
      className={`shrink-0 rounded-full px-2 py-0.5 text-[11px] font-medium ${hue}`}
    >
      {label}
    </span>
  );
}
