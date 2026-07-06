import type { MouseEvent } from "react";
import type { main } from "../../wailsjs/go/models";

export type Scope =
  | { kind: "all" }
  | { kind: "installed" }
  | { kind: "drafts" }
  | { kind: "collection"; name: string }
  | { kind: "team"; name: string }
  | { kind: "repo"; name: string };

function scopeKey(s: Scope): string {
  if (s.kind === "collection") return "collection:" + s.name;
  if (s.kind === "team") return "team:" + s.name;
  if (s.kind === "repo") return "repo:" + s.name;
  return s.kind;
}

/** Short display label for a repository URL: "owner/repo". */
export function repoLabel(url: string): string {
  const cleaned = url.replace(/\.git$/, "").replace(/\/+$/, "");
  const sshMatch = /^git@[^:]+:(.+)$/.exec(cleaned);
  const path = sshMatch ? sshMatch[1] : cleaned.replace(/^\w+:\/\/[^/]+\//, "");
  const parts = path.split("/").filter(Boolean);
  return parts.slice(-2).join("/") || url;
}

/**
 * Source-list sidebar (Apple HIG pattern): LIBRARY for structural views,
 * COLLECTIONS for the user's groupings (drop an asset row on one to add
 * it), TEAMS for who things are shared with (drop an asset or a whole
 * collection on one to share).
 *
 * Row drags are pointer-based (not HTML5 drag-and-drop, which the
 * native webview's file-drop handling swallows): Library hit-tests
 * [data-drop-collection] / [data-drop-team] under the cursor and passes
 * the hovered name in dropCollection / dropTeam. Collection rows start
 * their own drags via onCollectionDragHandle.
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
  repoAssets,
  pinnedCollections,
  pinnedTeams,
  pinnedRepos,
  onNewCollection,
  onNewTeam,
  onBrowseCollections,
  onBrowseTeams,
  onBrowseRepos,
  onSettings,
  dropCollection,
  dropTeam,
  dropRepo,
  onCollectionDragHandle,
  width,
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
  // Repo URL → asset names; null when this library doesn't track repos
  // (the section is absent entirely).
  repoAssets: Record<string, string[]> | null;
  pinnedCollections: string[];
  pinnedTeams: string[];
  pinnedRepos: string[];
  onNewCollection: () => void;
  onNewTeam: () => void;
  onBrowseCollections: () => void;
  onBrowseTeams: () => void;
  onBrowseRepos: () => void;
  onSettings: () => void;
  dropCollection: string;
  dropTeam: string;
  dropRepo: string;
  onCollectionDragHandle: (name: string, e: MouseEvent) => void;
  width: number;
}) {
  const active = scopeKey(scope);

  return (
    <aside
      className="flex shrink-0 flex-col border-r border-line bg-surface"
      style={{ width }}
    >
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
          {vault.icon ? (
            <img
              src={vault.icon}
              alt=""
              className="h-7 w-7 shrink-0 rounded-lg border border-line object-cover"
            />
          ) : (
            <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg border border-white/10 bg-gradient-to-b from-[#2e3138] to-[#15171b] text-xs font-bold text-[#8fa6ff] shadow-[inset_0_1px_0_rgba(255,255,255,0.08)]">
              sx
            </div>
          )}
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-1 text-sm font-semibold leading-tight">
              <span className="truncate" title={vault.location}>
                {vault.name || "Library"}
              </span>
              <svg
                className="h-3.5 w-3.5 shrink-0 text-ink-faint"
                viewBox="0 0 16 16"
                fill="none"
                stroke="currentColor"
                strokeWidth="1.5"
                strokeLinecap="round"
                strokeLinejoin="round"
              >
                <path d="M5 6.5 8 3.5l3 3M5 9.5l3 3 3-3" />
              </svg>
            </div>
            <div className="truncate text-xs text-ink-faint">
              {vault.type === "sleuth"
                ? "Cloud library"
                : vault.type === "git"
                  ? "Git library"
                  : "Local library"}
            </div>
          </div>
        </button>
      </div>

      <nav className="min-h-0 flex-1 overflow-y-auto px-2 pb-2">
        <SectionLabel>LIBRARY</SectionLabel>
        <Row
          label="Skills"
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
                  onMouseDown={(e) => onCollectionDragHandle(c.name, e)}
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
                <div
                  key={t.name}
                  data-drop-team={t.name}
                  className={
                    dropTeam === t.name
                      ? "rounded-lg ring-2 ring-accent"
                      : undefined
                  }
                >
                  <Row
                    label={t.name}
                    count={teamAssetCounts[t.name] ?? 0}
                    active={active === "team:" + t.name}
                    onClick={() => onScope({ kind: "team", name: t.name })}
                  />
                </div>
              ))}
            <BrowseRow
              label={`All teams (${teams.length})…`}
              onClick={onBrowseTeams}
            />
          </>
        )}

        {/* Per-library opt-in (Settings → Track repositories): which repos
            assets are scoped to. Off means the concept never appears. Like
            collections and teams, only pinned repos live in the sidebar;
            the browse row searches and pins the rest. */}
        {repoAssets !== null && (
          <>
            <div className="mt-4 flex items-center justify-between pr-1">
              <SectionLabel>REPOSITORIES</SectionLabel>
            </div>
            {Object.keys(repoAssets).length === 0 ? (
              <div className="mx-1 mt-1 rounded-lg border border-dashed border-line px-3 py-2.5 text-xs text-ink-faint">
                Assets scoped to repositories will show up here
              </div>
            ) : (
              <>
                {Object.keys(repoAssets)
                  .filter((url) => pinnedRepos.includes(url))
                  .sort((a, b) => repoLabel(a).localeCompare(repoLabel(b)))
                  .map((url) => (
                    <div
                      key={url}
                      title={url}
                      data-drop-repo={url}
                      className={
                        dropRepo === url
                          ? "rounded-lg ring-2 ring-accent"
                          : undefined
                      }
                    >
                      <Row
                        label={repoLabel(url)}
                        count={(repoAssets[url] ?? []).length}
                        active={active === "repo:" + url}
                        onClick={() => onScope({ kind: "repo", name: url })}
                      />
                    </div>
                  ))}
                <BrowseRow
                  label={`All repositories (${Object.keys(repoAssets).length})…`}
                  onClick={onBrowseRepos}
                />
              </>
            )}
          </>
        )}
      </nav>

      {/* Who you are in this library: the skills.new account, or the git
          identity vault changes are attributed to. */}
      {vault.identity && (
        <div
          className="flex shrink-0 items-center gap-2 border-t border-line px-4 py-2.5"
          title={
            vault.type === "sleuth"
              ? `Signed in as ${vault.identity}`
              : `Vault changes are attributed to ${vault.identity} (from your git config)`
          }
        >
          <span className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-accent-soft text-[10px] font-semibold uppercase text-accent">
            {vault.identity[0]}
          </span>
          <span className="min-w-0 truncate text-xs text-ink-faint">
            {vault.identity}
          </span>
        </div>
      )}
    </aside>
  );
}

function BrowseRow({ label, onClick }: { label: string; onClick: () => void }) {
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
