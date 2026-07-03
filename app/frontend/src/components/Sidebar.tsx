import type { main } from "../../wailsjs/go/models";

export type Scope =
  | { kind: "all" }
  | { kind: "type"; type: string; label: string }
  | { kind: "installed" }
  | { kind: "drafts" }
  | { kind: "collection"; name: string };

function scopeKey(s: Scope): string {
  switch (s.kind) {
    case "type":
      return "type:" + s.type;
    case "collection":
      return "collection:" + s.name;
    default:
      return s.kind;
  }
}

/**
 * Source-list sidebar (Apple HIG pattern): LIBRARY gives structural views
 * (all, per type, installed, drafts); COLLECTIONS gives the user's own
 * groupings, with the create affordance living right in the section.
 */
export default function Sidebar({
  vault,
  scope,
  onScope,
  types,
  typeCounts,
  totalCount,
  installedCount,
  draftCount,
  collections,
  aiClients,
  onNewCollection,
  onSettings,
}: {
  vault: main.VaultInfo;
  scope: Scope;
  onScope: (scope: Scope) => void;
  types: [string, string][];
  typeCounts: Record<string, number>;
  totalCount: number;
  installedCount: number;
  draftCount: number;
  collections: main.Collection[];
  aiClients: main.AIClient[];
  onNewCollection: () => void;
  onSettings: () => void;
}) {
  const active = scopeKey(scope);

  return (
    <aside className="flex w-56 shrink-0 flex-col border-r border-line bg-surface">
      {/* App mark — part of the draggable titlebar strip */}
      <div className="titlebar-drag flex items-center gap-2.5 px-4 pb-3 pt-10">
        <div className="flex h-7 w-7 items-center justify-center rounded-lg bg-accent text-xs font-semibold text-white">
          sx
        </div>
        <div className="min-w-0">
          <div className="text-sm font-semibold leading-tight">Library</div>
          <div
            className="truncate text-xs text-ink-faint"
            title={vault.location}
          >
            {vault.location}
          </div>
        </div>
      </div>

      <nav className="min-h-0 flex-1 overflow-y-auto px-2 pb-2">
        <SectionLabel>LIBRARY</SectionLabel>
        <Row
          label="All assets"
          count={totalCount}
          active={active === "all"}
          onClick={() => onScope({ kind: "all" })}
        />
        {types.map(([key, label]) => (
          <Row
            key={key}
            label={label + "s"}
            count={typeCounts[key] ?? 0}
            active={active === "type:" + key}
            onClick={() => onScope({ kind: "type", type: key, label })}
          />
        ))}
        <Row
          label="In your AI tools"
          count={installedCount}
          active={active === "installed"}
          onClick={() => onScope({ kind: "installed" })}
        />
        {draftCount > 0 && (
          <Row
            label="Drafts"
            count={draftCount}
            active={active === "drafts"}
            onClick={() => onScope({ kind: "drafts" })}
            accent="amber"
          />
        )}

        <div className="mt-4 flex items-center justify-between pr-1">
          <SectionLabel>COLLECTIONS</SectionLabel>
          <button
            onClick={onNewCollection}
            title="New collection"
            className="rounded px-1.5 text-sm leading-none text-ink-faint transition hover:text-accent"
          >
            +
          </button>
        </div>
        {collections.length === 0 ? (
          <button
            onClick={onNewCollection}
            className="mx-1 mt-1 w-[calc(100%-8px)] rounded-lg border border-dashed border-line px-3 py-2.5 text-left text-xs text-ink-faint transition hover:border-accent hover:text-accent"
          >
            Group related assets into your first collection
          </button>
        ) : (
          collections.map((c) => (
            <Row
              key={c.name}
              label={c.name}
              count={(c.assets ?? []).length}
              active={active === "collection:" + c.name}
              onClick={() => onScope({ kind: "collection", name: c.name })}
            />
          ))
        )}
      </nav>

      {/* Footer: what "your AI tools" means, and settings */}
      <footer className="border-t border-line px-4 py-3">
        <div className="text-[11px] font-semibold tracking-wide text-ink-faint">
          YOUR AI TOOLS
        </div>
        {aiClients.length === 0 ? (
          <div className="mt-1 text-xs text-ink-faint">
            None detected on this machine
          </div>
        ) : (
          <ul className="mt-1 space-y-0.5">
            {aiClients.map((c) => (
              <li
                key={c.id}
                className="flex items-center gap-1.5 text-xs text-ink-soft"
              >
                <span className="h-1.5 w-1.5 rounded-full bg-emerald-500" />
                {c.name}
              </li>
            ))}
          </ul>
        )}
        <button
          onClick={onSettings}
          className="mt-2.5 flex w-full items-center gap-1.5 rounded-lg border border-line px-2.5 py-1.5 text-xs font-medium text-ink-soft transition hover:border-accent hover:text-ink"
        >
          ⚙ Settings
        </button>
      </footer>
    </aside>
  );
}

function SectionLabel({ children }: { children: string }) {
  return (
    <div className="px-2 pb-1 pt-2 text-[11px] font-semibold tracking-wide text-ink-faint">
      {children}
    </div>
  );
}

function Row({
  label,
  count,
  active,
  onClick,
  accent,
}: {
  label: string;
  count: number;
  active: boolean;
  onClick: () => void;
  accent?: "amber";
}) {
  return (
    <button
      onClick={onClick}
      className={`flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-left text-[13px] transition ${
        active
          ? "bg-accent-soft font-medium text-accent"
          : "text-ink-soft hover:bg-canvas hover:text-ink"
      }`}
    >
      <span className="min-w-0 flex-1 truncate">{label}</span>
      <span
        className={`text-xs tabular-nums ${
          accent === "amber" && !active
            ? "rounded-full bg-amber-50 px-1.5 text-amber-700 dark:bg-amber-950 dark:text-amber-300"
            : "text-ink-faint"
        }`}
      >
        {count}
      </span>
    </button>
  );
}
