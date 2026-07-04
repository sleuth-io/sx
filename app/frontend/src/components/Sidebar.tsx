import type { main } from "../../wailsjs/go/models";

export type Scope =
  | { kind: "all" }
  | { kind: "installed" }
  | { kind: "drafts" }
  | { kind: "collection"; name: string }
  | { kind: "team"; name: string };

function scopeKey(s: Scope): string {
  if (s.kind === "collection") return "collection:" + s.name;
  if (s.kind === "team") return "team:" + s.name;
  return s.kind;
}

/**
 * Source-list sidebar (Apple HIG pattern): LIBRARY for structural views,
 * COLLECTIONS for the user's groupings (drop an asset row on one to add
 * it), TEAMS for who things are shared with.
 *
 * Asset-row drags are pointer-based (not HTML5 drag-and-drop, which the
 * native webview's file-drop handling swallows): Library hit-tests
 * [data-drop-collection] under the cursor and passes the hovered name in
 * dropCollection.
 */
export default function Sidebar({
  vault,
  scope,
  onScope,
  totalCount,
  installedCount,
  draftCount,
  collections,
  teams,
  teamAssetCounts,
  pinnedCollections,
  pinnedTeams,
  onNewCollection,
  onNewTeam,
  onBrowseCollections,
  onBrowseTeams,
  onSettings,
  dropCollection,
}: {
  vault: main.VaultInfo;
  scope: Scope;
  onScope: (scope: Scope) => void;
  totalCount: number;
  installedCount: number;
  draftCount: number;
  collections: main.Collection[];
  teams: main.TeamInfo[];
  teamAssetCounts: Record<string, number>;
  pinnedCollections: string[];
  pinnedTeams: string[];
  onNewCollection: () => void;
  onNewTeam: () => void;
  onBrowseCollections: () => void;
  onBrowseTeams: () => void;
  onSettings: () => void;
  dropCollection: string;
}) {
  const active = scopeKey(scope);

  return (
    <aside className="flex w-56 shrink-0 flex-col border-r border-line bg-surface">
      {/* Library switcher — the workspace-switcher pattern (Notion, Slack,
          Linear): names the current library and who you are, opens
          Settings to switch or add libraries. */}
      <div className="titlebar-drag px-2 pb-2 pt-9">
        <button
          onClick={onSettings}
          title="Switch or manage libraries (⌘,)"
          className="flex w-full items-center gap-2.5 rounded-lg px-2 py-1.5 text-left transition hover:bg-canvas"
          style={{ ["--wails-draggable" as never]: "no-drag" }}
        >
          <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-accent text-xs font-semibold text-white">
            sx
          </div>
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-1 text-sm font-semibold leading-tight">
              <span className="truncate">Library</span>
              <span className="text-[10px] text-ink-faint">▾</span>
            </div>
            <div
              className="truncate text-xs text-ink-faint"
              title={vault.location}
            >
              {vault.location}
            </div>
            {vault.type === "sleuth" && vault.identity && (
              <div
                className="truncate text-xs text-ink-faint"
                title={`Signed in as ${vault.identity}`}
              >
                {vault.identity}
              </div>
            )}
          </div>
        </button>
      </div>

      <nav className="min-h-0 flex-1 overflow-y-auto px-2 pb-2">
        <SectionLabel>LIBRARY</SectionLabel>
        <Row
          label="Assets"
          count={totalCount}
          active={active === "all"}
          onClick={() => onScope({ kind: "all" })}
        />
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
          <>
            {collections
              .filter((c) => pinnedCollections.includes(c.name))
              .map((c) => (
                <div
                  key={c.name}
                  data-drop-collection={c.name}
                  className={
                    dropCollection === c.name
                      ? "rounded-lg ring-2 ring-accent"
                      : undefined
                  }
                >
                  <Row
                    label={c.name}
                    count={(c.assets ?? []).length}
                    active={active === "collection:" + c.name}
                    onClick={() =>
                      onScope({ kind: "collection", name: c.name })
                    }
                  />
                </div>
              ))}
            <BrowseRow
              label={`All collections (${collections.length})…`}
              onClick={onBrowseCollections}
            />
          </>
        )}

        <div className="mt-4 flex items-center justify-between pr-1">
          <SectionLabel>TEAMS</SectionLabel>
          <button
            onClick={onNewTeam}
            title="New team"
            className="rounded px-1.5 text-sm leading-none text-ink-faint transition hover:text-accent"
          >
            +
          </button>
        </div>
        {teams.length === 0 ? (
          <button
            onClick={onNewTeam}
            className="mx-1 mt-1 w-[calc(100%-8px)] rounded-lg border border-dashed border-line px-3 py-2.5 text-left text-xs text-ink-faint transition hover:border-accent hover:text-accent"
          >
            Create a team to share assets with the right people
          </button>
        ) : (
          <>
            {teams
              .filter((t) => pinnedTeams.includes(t.name))
              .map((t) => (
                <Row
                  key={t.name}
                  label={t.name}
                  count={teamAssetCounts[t.name] ?? 0}
                  active={active === "team:" + t.name}
                  onClick={() => onScope({ kind: "team", name: t.name })}
                />
              ))}
            <BrowseRow
              label={`All teams (${teams.length})…`}
              onClick={onBrowseTeams}
            />
          </>
        )}
      </nav>
    </aside>
  );
}

function BrowseRow({
  label,
  onClick,
}: {
  label: string;
  onClick: () => void;
}) {
  return (
    <button
      onClick={onClick}
      className="w-full rounded-lg px-2 py-1.5 text-left text-xs text-ink-faint transition hover:bg-canvas hover:text-ink"
    >
      {label}
    </button>
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
