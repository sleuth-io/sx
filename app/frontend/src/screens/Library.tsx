import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  CreateDraftFromAsset,
  CreateDraftFromPaths,
  CreateTeam,
  DeleteCollection,
  GetCollectionSharing,
  GetDraft,
  InstallCollection,
  InstalledAssets,
  ListAIClients,
  ListAssets,
  ListCollections,
  ListDrafts,
  ListTeams,
  SetAssetTeamSharing,
  SetCollectionMembership,
  SetCollectionTeamSharing,
  ShareCollectionWithEveryone,
  TeamAssets,
} from "../../wailsjs/go/main/App";
import {
  EventsOff,
  EventsOn,
  OnFileDrop,
  OnFileDropOff,
} from "../../wailsjs/runtime/runtime";
import type { main } from "../../wailsjs/go/models";
import AddAssetModal from "../components/AddAssetModal";
import AssetDetail from "../components/AssetDetail";
import BrowseModal from "../components/BrowseModal";
import CollectionModal from "../components/CollectionModal";
import DraftSheet from "../components/DraftSheet";
import Modal from "../components/Modal";
import SettingsModal from "../components/SettingsModal";
import ShareModal from "../components/ShareModal";
import Sidebar, { Scope } from "../components/Sidebar";
import usePanelSize from "../lib/usePanelSize";
import TeamModal from "../components/TeamModal";
import TypeBadge from "../components/TypeBadge";

type ViewMode = "list" | "grid";
type SortMode = "updated" | "name";
type PinKind = "collections" | "teams";

function readPins(kind: PinKind, location: string): string[] | null {
  try {
    const raw = localStorage.getItem(`sx-pins-${kind}:${location}`);
    return raw ? (JSON.parse(raw) as string[]) : null;
  } catch {
    return null;
  }
}

/**
 * Library: a source-list sidebar (scopes + collections) and one scrollable
 * content pane. List view is the default — dense rows scan better than
 * cards for homogeneous text items; grid remains as a toggle.
 */
export default function Library({
  vault,
  onVaultChanged,
}: {
  vault: main.VaultInfo;
  onVaultChanged: () => void;
}) {
  const [assets, setAssets] = useState<main.AssetCard[] | null>(null);
  const [drafts, setDrafts] = useState<main.Draft[]>([]);
  const [collections, setCollections] = useState<main.Collection[]>([]);
  const [teams, setTeams] = useState<main.TeamInfo[]>([]);
  const [teamAssets, setTeamAssets] = useState<Record<string, string[]>>({});
  const [openTeam, setOpenTeam] = useState<main.TeamInfo | null>(null);
  const [showNewTeam, setShowNewTeam] = useState(false);
  const [newTeamName, setNewTeamName] = useState("");
  const [browse, setBrowse] = useState<"" | "collections" | "teams">("");
  const [shareCollection, setShareCollection] = useState("");
  // Pins are per-library. null = the user never pinned anything, so the
  // sidebar defaults to the first few; once they pin/unpin we persist.
  const [pinnedCollections, setPinnedCollections] = useState<string[] | null>(
    () => readPins("collections", vault.location),
  );
  const [pinnedTeams, setPinnedTeams] = useState<string[] | null>(() =>
    readPins("teams", vault.location),
  );
  const [typeFilter, setTypeFilter] = useState("");
  const [installedInfo, setInstalledInfo] = useState<main.InstalledAssetInfo[]>(
    [],
  );
  const [aiClients, setAiClients] = useState<main.AIClient[]>([]);
  const [error, setError] = useState("");
  const [query, setQuery] = useState("");
  const [scope, setScope] = useState<Scope>({ kind: "all" });
  const [view, setView] = useState<ViewMode>(
    () => (localStorage.getItem("sx-view") as ViewMode) || "list",
  );
  const [sort, setSort] = useState<SortMode>(
    () => (localStorage.getItem("sx-sort") as SortMode) || "updated",
  );
  const [showCollectionModal, setShowCollectionModal] = useState(false);
  const [showAddAsset, setShowAddAsset] = useState(false);
  const [showSettings, setShowSettings] = useState(false);
  const [newMenuOpen, setNewMenuOpen] = useState(false);
  const newMenuRef = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [openDraft, setOpenDraft] = useState<main.Draft | null>(null);
  const [dragging, setDragging] = useState(false);
  const [sidebarWidth, startSidebarResize] = usePanelSize(
    "sx-panel-sidebar",
    224,
    180,
    420,
  );

  // In-app drags: asset rows → collections or teams, collection rows →
  // teams. Pointer-based, NOT HTML5 drag-and-drop: the native webview's
  // file-drop handling swallows HTML5 drop events, so rows track the
  // mouse and drops hit-test [data-drop-collection] / [data-drop-team].
  type DragKind = "asset" | "collection";
  const [assetDrag, setAssetDrag] = useState<{
    name: string;
    x: number;
    y: number;
  } | null>(null);
  const [dropCollection, setDropCollection] = useState("");
  const [dropTeam, setDropTeam] = useState("");
  const pendingDragRef = useRef<{
    kind: DragKind;
    name: string;
    x: number;
    y: number;
  } | null>(null);
  const dragRef = useRef<{ kind: DragKind; name: string } | null>(null);
  const dropCollectionRef = useRef("");
  const dropTeamRef = useRef("");
  const dragHappenedRef = useRef(false);
  const [toast, setToast] = useState("");
  const [busyAction, setBusyAction] = useState(false);

  // Guard against overlapping loads resolving out of order (focus events,
  // post-mutation refreshes): only the newest generation's results apply.
  const loadGen = useRef(0);
  const load = useCallback(() => {
    const gen = ++loadGen.current;
    const apply =
      <T,>(setter: (v: T) => void) =>
      (v: T) => {
        if (gen === loadGen.current) setter(v);
      };
    setError("");
    ListAssets()
      .then(apply(setAssets))
      .catch((e) => {
        if (gen !== loadGen.current) return;
        setError(String(e));
        setAssets([]);
      });
    ListDrafts()
      .then(apply(setDrafts))
      .catch(() => apply(setDrafts)([]));
    ListCollections()
      .then(apply(setCollections))
      .catch(() => apply(setCollections)([]));
    InstalledAssets()
      .then(apply(setInstalledInfo))
      .catch(() => apply(setInstalledInfo)([]));
    ListTeams()
      .then(apply(setTeams))
      .catch(() => apply(setTeams)([]));
    TeamAssets()
      .then((m) => apply(setTeamAssets)(m ?? {}))
      .catch(() => apply(setTeamAssets)({}));
  }, []);

  useEffect(load, [load]);
  useEffect(() => {
    ListAIClients()
      .then(setAiClients)
      .catch(() => setAiClients([]));
  }, []);

  useEffect(() => {
    localStorage.setItem("sx-view", view);
  }, [view]);
  useEffect(() => {
    localStorage.setItem("sx-sort", sort);
  }, [sort]);

  // Reload when the window regains focus so remote changes appear.
  useEffect(() => {
    const onFocus = () => load();
    window.addEventListener("focus", onFocus);
    return () => window.removeEventListener("focus", onFocus);
  }, [load]);

  // Cmd/Ctrl+F or "/" focuses search.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const inField =
        document.activeElement instanceof HTMLInputElement ||
        document.activeElement instanceof HTMLTextAreaElement ||
        document.activeElement?.closest(".cm-editor");
      if (
        ((e.metaKey || e.ctrlKey) && e.key === "f") ||
        (!inField && e.key === "/")
      ) {
        e.preventDefault();
        searchRef.current?.focus();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  // Native file drops (Finder → window) become drafts.
  useEffect(() => {
    OnFileDrop((_x, _y, paths) => {
      setDragging(false);
      // Wails fires this for ANY drag ending in the window, including
      // in-app row drags that carry no files — ignore those.
      const real = (paths ?? []).filter((p) => p);
      if (real.length === 0) return;
      CreateDraftFromPaths(real)
        .then((draft) => {
          setOpenDraft(draft);
          load();
        })
        .catch((e) => setToastMessage(String(e)));
    }, false);
    const over = (e: DragEvent) => {
      // Only external file drags get the drop overlay — in-app asset
      // drags (rows → collections) have their own affordance.
      if (!e.dataTransfer?.types.includes("Files")) return;
      e.preventDefault();
      setDragging(true);
    };
    const leave = () => setDragging(false);
    window.addEventListener("dragover", over);
    window.addEventListener("dragleave", leave);
    window.addEventListener("drop", leave);
    return () => {
      OnFileDropOff();
      window.removeEventListener("dragover", over);
      window.removeEventListener("dragleave", leave);
      window.removeEventListener("drop", leave);
    };
  }, [load]);

  // Drive the in-app drag: activate past a small threshold, follow the
  // cursor with a ghost chip, hit-test drop targets, commit on mouseup.
  // Assets can drop on collections or teams; collections on teams only.
  useEffect(() => {
    const finish = () => {
      pendingDragRef.current = null;
      dragRef.current = null;
      dropCollectionRef.current = "";
      dropTeamRef.current = "";
      setAssetDrag(null);
      setDropCollection("");
      setDropTeam("");
      document.body.style.userSelect = "";
      document.body.style.cursor = "";
    };
    const onMove = (e: MouseEvent) => {
      const pending = pendingDragRef.current;
      if (!dragRef.current) {
        if (!pending) return;
        if (Math.hypot(e.clientX - pending.x, e.clientY - pending.y) < 6)
          return;
        dragRef.current = { kind: pending.kind, name: pending.name };
        dragHappenedRef.current = true;
        document.body.style.userSelect = "none";
        document.body.style.cursor = "grabbing";
      }
      const drag = dragRef.current;
      setAssetDrag({ name: drag.name, x: e.clientX, y: e.clientY });
      const selector =
        drag.kind === "asset"
          ? "[data-drop-collection], [data-drop-team]"
          : "[data-drop-team]";
      const hit = document
        .elementFromPoint(e.clientX, e.clientY)
        ?.closest(selector);
      // A collection must not highlight as a drop target for itself.
      const collection = hit?.getAttribute("data-drop-collection") ?? "";
      const team = hit?.getAttribute("data-drop-team") ?? "";
      dropCollectionRef.current = collection;
      dropTeamRef.current = team;
      setDropCollection(collection);
      setDropTeam(team);
    };
    const onUp = () => {
      const drag = dragRef.current;
      const collection = dropCollectionRef.current;
      const team = dropTeamRef.current;
      finish();
      // The click event (if any) fires synchronously after mouseup; clear
      // the suppress flag right after so the NEXT click isn't swallowed
      // when a drag ends off its origin row and no click fires at all.
      setTimeout(() => {
        dragHappenedRef.current = false;
      }, 0);
      if (!drag) return;
      if (drag.kind === "asset" && collection) {
        SetCollectionMembership(collection, drag.name, true)
          .then(() => {
            load();
            setToastMessage(`Added ${drag.name} to ${collection}`);
          })
          .catch((e) => setToastMessage(String(e)));
      } else if (drag.kind === "asset" && team) {
        SetAssetTeamSharing(drag.name, team, true)
          .then(() => {
            load();
            setToastMessage(`Shared ${drag.name} with ${team}`);
          })
          .catch((e) => setToastMessage(String(e)));
      } else if (drag.kind === "collection" && team) {
        SetCollectionTeamSharing(drag.name, team, true)
          .then(() => {
            load();
            setToastMessage(`Shared everything in ${drag.name} with ${team}`);
          })
          .catch((e) => setToastMessage(String(e)));
      }
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && dragRef.current) finish();
    };
    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("mousemove", onMove);
      window.removeEventListener("mouseup", onUp);
      window.removeEventListener("keydown", onKey);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [load]);

  // Close the New menu on outside clicks.
  useEffect(() => {
    if (!newMenuOpen) return;
    const onClick = (e: MouseEvent) => {
      if (
        !(e.target instanceof Node) ||
        !newMenuRef.current?.contains(e.target)
      )
        setNewMenuOpen(false);
    };
    window.addEventListener("mousedown", onClick);
    return () => window.removeEventListener("mousedown", onClick);
  }, [newMenuOpen]);

  const toastTimer = useRef(0);
  function setToastMessage(message: string) {
    setToast(message);
    window.clearTimeout(toastTimer.current);
    toastTimer.current = window.setTimeout(() => setToast(""), 4000);
  }
  useEffect(() => () => window.clearTimeout(toastTimer.current), []);

  const installed = useMemo(
    () => new Set(installedInfo.map((i) => i.name)),
    [installedInfo],
  );
  // The tracker is machine-wide; the sidebar count is THIS library's
  // installed assets, so count the intersection.
  const installedHereCount = useMemo(
    () => (assets ?? []).filter((a) => installed.has(a.name)).length,
    [assets, installed],
  );
  const installedScopes = useMemo(() => {
    const m = new Map<string, string[]>();
    for (const i of installedInfo) m.set(i.name, i.scopes ?? []);
    return m;
  }, [installedInfo]);

  // Native menu → frontend views (Settings Cmd+,; File → New …)
  useEffect(() => {
    EventsOn("open-settings", () => setShowSettings(true));
    EventsOn("new-skill", () => setShowAddAsset(true));
    EventsOn("new-collection", () => setShowCollectionModal(true));
    EventsOn("new-library", () => setShowSettings(true));
    return () =>
      EventsOff("open-settings", "new-skill", "new-collection", "new-library");
  }, []);

  const types = useMemo(() => {
    const seen = new Map<string, string>();
    for (const a of assets ?? []) {
      if (!seen.has(a.type)) seen.set(a.type, a.typeLabel);
    }
    return [...seen.entries()].sort();
  }, [assets]);

  const activeCollection = useMemo(
    () =>
      scope.kind === "collection"
        ? (collections.find((c) => c.name === scope.name) ?? null)
        : null,
    [collections, scope],
  );

  const activeTeam = useMemo(
    () =>
      scope.kind === "team"
        ? (teams.find((t) => t.name === scope.name) ?? null)
        : null,
    [teams, scope],
  );

  // Never-pinned libraries default to the first few so the sidebar isn't
  // empty; an explicit pin/unpin takes over from there.
  const shownCollectionPins =
    pinnedCollections ?? collections.slice(0, 5).map((c) => c.name);
  const shownTeamPins = pinnedTeams ?? teams.slice(0, 5).map((t) => t.name);

  const teamAssetCounts = useMemo(() => {
    const counts: Record<string, number> = {};
    for (const t of teams) counts[t.name] = (teamAssets[t.name] ?? []).length;
    return counts;
  }, [teams, teamAssets]);

  function setPins(kind: PinKind, next: string[]) {
    localStorage.setItem(
      `sx-pins-${kind}:${vault.location}`,
      JSON.stringify(next),
    );
    (kind === "collections" ? setPinnedCollections : setPinnedTeams)(next);
  }

  function togglePin(kind: PinKind, name: string) {
    const current =
      kind === "collections" ? shownCollectionPins : shownTeamPins;
    setPins(
      kind,
      current.includes(name)
        ? current.filter((n) => n !== name)
        : [...current, name],
    );
  }

  function ensurePinned(kind: PinKind, name: string) {
    const current =
      kind === "collections" ? shownCollectionPins : shownTeamPins;
    if (!current.includes(name)) setPins(kind, [...current, name]);
  }

  const visible = useMemo(() => {
    const q = query.trim().toLowerCase();
    const list = (assets ?? []).filter((a) => {
      if (typeFilter && a.type !== typeFilter) return false;
      switch (scope.kind) {
        case "installed":
          if (!installed.has(a.name)) return false;
          break;
        case "drafts":
          return false;
        case "collection":
          if (!(activeCollection?.assets ?? []).includes(a.name)) return false;
          break;
        case "team":
          if (!(teamAssets[scope.name] ?? []).includes(a.name)) return false;
          break;
        case "all":
          break;
      }
      if (!q) return true;
      return (
        a.name.toLowerCase().includes(q) ||
        a.description.toLowerCase().includes(q)
      );
    });
    return list.sort((a, b) => {
      if (sort === "name") return a.name.localeCompare(b.name);
      return (b.updatedAt || "").localeCompare(a.updatedAt || "");
    });
  }, [
    assets,
    query,
    scope,
    installed,
    activeCollection,
    teamAssets,
    sort,
    typeFilter,
  ]);

  // Collections fully shared with the viewed team — every asset in the
  // collection is installed for the team (same rule GetCollectionSharing
  // uses), so the collection itself belongs in the team's list.
  const teamCollections = useMemo(() => {
    if (scope.kind !== "team") return [];
    const shared = new Set(teamAssets[scope.name] ?? []);
    const q = query.trim().toLowerCase();
    return collections.filter(
      (c) =>
        (c.assets ?? []).length > 0 &&
        (c.assets ?? []).every((a) => shared.has(a)) &&
        (!q || c.name.toLowerCase().includes(q)),
    );
  }, [scope, collections, teamAssets, query]);

  const visibleDrafts = useMemo(() => {
    // Drafts are local, unpublished work — they live in the Drafts view
    // only, not mixed into the library's published skills.
    if (scope.kind !== "drafts") return [];
    const q = query.trim().toLowerCase();
    return drafts.filter(
      (d) =>
        !q ||
        d.name.toLowerCase().includes(q) ||
        d.description.toLowerCase().includes(q),
    );
  }, [drafts, query, scope]);

  async function editAsset(name: string) {
    try {
      const draft = await CreateDraftFromAsset(name);
      setSelected(null);
      setOpenDraft(draft);
      load();
    } catch (e) {
      setToastMessage(String(e));
    }
  }

  async function openExistingDraft(id: string) {
    try {
      setOpenDraft(await GetDraft(id));
    } catch (e) {
      setToastMessage(String(e));
    }
  }

  const scopeTitle = (() => {
    switch (scope.kind) {
      case "all":
        return "Skills";
      case "installed":
        return "In your AI tools";
      case "drafts":
        return "Drafts";
      case "collection":
        return scope.name;
      case "team":
        return scope.name;
    }
  })();

  async function createTeam() {
    const name = newTeamName.trim();
    if (!name) return;
    try {
      const team = await CreateTeam(name);
      setShowNewTeam(false);
      setNewTeamName("");
      load();
      ensurePinned("teams", team.name);
      setScope({ kind: "team", name: team.name });
      setSelected(null);
    } catch (e) {
      setToastMessage(String(e));
    }
  }

  // The badge column fits the longest label in the current list —
  // "Collection" rows only appear in team views.
  const badgeWidth = teamCollections.length > 0 ? "w-[76px]" : "w-14";

  // Large vaults: render rows incrementally instead of mounting thousands
  // of DOM nodes at once.
  const RENDER_CAP = 200;
  const [renderCap, setRenderCap] = useState(RENDER_CAP);
  useEffect(() => {
    setRenderCap(RENDER_CAP);
  }, [scope, query, typeFilter, sort]);
  const shown = useMemo(
    () => visible.slice(0, renderCap),
    [visible, renderCap],
  );

  const nothingToShow =
    visible.length === 0 &&
    visibleDrafts.length === 0 &&
    teamCollections.length === 0 &&
    !error;

  return (
    <div
      className="flex h-full bg-canvas"
      style={{ ["--wails-drop-target" as never]: "drop" }}
    >
      <Sidebar
        vault={vault}
        scope={scope}
        onScope={(s) => {
          // A drag that started and ended on the same row still fires a
          // click — don't treat it as navigation.
          if (dragHappenedRef.current) {
            dragHappenedRef.current = false;
            return;
          }
          setScope(s);
          setSelected(null);
        }}
        totalCount={(assets ?? []).length}
        installedCount={installedHereCount}
        draftCount={drafts.length}
        collections={collections}
        teams={teams}
        teamAssetCounts={teamAssetCounts}
        pinnedCollections={shownCollectionPins}
        pinnedTeams={shownTeamPins}
        onNewCollection={() => setShowCollectionModal(true)}
        onNewTeam={() => setShowNewTeam(true)}
        onBrowseCollections={() => setBrowse("collections")}
        onBrowseTeams={() => setBrowse("teams")}
        onSettings={() => setShowSettings(true)}
        dropCollection={dropCollection}
        dropTeam={dropTeam}
        onCollectionDragHandle={(name, e) => {
          if (e.button !== 0) return;
          dragHappenedRef.current = false;
          pendingDragRef.current = {
            kind: "collection",
            name,
            x: e.clientX,
            y: e.clientY,
          };
        }}
        width={sidebarWidth}
      />

      {/* Sidebar resize handle */}
      <div
        onMouseDown={startSidebarResize}
        title="Drag to resize"
        className="-ml-1 w-1.5 shrink-0 cursor-col-resize transition hover:bg-accent/40"
        style={{ ["--wails-draggable" as never]: "no-drag" }}
      />

      <div className="flex min-w-0 flex-1 flex-col">
        {/* Toolbar */}
        <header className="titlebar-drag shrink-0 border-b border-line bg-surface">
          <div className="flex items-center gap-3 px-5 pb-3 pt-9">
            <h1 className="text-sm font-semibold">{scopeTitle}</h1>
            <span className="text-xs text-ink-faint">
              {visible.length + visibleDrafts.length + teamCollections.length}
            </span>
            {scope.kind === "installed" && aiClients.length > 0 && (
              <span className="text-xs text-ink-faint">
                · delivered to {aiClients.map((c) => c.name).join(", ")}
              </span>
            )}

            <div className="flex-1" />

            <div
              className="flex h-9 items-center gap-2"
              style={{ ["--wails-draggable" as never]: "no-drag" }}
            >
              <div className="relative h-full">
                <input
                  ref={searchRef}
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                  placeholder="Search…"
                  className="peer h-full w-56 rounded-lg border border-line bg-canvas px-3 pr-8 text-sm outline-none focus:border-accent"
                />
                {!query && (
                  <kbd className="pointer-events-none absolute right-2 top-1/2 -translate-y-1/2 rounded border border-line bg-surface px-1.5 py-0.5 font-mono text-[10px] text-ink-faint peer-focus:hidden">
                    /
                  </kbd>
                )}
              </div>

              <select
                value={typeFilter}
                onChange={(e) => setTypeFilter(e.target.value)}
                title="Filter by type"
                className="h-full rounded-lg border border-line bg-canvas py-0 pl-3 pr-7 text-sm text-ink-soft outline-none"
              >
                <option value="">All types</option>
                {types.map(([key, label]) => (
                  <option key={key} value={key}>
                    {label}s
                  </option>
                ))}
              </select>

              <select
                value={sort}
                onChange={(e) => setSort(e.target.value as SortMode)}
                title="Sort"
                className="h-full rounded-lg border border-line bg-canvas py-0 pl-3 pr-7 text-sm text-ink-soft outline-none"
              >
                <option value="updated">Recently updated</option>
                <option value="name">Name</option>
              </select>

              <div className="flex h-full overflow-hidden rounded-lg border border-line">
                <ViewToggle
                  label="List"
                  active={view === "list"}
                  onClick={() => setView("list")}
                />
                <ViewToggle
                  label="Grid"
                  active={view === "grid"}
                  onClick={() => setView("grid")}
                />
              </div>

              <div className="relative h-full" ref={newMenuRef}>
                <button
                  onClick={() => setNewMenuOpen((v) => !v)}
                  className="flex h-full items-center gap-1.5 rounded-lg bg-accent px-3.5 text-sm font-medium text-white transition hover:opacity-90"
                >
                  <span className="text-base leading-none">+</span> New
                  <span className="text-[10px] opacity-70">▾</span>
                </button>
                {newMenuOpen && (
                  <div className="absolute right-0 z-40 mt-1.5 w-56 overflow-hidden rounded-xl border border-line bg-surface py-1 shadow-xl">
                    <MenuItem
                      label="New skill…"
                      hint="Files, zip, or scratch"
                      onClick={() => {
                        setNewMenuOpen(false);
                        setShowAddAsset(true);
                      }}
                    />
                    <MenuItem
                      label="New collection…"
                      hint="Group related assets"
                      onClick={() => {
                        setNewMenuOpen(false);
                        setShowCollectionModal(true);
                      }}
                    />
                  </div>
                )}
              </div>
            </div>
          </div>

          {activeCollection && (
            <div
              className="flex items-center gap-3 border-t border-line bg-accent-soft/50 px-5 py-2 text-xs"
              style={{ ["--wails-draggable" as never]: "no-drag" }}
            >
              <span className="text-ink-soft">
                {activeCollection.description ||
                  "Assets in this collection can be set up together."}
              </span>
              <div className="flex-1" />
              <button
                disabled={
                  busyAction || (activeCollection.assets ?? []).length === 0
                }
                onClick={() => {
                  setBusyAction(true);
                  InstallCollection(activeCollection.name)
                    .then((r) => {
                      setToastMessage(
                        `Ready to use in ${r.clients.join(", ")}`,
                      );
                      load();
                    })
                    .catch((e) => setToastMessage(String(e)))
                    .finally(() => setBusyAction(false));
                }}
                className="rounded-md bg-accent px-2.5 py-1 font-medium text-white transition hover:opacity-90 disabled:opacity-50"
              >
                {busyAction ? "Setting up…" : "Use all in my AI tools"}
              </button>
              <button
                disabled={
                  busyAction || (activeCollection.assets ?? []).length === 0
                }
                onClick={() => setShareCollection(activeCollection.name)}
                title="Share every asset in this collection with a team"
                className="rounded-md border border-line bg-surface px-2.5 py-1 font-medium text-ink-soft transition hover:border-accent hover:text-ink disabled:opacity-50"
              >
                Share…
              </button>
              <button
                disabled={busyAction}
                onClick={() => {
                  DeleteCollection(activeCollection.name)
                    .then(() => {
                      setScope({ kind: "all" });
                      load();
                      setToastMessage(
                        "Collection removed — its assets are still in the library",
                      );
                    })
                    .catch((e) => setToastMessage(String(e)));
                }}
                className="rounded-md px-2 py-1 font-medium text-ink-faint transition hover:text-danger"
              >
                Delete
              </button>
            </div>
          )}

          {activeTeam && (
            <div
              className="flex items-center gap-3 border-t border-line bg-accent-soft/50 px-5 py-2 text-xs"
              style={{ ["--wails-draggable" as never]: "no-drag" }}
            >
              <span className="text-ink-soft">
                {(activeTeam.members ?? []).length}{" "}
                {(activeTeam.members ?? []).length === 1 ? "member" : "members"}{" "}
                · assets shared with this team install automatically for its
                members
              </span>
              <div className="flex-1" />
              <button
                onClick={() => setOpenTeam(activeTeam)}
                className="rounded-md border border-line bg-surface px-2.5 py-1 font-medium text-ink-soft transition hover:border-accent hover:text-ink"
              >
                Manage team…
              </button>
            </div>
          )}
        </header>

        {/* Content */}
        <main className="flex-1 overflow-y-auto">
          {error && (
            <div className="m-5 rounded-lg bg-danger-soft px-4 py-3 text-sm text-danger">
              {error}{" "}
              <button className="underline" onClick={load}>
                Try again
              </button>
            </div>
          )}

          {assets === null ? (
            <ListSkeleton />
          ) : nothingToShow ? (
            <EmptyState
              scope={scope}
              hasAssets={(assets ?? []).length + drafts.length > 0}
            />
          ) : view === "list" ? (
            <div className="px-3 py-2">
              {teamCollections.map((c) => (
                <SharedCollectionRow
                  key={"col-" + c.name}
                  collection={c}
                  badgeWidth={badgeWidth}
                  onClick={() => setScope({ kind: "collection", name: c.name })}
                />
              ))}
              {visibleDrafts.map((d) => (
                <DraftRow
                  key={"draft-" + d.id}
                  draft={d}
                  onClick={() => void openExistingDraft(d.id)}
                />
              ))}
              {shown.map((a) => (
                <AssetRow
                  key={a.name}
                  asset={a}
                  badgeWidth={badgeWidth}
                  installed={installed.has(a.name)}
                  onClick={() => {
                    if (dragHappenedRef.current) {
                      dragHappenedRef.current = false;
                      return;
                    }
                    setSelected(a.name);
                  }}
                  onDragHandle={(name, e) => {
                    if (e.button !== 0) return;
                    dragHappenedRef.current = false;
                    pendingDragRef.current = {
                      kind: "asset",
                      name,
                      x: e.clientX,
                      y: e.clientY,
                    };
                  }}
                />
              ))}
              {visible.length > shown.length && (
                <button
                  onClick={() => setRenderCap((c) => c + RENDER_CAP)}
                  className="mx-3 my-2 rounded-lg border border-line px-3 py-2 text-sm text-ink-soft transition hover:border-accent hover:text-ink"
                >
                  Show {Math.min(RENDER_CAP, visible.length - shown.length)}{" "}
                  more ({visible.length - shown.length} remaining)
                </button>
              )}
            </div>
          ) : (
            <div className="grid grid-cols-[repeat(auto-fill,minmax(240px,1fr))] gap-3 p-5">
              {teamCollections.map((c) => (
                <button
                  key={"col-" + c.name}
                  onClick={() => setScope({ kind: "collection", name: c.name })}
                  className="rounded-xl border border-line bg-surface p-4 text-left transition hover:-translate-y-px hover:border-accent hover:shadow-sm"
                >
                  <div className="flex items-start justify-between gap-2">
                    <div className="truncate text-sm font-semibold">
                      {c.name}
                    </div>
                    <span className="shrink-0 rounded-full bg-accent-soft px-2 py-0.5 text-[11px] font-medium text-accent">
                      Collection
                    </span>
                  </div>
                  <div className="mt-1.5 line-clamp-2 min-h-10 text-sm text-ink-soft">
                    {c.description ||
                      `${(c.assets ?? []).length} assets, all shared with this team.`}
                  </div>
                </button>
              ))}
              {visibleDrafts.map((d) => (
                <button
                  key={"draft-" + d.id}
                  onClick={() => void openExistingDraft(d.id)}
                  className="rounded-xl border border-dashed border-amber-300 bg-surface p-4 text-left transition hover:-translate-y-px hover:shadow-sm dark:border-amber-700"
                >
                  <div className="flex items-start justify-between gap-2">
                    <div className="truncate text-sm font-semibold">
                      {d.name}
                    </div>
                    <span className="shrink-0 rounded-full bg-amber-50 px-2 py-0.5 text-[11px] font-medium text-amber-700 dark:bg-amber-950 dark:text-amber-300">
                      Draft
                    </span>
                  </div>
                  <div className="mt-1.5 line-clamp-2 min-h-10 text-sm text-ink-soft">
                    {d.description || "Not published yet."}
                  </div>
                </button>
              ))}
              {shown.map((a) => (
                <button
                  key={a.name}
                  onClick={() => setSelected(a.name)}
                  className="rounded-xl border border-line bg-surface p-4 text-left transition hover:-translate-y-px hover:border-accent hover:shadow-sm"
                >
                  <div className="flex items-start justify-between gap-2">
                    <div className="truncate text-sm font-semibold">
                      {a.name}
                    </div>
                    <TypeBadge type={a.type} label={a.typeLabel} />
                  </div>
                  <div className="mt-1.5 line-clamp-2 min-h-10 text-sm text-ink-soft">
                    {a.description || "No description yet."}
                  </div>
                  <div className="mt-3 flex items-center gap-2 text-xs text-ink-faint">
                    {installed.has(a.name) && (
                      <span className="text-emerald-600 dark:text-emerald-400">
                        ✓ in your AI tools
                      </span>
                    )}
                    <span>
                      {a.updatedAt ? `updated ${timeAgo(a.updatedAt)}` : ""}
                    </span>
                  </div>
                </button>
              ))}
              {visible.length > shown.length && (
                <button
                  onClick={() => setRenderCap((c) => c + RENDER_CAP)}
                  className="rounded-xl border border-dashed border-line p-4 text-sm text-ink-soft transition hover:border-accent hover:text-ink"
                >
                  Show more ({visible.length - shown.length} remaining)
                </button>
              )}
            </div>
          )}
        </main>
      </div>

      {assetDrag && (
        <div
          className="pointer-events-none fixed z-50 -translate-y-1/2 rounded-full bg-accent px-3 py-1 text-xs font-medium text-white shadow-lg"
          style={{ left: assetDrag.x + 12, top: assetDrag.y }}
        >
          {assetDrag.name}
          {dropCollection || dropTeam ? ` → ${dropCollection || dropTeam}` : ""}
        </div>
      )}

      {dragging && (
        <div className="pointer-events-none fixed inset-0 z-30 flex items-center justify-center bg-accent-soft/80">
          <div className="rounded-2xl border-2 border-dashed border-accent bg-surface px-10 py-8 text-center shadow-lg">
            <div className="text-2xl">📥</div>
            <div className="mt-2 text-sm font-semibold">
              Drop to add to your library
            </div>
            <div className="mt-1 text-xs text-ink-soft">
              Markdown files, folders, or zips
            </div>
          </div>
        </div>
      )}

      {selected && (
        <AssetDetail
          name={selected}
          collections={collections}
          teams={teams}
          installed={installed.has(selected)}
          installedScopes={installedScopes.get(selected) ?? []}
          onClose={() => setSelected(null)}
          onEdit={() => void editAsset(selected)}
          onChanged={() => {
            load();
            setToastMessage("Restored — it's now the current revision");
          }}
          onToast={(m) => {
            setToastMessage(m);
            load();
          }}
          onCollectionsChanged={load}
        />
      )}

      {openDraft && (
        <DraftSheet
          draft={openDraft}
          onClose={() => {
            setOpenDraft(null);
            load();
          }}
          onPublished={(name) => {
            setOpenDraft(null);
            load();
            setToastMessage(`${name} published to your library`);
          }}
        />
      )}

      {showAddAsset && (
        <AddAssetModal
          onClose={() => setShowAddAsset(false)}
          onDraft={(draft) => {
            setShowAddAsset(false);
            setOpenDraft(draft);
            load();
          }}
          onError={setToastMessage}
        />
      )}

      {showCollectionModal && (
        <CollectionModal
          onClose={() => setShowCollectionModal(false)}
          onCreated={(name) => {
            setShowCollectionModal(false);
            ensurePinned("collections", name);
            setScope({ kind: "collection", name });
            load();
          }}
        />
      )}

      {shareCollection && (
        <ShareModal
          title={`Share ${shareCollection}`}
          teams={teams}
          getSharing={() => GetCollectionSharing(shareCollection)}
          setTeamShared={(team, shared) =>
            SetCollectionTeamSharing(shareCollection, team, shared)
          }
          shareEveryone={() => ShareCollectionWithEveryone(shareCollection)}
          onClose={() => setShareCollection("")}
          onChanged={load}
        />
      )}

      {browse === "collections" && (
        <BrowseModal
          title="All collections"
          items={collections.map((c) => {
            const count = (c.assets ?? []).length;
            return {
              name: c.name,
              count,
              countLabel: count === 1 ? "asset" : "assets",
            };
          })}
          pinned={shownCollectionPins}
          onTogglePin={(name) => togglePin("collections", name)}
          onSelect={(name) => {
            setBrowse("");
            setScope({ kind: "collection", name });
            setSelected(null);
          }}
          onCreate={() => {
            setBrowse("");
            setShowCollectionModal(true);
          }}
          createLabel="New collection"
          onClose={() => setBrowse("")}
        />
      )}

      {browse === "teams" && (
        <BrowseModal
          title="All teams"
          items={teams.map((t) => {
            const count = teamAssetCounts[t.name] ?? 0;
            return {
              name: t.name,
              count,
              countLabel: count === 1 ? "asset" : "assets",
            };
          })}
          pinned={shownTeamPins}
          onTogglePin={(name) => togglePin("teams", name)}
          onSelect={(name) => {
            setBrowse("");
            setScope({ kind: "team", name });
            setSelected(null);
          }}
          onCreate={() => {
            setBrowse("");
            setShowNewTeam(true);
          }}
          createLabel="New team"
          onClose={() => setBrowse("")}
        />
      )}

      {openTeam && (
        <TeamModal
          team={openTeam}
          onClose={() => setOpenTeam(null)}
          onChanged={load}
        />
      )}

      {showNewTeam && (
        <Modal title="New team" onClose={() => setShowNewTeam(false)}>
          <form
            onSubmit={(e) => {
              e.preventDefault();
              void createTeam();
            }}
          >
            <input
              autoFocus
              value={newTeamName}
              onChange={(e) => setNewTeamName(e.target.value)}
              placeholder="platform, marketing, data…"
              className="w-full rounded-lg border border-line bg-canvas px-3 py-2 text-sm outline-none focus:border-accent"
            />
            <p className="mt-2 text-xs text-ink-faint">
              You'll be its first member and admin. Share assets with the team
              from any asset's panel.
            </p>
            <div className="mt-4 flex justify-end gap-2">
              <button
                type="button"
                onClick={() => setShowNewTeam(false)}
                className="rounded-lg border border-line px-4 py-2 text-sm font-medium text-ink-soft transition hover:text-ink"
              >
                Cancel
              </button>
              <button
                type="submit"
                disabled={!newTeamName.trim()}
                className="rounded-lg bg-accent px-4 py-2 text-sm font-medium text-white transition hover:opacity-90 disabled:opacity-50"
              >
                Create
              </button>
            </div>
          </form>
        </Modal>
      )}

      {showSettings && (
        <SettingsModal
          onClose={() => setShowSettings(false)}
          onProfileChanged={() => {
            setShowSettings(false);
            setScope({ kind: "all" });
            onVaultChanged();
          }}
          onLibrariesChanged={() => onVaultChanged()}
        />
      )}

      {toast && (
        <div className="fixed bottom-6 left-1/2 z-50 -translate-x-1/2 rounded-full bg-ink px-5 py-2.5 text-sm font-medium text-canvas shadow-lg">
          {toast}
        </div>
      )}
    </div>
  );
}

function AssetRow({
  asset,
  installed,
  badgeWidth,
  onClick,
  onDragHandle,
}: {
  asset: main.AssetCard;
  installed: boolean;
  badgeWidth: string;
  onClick: () => void;
  onDragHandle: (name: string, e: React.MouseEvent) => void;
}) {
  return (
    <button
      onClick={onClick}
      onMouseDown={(e) => onDragHandle(asset.name, e)}
      title="Drag onto a collection in the sidebar to add it"
      className="group flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left transition hover:bg-surface"
    >
      <span className={`flex shrink-0 ${badgeWidth}`}>
        <TypeBadge type={asset.type} label={asset.typeLabel} />
      </span>
      <span className="w-52 shrink-0 truncate text-sm font-medium">
        {asset.name}
      </span>
      <span className="min-w-0 flex-1 truncate text-sm text-ink-soft">
        {asset.description || "No description yet."}
      </span>
      {installed && (
        <span
          className="shrink-0 text-xs text-emerald-600 dark:text-emerald-400"
          title="Installed in your AI tools"
        >
          ✓
        </span>
      )}
      <span className="w-24 shrink-0 text-right text-xs text-ink-faint">
        {asset.updatedAt ? timeAgo(asset.updatedAt) : ""}
      </span>
    </button>
  );
}

function SharedCollectionRow({
  collection,
  badgeWidth,
  onClick,
}: {
  collection: main.Collection;
  badgeWidth: string;
  onClick: () => void;
}) {
  const count = (collection.assets ?? []).length;
  return (
    <button
      onClick={onClick}
      className="flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left transition hover:bg-surface"
    >
      <span className={`flex shrink-0 ${badgeWidth}`}>
        <span className="shrink-0 rounded-full bg-accent-soft px-2 py-0.5 text-[11px] font-medium text-accent">
          Collection
        </span>
      </span>
      <span className="w-52 shrink-0 truncate text-sm font-medium">
        {collection.name}
      </span>
      <span className="min-w-0 flex-1 truncate text-sm text-ink-soft">
        {collection.description ||
          `All ${count} assets are shared with this team.`}
      </span>
      <span className="w-24 shrink-0 text-right text-xs text-ink-faint">
        {count} {count === 1 ? "asset" : "assets"}
      </span>
    </button>
  );
}

function DraftRow({
  draft,
  onClick,
}: {
  draft: main.Draft;
  onClick: () => void;
}) {
  return (
    <button
      onClick={onClick}
      className="flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left transition hover:bg-surface"
    >
      <span className="flex w-14 shrink-0">
        <span className="shrink-0 rounded-full bg-amber-50 px-2 py-0.5 text-[11px] font-medium text-amber-700 dark:bg-amber-950 dark:text-amber-300">
          Draft
        </span>
      </span>
      <span className="w-52 shrink-0 truncate text-sm font-medium">
        {draft.name}
      </span>
      <span className="min-w-0 flex-1 truncate text-sm text-ink-soft">
        {draft.targetAsset
          ? `Unpublished changes to ${draft.targetAsset}`
          : draft.description || "Not published yet."}
      </span>
    </button>
  );
}

function ViewToggle({
  label,
  active,
  onClick,
}: {
  label: string;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button
      onClick={onClick}
      className={`px-2.5 py-1.5 text-xs font-medium transition ${
        active ? "bg-accent-soft text-accent" : "text-ink-faint hover:text-ink"
      }`}
    >
      {label}
    </button>
  );
}

function MenuItem({
  label,
  hint,
  onClick,
}: {
  label: string;
  hint?: string;
  onClick: () => void;
}) {
  return (
    <button
      onClick={onClick}
      className="flex w-full items-baseline gap-2 px-3.5 py-2 text-left text-sm transition hover:bg-accent-soft"
    >
      <span className="whitespace-nowrap font-medium">{label}</span>
      {hint && (
        <span className="min-w-0 flex-1 truncate text-right text-xs text-ink-faint">
          {hint}
        </span>
      )}
    </button>
  );
}

function EmptyState({
  scope,
  hasAssets,
}: {
  scope: Scope;
  hasAssets: boolean;
}) {
  if (scope.kind === "installed") {
    return (
      <Centered>
        <div className="text-sm font-medium">Nothing installed yet</div>
        <div className="mt-1 max-w-sm text-sm text-ink-faint">
          Open any asset and choose{" "}
          <span className="font-medium text-ink-soft">Use in my AI tools</span>{" "}
          — it lands in the AI tools shown above.
        </div>
      </Centered>
    );
  }
  if (scope.kind === "collection") {
    return (
      <Centered>
        <div className="text-sm font-medium">This collection is empty</div>
        <div className="mt-1 max-w-sm text-sm text-ink-faint">
          Open an asset and tap this collection's name to add it.
        </div>
      </Centered>
    );
  }
  if (scope.kind === "team") {
    return (
      <Centered>
        <div className="text-sm font-medium">
          Nothing shared with this team yet
        </div>
        <div className="mt-1 max-w-sm text-sm text-ink-faint">
          Open an asset and use{" "}
          <span className="font-medium text-ink-soft">Share…</span> to send it
          to {scope.name} — it installs automatically for every member.
        </div>
      </Centered>
    );
  }
  if (hasAssets) {
    return (
      <Centered>
        <div className="text-sm font-medium text-ink-soft">Nothing matches</div>
        <div className="mt-1 text-sm text-ink-faint">
          Try a different search, or another section in the sidebar.
        </div>
      </Centered>
    );
  }
  return (
    <Centered>
      <div className="mb-3 text-3xl">📚</div>
      <div className="text-sm font-medium">Your library is empty</div>
      <div className="mt-1 max-w-sm text-sm text-ink-faint">
        Drop a markdown file, folder, or zip anywhere in this window — or use
        the <span className="font-medium text-ink-soft">+ New</span> button — to
        add your first asset.
      </div>
    </Centered>
  );
}

function Centered({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex h-64 flex-col items-center justify-center text-center">
      {children}
    </div>
  );
}

function ListSkeleton() {
  return (
    <div className="space-y-1 px-3 py-2">
      {Array.from({ length: 10 }).map((_, i) => (
        <div key={i} className="h-9 animate-pulse rounded-lg bg-surface" />
      ))}
    </div>
  );
}

function timeAgo(iso: string): string {
  const then = new Date(iso).getTime();
  const mins = Math.max(0, Math.round((Date.now() - then) / 60000));
  if (mins < 1) return "just now";
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.round(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.round(hours / 24);
  if (days < 30) return `${days}d ago`;
  const months = Math.round(days / 30);
  return months < 12 ? `${months}mo ago` : `${Math.round(months / 12)}y ago`;
}
