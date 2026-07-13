import { useState } from "react";
import type { main } from "../../wailsjs/go/models";

const STATUS_STYLES: Record<string, { label: string; className: string }> = {
  added: {
    label: "ADDED",
    className:
      "bg-emerald-100 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300",
  },
  modified: {
    label: "MODIFIED",
    className:
      "bg-amber-100 text-amber-700 dark:bg-amber-950 dark:text-amber-300",
  },
  deleted: {
    label: "DELETED",
    className: "bg-red-100 text-red-700 dark:bg-red-950 dark:text-red-300",
  },
};

/**
 * The draft sheet's "Changes" view: what publishing would change, rendered
 * like the skills.new PR diff — a summary strip, then one collapsible
 * unified diff per file.
 */
export default function DraftDiffView({
  diff,
  loading,
}: {
  diff: main.DraftDiff | null;
  loading: boolean;
}) {
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});

  if (loading || !diff) {
    return (
      <div className="space-y-3">
        <div className="h-12 animate-pulse rounded-lg bg-canvas" />
        <div className="h-64 animate-pulse rounded-lg bg-canvas" />
      </div>
    );
  }

  const files = diff.files ?? [];
  if (files.length === 0) {
    return (
      <div className="flex h-full items-center justify-center rounded-lg border border-line bg-canvas">
        <p className="px-6 py-10 text-sm text-ink-faint">
          No changes yet — this draft matches the published files.
        </p>
      </div>
    );
  }

  return (
    <div className="h-full space-y-3 overflow-y-auto pr-1">
      <div className="flex items-center justify-between rounded-lg border border-line bg-canvas px-4 py-2.5 text-sm">
        <span className="text-ink-soft">
          {files.length} file{files.length !== 1 ? "s" : ""} changed
        </span>
        <span className="font-mono text-xs">
          <span className="font-medium text-emerald-600 dark:text-emerald-400">
            +{diff.additions}
          </span>{" "}
          <span className="font-medium text-red-600 dark:text-red-400">
            −{diff.deletions}
          </span>
        </span>
      </div>

      {files.map((f) => (
        <FileSection
          key={f.path}
          file={f}
          collapsed={!!collapsed[f.path]}
          onToggle={() =>
            setCollapsed((c) => ({ ...c, [f.path]: !c[f.path] }))
          }
        />
      ))}
    </div>
  );
}

function FileSection({
  file,
  collapsed,
  onToggle,
}: {
  file: main.FileDiff;
  collapsed: boolean;
  onToggle: () => void;
}) {
  const status = STATUS_STYLES[file.status] ?? STATUS_STYLES.modified;
  return (
    <div className="overflow-hidden rounded-lg border border-line">
      <button
        onClick={onToggle}
        className="flex w-full items-center gap-2.5 bg-canvas px-3 py-2 text-left transition hover:bg-accent-soft/40"
      >
        <span className="text-[10px] text-ink-faint">
          {collapsed ? "▸" : "▾"}
        </span>
        <span
          className={`rounded px-1.5 py-0.5 text-[10px] font-semibold tracking-wide ${status.className}`}
        >
          {status.label}
        </span>
        <span className="min-w-0 flex-1 truncate font-mono text-xs text-ink">
          {file.path}
        </span>
        <span className="shrink-0 font-mono text-[11px]">
          {file.additions > 0 && (
            <span className="text-emerald-600 dark:text-emerald-400">
              +{file.additions}
            </span>
          )}{" "}
          {file.deletions > 0 && (
            <span className="text-red-600 dark:text-red-400">
              −{file.deletions}
            </span>
          )}
        </span>
      </button>

      {!collapsed && (
        <div className="overflow-x-auto border-t border-line bg-surface">
          <table className="w-full border-collapse font-mono text-xs leading-relaxed">
            <tbody>
              {(file.hunks ?? []).map((h, hi) => (
                <Hunk
                  key={hi}
                  hunk={h}
                  withHeader={
                    (file.hunks ?? []).length > 1 ||
                    h.oldStart > 1 ||
                    h.newStart > 1
                  }
                />
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

function Hunk({
  hunk,
  withHeader,
}: {
  hunk: main.DiffHunk;
  withHeader: boolean;
}) {
  return (
    <>
      {withHeader && (
        <tr className="bg-accent-soft/60 text-ink-faint">
          <td colSpan={3} className="select-none px-2 py-0.5" />
          <td className="px-3 py-0.5">
            @@ -{hunk.oldStart},{hunk.oldLines} +{hunk.newStart},
            {hunk.newLines} @@
          </td>
        </tr>
      )}
      {(hunk.lines ?? []).map((l, i) => (
        <Line key={i} line={l} />
      ))}
    </>
  );
}

function Line({ line }: { line: main.DiffLine }) {
  const rowClass =
    line.kind === "add"
      ? "bg-emerald-50 dark:bg-emerald-950/50"
      : line.kind === "del"
        ? "bg-red-50 dark:bg-red-950/40"
        : "";
  const markerClass =
    line.kind === "add"
      ? "text-emerald-600 dark:text-emerald-400"
      : line.kind === "del"
        ? "text-red-600 dark:text-red-400"
        : "";
  return (
    <tr className={rowClass}>
      <td className="w-10 select-none px-2 text-right text-ink-faint">
        {line.oldNo > 0 ? line.oldNo : ""}
      </td>
      <td className="w-10 select-none px-2 text-right text-ink-faint">
        {line.newNo > 0 ? line.newNo : ""}
      </td>
      <td className={`w-5 select-none text-center ${markerClass}`}>
        {line.kind === "add" ? "+" : line.kind === "del" ? "−" : ""}
      </td>
      <td className="whitespace-pre-wrap break-words px-3 text-ink">
        {line.text || " "}
      </td>
    </tr>
  );
}
