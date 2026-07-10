import { useState, type MouseEvent, type ReactNode } from "react";
import type { main } from "../../wailsjs/go/models";
import { useSlot } from "../plugins/registry";
import PluginMount from "./PluginMount";

export type Scope =
  | { kind: "all" }
  | { kind: "installed" }
  | { kind: "personal" }
  | { kind: "drafts" }
  | { kind: "dashboard" }
  | { kind: "plugin-view"; name: string }
  | { kind: "collection"; name: string }
  | { kind: "team"; name: string }
  | { kind: "repo"; name: string };

function scopeKey(s: Scope): string {
  if (s.kind === "plugin-view") return "plugin-view:" + s.name;
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
 * their own drags via onCollectionDragHandle, and repo rows via
 * onRepoDragHandle (drop one on a team to add it to the team's
 * repositories).
 */
export default function Sidebar({
  vault,
  scope,
  onScope,
  totalCount,
  installedCount,
  personalCount,
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
  onRepoDragHandle,
  onUnpin,
  width,
}: {
  vault: main.VaultInfo;
  scope: Scope;
  onScope: (scope: Scope) => void;
  totalCount: number;
  installedCount: number;
  personalCount: number;
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
  onRepoDragHandle: (url: string, e: MouseEvent) => void;
  onUnpin: (kind: "collections" | "teams" | "repos", name: string) => void;
  width: number;
}) {
  const active = scopeKey(scope);
  // One flat PINNED list instead of per-type sections: type icons carry
  // the collection/team/repo distinction (the Finder/Linear/Notion
  // favorites pattern). The BROWSE rows below are the durable path to
  // the full catalogs, so the section can simply vanish when empty.
  const pinnedCollectionRows = collections.filter((c) =>
    pinnedCollections.includes(c.name),
  );
  const pinnedTeamRows = teams.filter((t) => pinnedTeams.includes(t.name));
  const pinnedRepoRows = repoAssets
    ? Object.keys(repoAssets)
        .filter((url) => pinnedRepos.includes(url))
        .sort((a, b) => repoLabel(a).localeCompare(repoLabel(b)))
    : [];
  const hasPins =
    pinnedCollectionRows.length + pinnedTeamRows.length + pinnedRepoRows.length >
    0;
  const sidebarPanels = useSlot("sidebar-panel");
  const dashboardWidgets = useSlot("dashboard-widget").length;
  const mainViews = useSlot("main-view");
  // Views split by their declared section: library views sit with core
  // navigation, tool views live under the collapsed TOOLS section.
  const libraryViews = mainViews.filter((v) => v.spec.section !== "tools");
  const toolViews = mainViews.filter((v) => v.spec.section === "tools");
  // Collapsed by default; remembering the choice per machine.
  const [toolsOpen, setToolsOpen] = useState(
    () => localStorage.getItem("sx-tools-open") === "1",
  );

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
        {/* Appears only once something is installed just for this user —
            no personal installs, no row (the app stays simple until the
            concept exists in their library). */}
        {personalCount > 0 && (
          <Row
            label="My skills"
            count={personalCount}
            active={active === "personal"}
            onClick={() => onScope({ kind: "personal" })}
          />
        )}
        {draftCount > 0 && (
          <Row
            label="Drafts"
            count={draftCount}
            active={active === "drafts"}
            onClick={() => onScope({ kind: "drafts" })}
            accent="amber"
          />
        )}
        {dashboardWidgets > 0 && (
          <Row
            label="Dashboard"
            count={dashboardWidgets}
            active={active === "dashboard"}
            onClick={() => onScope({ kind: "dashboard" })}
          />
        )}
        {/* Extension-contributed full-page views (views:main) that
            declare themselves library navigation. */}
        {libraryViews.map((v) => {
          const key = v.pluginId + ":" + v.spec.id;
          return (
            <Row
              key={key}
              label={v.spec.title}
              active={active === "plugin-view:" + key}
              onClick={() => onScope({ kind: "plugin-view", name: key })}
            />
          );
        })}

        {/* PINNED: quick access the user curated (plus the first-few
            defaults until they pin), one flat mixed list — icons carry
            the type. Drop targets and drag handles are unchanged, so
            dragging an asset onto a pinned collection/team/repo still
            works. Vanishes when empty; BROWSE below is always there. */}
        {hasPins && (
          <>
            <div className="mt-4">
              <SectionLabel>PINNED</SectionLabel>
            </div>
            {pinnedCollectionRows.map((c) => (
              <div
                key={"collection:" + c.name}
                data-drop-collection={c.name}
                onMouseDown={(e) => onCollectionDragHandle(c.name, e)}
                className={`group relative ${
                  dropCollection === c.name ? "rounded-lg ring-2 ring-accent" : ""
                }`}
              >
                <Row
                  icon={<CollectionIcon />}
                  label={c.name}
                  count={(c.assets ?? []).length}
                  active={active === "collection:" + c.name}
                  onClick={() => onScope({ kind: "collection", name: c.name })}
                  countYieldsOnHover
                />
                <UnpinButton
                  label={c.name}
                  onClick={() => onUnpin("collections", c.name)}
                />
              </div>
            ))}
            {pinnedTeamRows.map((t) => (
              <div
                key={"team:" + t.name}
                data-drop-team={t.name}
                className={`group relative ${
                  dropTeam === t.name ? "rounded-lg ring-2 ring-accent" : ""
                }`}
              >
                <Row
                  icon={<TeamIcon />}
                  label={t.name}
                  count={teamAssetCounts[t.name] ?? 0}
                  active={active === "team:" + t.name}
                  onClick={() => onScope({ kind: "team", name: t.name })}
                  countYieldsOnHover
                />
                <UnpinButton
                  label={t.name}
                  onClick={() => onUnpin("teams", t.name)}
                />
              </div>
            ))}
            {pinnedRepoRows.map((url) => (
              <div
                key={"repo:" + url}
                title={url}
                data-drop-repo={url}
                onMouseDown={(e) => onRepoDragHandle(url, e)}
                className={`group relative ${
                  dropRepo === url ? "rounded-lg ring-2 ring-accent" : ""
                }`}
              >
                <Row
                  icon={<RepoIcon />}
                  label={repoLabel(url)}
                  count={(repoAssets?.[url] ?? []).length}
                  active={active === "repo:" + url}
                  onClick={() => onScope({ kind: "repo", name: url })}
                  countYieldsOnHover
                />
                <UnpinButton
                  label={repoLabel(url)}
                  onClick={() => onUnpin("repos", url)}
                />
              </div>
            ))}
          </>
        )}

        {/* BROWSE: one count-bearing row per catalog — the counts keep
            the "how much is there" scope cue that fully hidden
            navigation loses. An empty catalog's row becomes its own
            create CTA. Repositories appear only when the library tracks
            them (Settings → Track repositories). */}
        <div className="mt-4">
          <SectionLabel>BROWSE</SectionLabel>
        </div>
        <CatalogRow
          icon={<CollectionIcon />}
          label="Collections"
          count={collections.length}
          onClick={
            collections.length > 0 ? onBrowseCollections : onNewCollection
          }
          title={
            collections.length === 0
              ? "Group related assets into your first collection"
              : "Browse and pin collections"
          }
        />
        <CatalogRow
          icon={<TeamIcon />}
          label="Teams"
          count={teams.length}
          onClick={teams.length > 0 ? onBrowseTeams : onNewTeam}
          title={
            teams.length === 0
              ? "Create a team to share assets with the right people"
              : "Browse and pin teams"
          }
        />
        {repoAssets !== null && (
          <CatalogRow
            icon={<RepoIcon />}
            label="Repositories"
            count={Object.keys(repoAssets).length}
            onClick={onBrowseRepos}
            title={
              Object.keys(repoAssets).length === 0
                ? "Assets scoped to repositories will show up here"
                : "Browse and pin repositories"
            }
          />
        )}

        {/* Extension-contributed tools live under ONE collapsed TOOLS
            section at the BOTTOM: sidebar panels (views:sidebar) and
            full-page views declaring section:"tools" — they act ON the
            library rather than navigate it, and expanded by default
            they crowd out collections and teams. Collapsed, the tool
            names still show so the section never reads as an empty
            header. */}
        {(sidebarPanels.length > 0 || toolViews.length > 0) && (
          <div className="mt-4" data-tools-section>
            <button
              onClick={() => {
                setToolsOpen((v) => {
                  localStorage.setItem("sx-tools-open", v ? "" : "1");
                  return !v;
                });
              }}
              className="w-full rounded-lg px-2 pb-1 pt-2 text-left transition hover:bg-canvas"
              aria-expanded={toolsOpen}
              title={toolsOpen ? "Collapse tools" : "Expand tools"}
            >
              <span className="flex items-center gap-1.5">
                <span className="text-[11px] font-semibold tracking-wide text-ink-faint">
                  TOOLS
                </span>
                <span className="text-xs leading-none text-ink-faint">
                  {toolsOpen ? "▾" : "▸"}
                </span>
              </span>
              {!toolsOpen && (
                <span className="mt-0.5 block truncate text-xs text-ink-faint">
                  {[...toolViews, ...sidebarPanels]
                    .map((p) => p.spec.title)
                    .join(" · ")}
                </span>
              )}
            </button>
            {toolsOpen &&
              toolViews.map((v) => {
                const key = v.pluginId + ":" + v.spec.id;
                return (
                  <Row
                    key={key}
                    label={v.spec.title}
                    active={active === "plugin-view:" + key}
                    onClick={() => onScope({ kind: "plugin-view", name: key })}
                  />
                );
              })}
            {toolsOpen &&
              sidebarPanels.map((p) => (
                <div key={p.pluginId + ":" + p.spec.id} className="mt-1">
                  <div className="px-2 text-[10px] font-semibold tracking-wider text-ink-faint">
                    {p.spec.title.toUpperCase()}
                  </div>
                  <PluginMount
                    pluginId={p.pluginId}
                    mount={p.spec.mount}
                    className="px-1"
                  />
                </div>
              ))}
          </div>
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

/** One catalog's browse entry: icon, name, count — the whole catalog
 * (and its create button) lives behind it in the browse view, keeping
 * the sidebar to pins while the count preserves the at-a-glance scope
 * cue. An empty catalog's click goes straight to create. */
function CatalogRow({
  icon,
  label,
  count,
  onClick,
  title,
}: {
  icon: ReactNode;
  label: string;
  count: number;
  onClick: () => void;
  title: string;
}) {
  return (
    <button
      onClick={onClick}
      title={title}
      className="flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-left text-[13px] text-ink-soft transition hover:bg-canvas hover:text-ink"
    >
      {icon}
      <span className="min-w-0 flex-1 truncate">{label}</span>
      <span className="text-xs tabular-nums text-ink-faint">{count}</span>
    </button>
  );
}

// Type icons for the mixed PINNED list (and the matching BROWSE rows):
// monochrome, inherit the row's text color, familiar shapes only —
// folder = collection, people = team, branch = repository.
function typeIcon(children: ReactNode): ReactNode {
  return (
    <svg
      aria-hidden="true"
      className="h-3.5 w-3.5 shrink-0 opacity-70"
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      {children}
    </svg>
  );
}

function CollectionIcon() {
  return typeIcon(
    <path d="M1.75 4.25c0-.55.45-1 1-1h3.1l1.4 1.5h5a1 1 0 0 1 1 1v6a1 1 0 0 1-1 1H2.75a1 1 0 0 1-1-1z" />,
  );
}

function TeamIcon() {
  return typeIcon(
    <>
      <circle cx="5.5" cy="4.5" r="2" />
      <path d="M1.75 13c.35-2.2 1.9-3.5 3.75-3.5s3.4 1.3 3.75 3.5" />
      <path d="M10.5 2.9a2 2 0 0 1 0 3.9" />
      <path d="M11.5 9.7c1.6.4 2.6 1.6 2.85 3.3" />
    </>,
  );
}

function RepoIcon() {
  return typeIcon(
    <>
      <circle cx="4.5" cy="3.5" r="1.75" />
      <circle cx="4.5" cy="12.5" r="1.75" />
      <circle cx="11.5" cy="3.5" r="1.75" />
      <path d="M4.5 5.25v5.5" />
      <path d="M11.5 5.25c0 2.75-2.75 3.5-4.75 3.75" />
    </>,
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
  icon,
  countYieldsOnHover,
}: {
  label: string;
  count?: number;
  active: boolean;
  onClick: () => void;
  accent?: "amber";
  icon?: ReactNode;
  /** PINNED rows: fade the count on row hover so the sibling
   * UnpinButton overlay (same spot) reads cleanly. Opacity, not
   * display — the reserved width means no layout shift. */
  countYieldsOnHover?: boolean;
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
      {icon}
      <span className="min-w-0 flex-1 truncate">{label}</span>
      {count !== undefined && (
        <span
          className={`text-xs tabular-nums transition-opacity ${
            countYieldsOnHover
              ? "group-hover:opacity-0 group-focus-within:opacity-0 "
              : ""
          }${
            accent === "amber" && !active
              ? "rounded-full bg-amber-50 px-1.5 text-amber-700 dark:bg-amber-950 dark:text-amber-300"
              : "text-ink-faint"
          }`}
        >
          {count}
        </span>
      )}
    </button>
  );
}

/** The hover/focus unpin affordance on PINNED rows. A real sibling
 * button overlaying the count — never nested inside the row button
 * (invalid interactive nesting), revealed by opacity so it stays in
 * the tab order and focus-visible can show it to keyboard users. */
function UnpinButton({ label, onClick }: { label: string; onClick: () => void }) {
  return (
    <button
      aria-label={`Unpin ${label} from the sidebar`}
      title="Unpin from sidebar"
      onClick={onClick}
      className="pointer-events-none absolute right-2 top-1/2 -translate-y-1/2 rounded p-0.5 text-ink-faint opacity-0 transition hover:text-accent focus-visible:pointer-events-auto focus-visible:opacity-100 group-hover:pointer-events-auto group-hover:opacity-100"
    >
      <PinIcon slashed />
    </button>
  );
}

/** Pin glyph (map-tack). `filled` marks pinned state on the scope
 * toolbar toggle; `slashed` is the sidebar's hover-unpin affordance. */
export function PinIcon({
  filled,
  slashed,
}: {
  filled?: boolean;
  slashed?: boolean;
}) {
  return (
    <svg
      aria-hidden="true"
      className="h-3.5 w-3.5 shrink-0"
      viewBox="0 0 16 16"
      fill={filled ? "currentColor" : "none"}
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d="M5.5 2.5h5l-.75 4 2.25 2.5v1h-8v-1L6.25 6.5z" />
      <path d="M8 10v3.5" />
      {slashed && <path d="M2.5 2.5l11 11" />}
    </svg>
  );
}
