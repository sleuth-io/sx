import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  AddAssetInstallation,
  AddAssetRepoScope,
  AddCollectionInstallation,
  AddCollectionRepoScope,
  CreateDraftFromAsset,
  CreateDraftFromPaths,
  CreateTeam,
  DeleteAssets,
  DeleteCollection,
  DeleteTeam,
  GetAssetInstallations,
  GetCollectionInstallations,
  GetDraft,
  InstalledAssets,
  ListAIClients,
  ListAssets,
  ListCollections,
  ListDrafts,
  ListTeams,
  PersonalAssets,
  RemoveAssetInstallationRow,
  RemoveCollectionInstallationRow,
  RenameCollection,
  RenameTeam,
  RepoAssets,
  SearchAssetContent,
  SetAssetTeamSharing,
  SetCollectionMembershipBulk,
  SetCollectionTeamSharing,
  SetTeamRepository,
  SyncAITools,
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
import Sidebar, { repoLabel, Scope } from "../components/Sidebar";
import CommandPalette from "../components/CommandPalette";
import Dashboard from "../components/Dashboard";
import PluginMount from "../components/PluginMount";
import { bootExtensions, syncVaultExtensions } from "../plugins/boot";
import { useSlot } from "../plugins/registry";
import { emitEvent } from "../plugins/events";
import { setPluginUIHandlers } from "../plugins/sxapi";
import type {
  CollectionViewSpec,
  CommandSpec,
  ViewMount,
} from "../plugins/api";
import usePanelSize from "../lib/usePanelSize";
import TeamModal from "../components/TeamModal";
import TypeBadge from "../components/TypeBadge";

type ViewMode = "list" | "grid";
type SortMode = "updated" | "name";
type PinKind = "collections" | "teams" | "repos";

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
  // Assets installed just for this user — drives the sidebar's
  // conditional "My skills" row and its scope filter.
  const [personalAssets, setPersonalAssets] = useState<string[]>([]);
  // Repo URL → asset names; null when this library doesn't track repos.
  const [repoAssets, setRepoAssets] = useState<Record<string, string[]> | null>(
    null,
  );
  const [openTeam, setOpenTeam] = useState<main.TeamInfo | null>(null);
  const [showNewTeam, setShowNewTeam] = useState(false);
  const [newTeamName, setNewTeamName] = useState("");
  const [browse, setBrowse] = useState<"" | "collections" | "teams" | "repos">(
    "",
  );
  const [shareCollection, setShareCollection] = useState("");
  // Pins are per-library. null = the user never pinned anything, so the
  // sidebar defaults to the first few; once they pin/unpin we persist.
  const [pinnedCollections, setPinnedCollections] = useState<string[] | null>(
    () => readPins("collections", vault.location),
  );
  const [pinnedTeams, setPinnedTeams] = useState<string[] | null>(() =>
    readPins("teams", vault.location),
  );
  const [pinnedRepos, setPinnedRepos] = useState<string[] | null>(() =>
    readPins("repos", vault.location),
  );
  const [typeFilter, setTypeFilter] = useState("");
  const [installedInfo, setInstalledInfo] = useState<main.InstalledAssetInfo[]>(
    [],
  );
  const [aiClients, setAiClients] = useState<main.AIClient[]>([]);
  const [error, setError] = useState("");
  const [query, setQuery] = useState("");
  // Content matches from the vault-side full-text search — the main
  // search box finds text INSIDE assets, not just names/descriptions.
  const [contentHits, setContentHits] = useState<Map<
    string,
    main.ContentMatch
  > | null>(null);
  const [contentSearching, setContentSearching] = useState(false);
  const contentSeq = useRef(0);
  useEffect(() => {
    const q = query.trim();
    const seq = ++contentSeq.current;
    if (q.length < 3) {
      setContentHits(null);
      setContentSearching(false);
      return;
    }
    setContentSearching(true);
    const timer = setTimeout(() => {
      SearchAssetContent(q)
        .then((matches) => {
          if (seq !== contentSeq.current) return;
          setContentHits(new Map((matches ?? []).map((m) => [m.name, m])));
        })
        .catch(() => {
          if (seq === contentSeq.current) setContentHits(null);
        })
        .finally(() => {
          if (seq === contentSeq.current) setContentSearching(false);
        });
    }, 350);
    return () => clearTimeout(timer);
  }, [query]);
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
  const [newSubOpen, setNewSubOpen] = useState(false);
  const newMenuRef = useRef<HTMLDivElement>(null);
  const [sortMenuOpen, setSortMenuOpen] = useState(false);
  const sortMenuRef = useRef<HTMLDivElement>(null);
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

  // In-app drags: asset rows → collections, teams, or repos; collection
  // rows → teams or repos; repo rows → teams (adds the repo to the
  // team's repositories). Pointer-based, NOT HTML5 drag-and-drop: the
  // native webview's file-drop handling swallows HTML5 drop events, so
  // rows track the mouse and drops hit-test [data-drop-*] targets.
  type DragKind = "asset" | "collection" | "repo";
  const [assetDrag, setAssetDrag] = useState<{
    name: string;
    x: number;
    y: number;
  } | null>(null);
  const [dropCollection, setDropCollection] = useState("");
  const [dropTeam, setDropTeam] = useState("");
  const [dropRepo, setDropRepo] = useState("");
  // names carries a multi-selection when the dragged row is part of one;
  // label overrides name in the ghost chip when the payload isn't
  // presentable as-is (a repo drag carries the full URL in name).
  const pendingDragRef = useRef<{
    kind: DragKind;
    name: string;
    names?: string[];
    label?: string;
    x: number;
    y: number;
  } | null>(null);
  const dragRef = useRef<{
    kind: DragKind;
    name: string;
    names?: string[];
    label?: string;
  } | null>(null);
  const dropCollectionRef = useRef("");
  const dropTeamRef = useRef("");
  const dropRepoRef = useRef("");
  const dragHappenedRef = useRef(false);
  const [toast, setToast] = useState("");
  const [busyAction, setBusyAction] = useState(false);
  const [syncing, setSyncing] = useState(false);

  // Multi-selection (OS conventions: click, shift-range, cmd/ctrl-toggle,
  // background marquee). Plain click still opens the detail panel; the
  // modified clicks only adjust the selection.
  const [multiSel, setMultiSel] = useState<Set<string>>(new Set());
  const selAnchorRef = useRef<string | null>(null);
  const [ctxMenu, setCtxMenu] = useState<{ x: number; y: number } | null>(null);
  const [bulkShare, setBulkShare] = useState(false);
  const [confirmBulkDelete, setConfirmBulkDelete] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [marquee, setMarquee] = useState<{
    x0: number;
    y0: number;
    x1: number;
    y1: number;
  } | null>(null);

  // Promise-based confirmation for bulk mutations: the caller awaits the
  // user's answer, so gesture-triggered bulk actions (drops) and the bulk
  // share dialog can pause before touching N assets.
  const [confirmState, setConfirmState] = useState<{
    message: string;
    action: string;
    resolve: (ok: boolean) => void;
  } | null>(null);
  const confirmAction = useCallback(
    (message: string, action: string): Promise<boolean> =>
      new Promise((resolve) => setConfirmState({ message, action, resolve })),
    [],
  );
  function answerConfirm(ok: boolean) {
    confirmState?.resolve(ok);
    setConfirmState(null);
  }

  function handleRowClick(name: string, e: React.MouseEvent, order: string[]) {
    if (dragHappenedRef.current) {
      dragHappenedRef.current = false;
      return;
    }
    if (e.shiftKey && selAnchorRef.current) {
      const i = order.indexOf(selAnchorRef.current);
      const j = order.indexOf(name);
      if (i >= 0 && j >= 0) {
        const [lo, hi] = i < j ? [i, j] : [j, i];
        setMultiSel(new Set(order.slice(lo, hi + 1)));
        return;
      }
    }
    if (e.metaKey || e.ctrlKey) {
      setMultiSel((prev) => {
        const next = new Set(prev);
        if (next.has(name)) next.delete(name);
        else next.add(name);
        return next;
      });
      selAnchorRef.current = name;
      return;
    }
    selAnchorRef.current = name;
    setMultiSel(new Set([name]));
    setSelected(name);
  }

  function handleRowContextMenu(name: string, e: React.MouseEvent) {
    e.preventDefault();
    if (!multiSel.has(name)) {
      setMultiSel(new Set([name]));
      selAnchorRef.current = name;
    }
    setCtxMenu({ x: e.clientX, y: e.clientY });
  }

  // Dragging a row that belongs to the selection drags the whole
  // selection; dragging an unselected row drags just that row.
  function startAssetDrag(name: string, e: React.MouseEvent) {
    if (e.button !== 0) return;
    dragHappenedRef.current = false;
    const names =
      multiSel.has(name) && multiSel.size > 1 ? [...multiSel] : undefined;
    pendingDragRef.current = {
      kind: "asset",
      name: names ? `${names.length} skills` : name,
      names,
      x: e.clientX,
      y: e.clientY,
    };
  }

  // Marquee selection: press on empty list background and drag a
  // rectangle; rows it touches become the selection.
  function startMarquee(e: React.MouseEvent) {
    if (e.button !== 0) return;
    if ((e.target as Element).closest("[data-asset-row], button, input")) {
      return;
    }
    const x0 = e.clientX;
    const y0 = e.clientY;
    let moved = false;
    const onMove = (ev: MouseEvent) => {
      if (!moved && Math.hypot(ev.clientX - x0, ev.clientY - y0) < 5) return;
      moved = true;
      const rect = {
        left: Math.min(x0, ev.clientX),
        right: Math.max(x0, ev.clientX),
        top: Math.min(y0, ev.clientY),
        bottom: Math.max(y0, ev.clientY),
      };
      setMarquee({ x0, y0, x1: ev.clientX, y1: ev.clientY });
      const hit = new Set<string>();
      document.querySelectorAll("[data-asset-row]").forEach((el) => {
        const b = el.getBoundingClientRect();
        if (
          b.left < rect.right &&
          b.right > rect.left &&
          b.top < rect.bottom &&
          b.bottom > rect.top
        ) {
          hit.add(el.getAttribute("data-asset-row") ?? "");
        }
      });
      hit.delete("");
      setMultiSel(hit);
    };
    const onUp = () => {
      window.removeEventListener("mousemove", onMove);
      window.removeEventListener("mouseup", onUp);
      setMarquee(null);
      // A plain background click (no drag) clears the selection.
      if (!moved) setMultiSel(new Set());
    };
    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
  }

  // Renaming a collection or team: one small modal for both.
  const [renameTarget, setRenameTarget] = useState<{
    kind: "collection" | "team";
    name: string;
  } | null>(null);
  const [renameValue, setRenameValue] = useState("");

  function startRename(kind: "collection" | "team", name: string) {
    setRenameTarget({ kind, name });
    setRenameValue(name);
  }

  async function confirmRename() {
    if (!renameTarget) return;
    const next = renameValue.trim();
    if (!next || next === renameTarget.name) {
      setRenameTarget(null);
      return;
    }
    setBusyAction(true);
    try {
      if (renameTarget.kind === "collection") {
        await RenameCollection(renameTarget.name, next);
        // Pins and the current view follow the rename.
        if (shownCollectionPins.includes(renameTarget.name)) {
          setPins(
            "collections",
            shownCollectionPins.map((n) =>
              n === renameTarget.name ? next : n,
            ),
          );
        }
        setScope({ kind: "collection", name: next });
      } else {
        await RenameTeam(renameTarget.name, next);
        if (shownTeamPins.includes(renameTarget.name)) {
          setPins(
            "teams",
            shownTeamPins.map((n) => (n === renameTarget.name ? next : n)),
          );
        }
        setScope({ kind: "team", name: next });
      }
      setRenameTarget(null);
      load();
    } catch (e) {
      setToastMessage(String(e));
    } finally {
      setBusyAction(false);
    }
  }

  // The library-level equivalent of `sx install`.
  async function syncAITools() {
    setSyncing(true);
    try {
      const summary = await SyncAITools();
      setToastMessage(summary);
      emitEvent("vault-synced", {});
      load();
    } catch (e) {
      setToastMessage(String(e));
    } finally {
      setSyncing(false);
    }
  }

  // Guard against overlapping loads resolving out of order (focus events,
  // post-mutation refreshes): only the newest generation's results apply.
  // Failures follow stale-while-error semantics: a background refresh that
  // errors keeps the last good data on screen instead of blanking it —
  // only the very first load falls back to empty (with the error shown).
  const loadGen = useRef(0);
  const loadedOnce = useRef(false);
  const load = useCallback(() => {
    const gen = ++loadGen.current;
    const apply =
      <T,>(setter: (v: T) => void) =>
      (v: T) => {
        if (gen === loadGen.current) setter(v);
      };
    const applyFallback =
      <T,>(setter: (v: T) => void, fallback: T) =>
      () => {
        if (gen === loadGen.current && !loadedOnce.current) setter(fallback);
      };
    setError("");
    ListAssets()
      .then((v) => {
        if (gen !== loadGen.current) return;
        loadedOnce.current = true;
        // App extensions ride the asset pipeline but are NOT AI assets:
        // they surface only in the Extensions screen, never in the
        // library views (docs/app-plugins-spec.md, strict UI separation).
        setAssets((v ?? []).filter((a) => a.type !== "app-plugin"));
      })
      .catch((e) => {
        if (gen !== loadGen.current) return;
        if (!loadedOnce.current) {
          setError(String(e));
          setAssets([]);
        }
      });
    ListDrafts().then(apply(setDrafts)).catch(applyFallback(setDrafts, []));
    ListCollections()
      .then(apply(setCollections))
      .catch(applyFallback(setCollections, []));
    InstalledAssets()
      .then(apply(setInstalledInfo))
      .catch(applyFallback(setInstalledInfo, []));
    ListTeams().then(apply(setTeams)).catch(applyFallback(setTeams, []));
    TeamAssets()
      .then((m) => apply(setTeamAssets)(m ?? {}))
      .catch(applyFallback(setTeamAssets, {}));
    PersonalAssets()
      .then((v) => apply(setPersonalAssets)(v ?? []))
      .catch(applyFallback(setPersonalAssets, []));
    if (vault.trackRepos) {
      RepoAssets()
        .then((m) => apply(setRepoAssets)(m ?? {}))
        .catch(applyFallback(setRepoAssets, {}));
    } else {
      apply(setRepoAssets)(null);
    }
  }, [vault.trackRepos]);

  useEffect(load, [load]);
  const loadRef = useRef(load);
  useEffect(() => {
    loadRef.current = load;
  }, [load]);

  // Extension system: boot once, and hand extensions the app's real UI
  // services (toast + confirm) so sx.ui works.
  const [paletteOpen, setPaletteOpen] = useState(false);
  useEffect(() => {
    setPluginUIHandlers({
      notice: setToastMessage,
      confirm: confirmAction,
      refresh: () => loadRef.current(),
      openAsset: setSelected,
      openView: (key) => setScope({ kind: "plugin-view", name: key }),
    });
    void bootExtensions();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  // The keydown and menu-accelerator paths can BOTH fire for one ⌘K
  // press on some platforms; the debounce makes that one toggle. The
  // header chip deliberately bypasses it (below) — a click is never a
  // duplicate delivery and must never be swallowed.
  const lastPaletteToggle = useRef(0);
  const togglePalette = useCallback(() => {
    const now = Date.now();
    if (now - lastPaletteToggle.current < 250) return;
    lastPaletteToggle.current = now;
    setPaletteOpen((v) => !v);
  }, []);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        togglePalette();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [togglePalette]);
  const pluginCommands = useSlot("command");
  const mainViews = useSlot("main-view");
  // Extension-contributed collection tabs (views:collection). "" is the
  // built-in Assets list; a missing key (extension disabled mid-view)
  // falls back to it. With no registrations the collection view renders
  // exactly as before — no tab row at all.
  const collectionViews = useSlot("collection-view");
  const [collectionTab, setCollectionTab] = useState("");
  useEffect(() => setCollectionTab(""), [scope]);
  const currentCollectionView =
    scope.kind === "collection"
      ? collectionViews.find(
          (v) => v.pluginId + ":" + v.spec.id === collectionTab,
        )
      : undefined;
  const newMenuCommands = useMemo(
    () => pluginCommands.map((e) => e.spec).filter((c) => c.menu === "new"),
    [pluginCommands],
  );
  const coreCommands = useMemo<CommandSpec[]>(
    () => [
      { id: "core-new-skill", title: "New skill…", run: () => setShowAddAsset(true) },
      {
        id: "core-new-collection",
        title: "New collection…",
        run: () => setShowCollectionModal(true),
      },
      { id: "core-sync", title: "Sync AI tools", run: () => syncAITools() },
      {
        id: "core-dashboard",
        title: "Open dashboard",
        run: () => setScope({ kind: "dashboard" }),
      },
      {
        id: "core-settings",
        title: "Open settings…",
        run: () => setShowSettings(true),
      },
    ],
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [],
  );
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
      // Select all visible skills (OS convention); Escape clears.
      if (!inField && (e.metaKey || e.ctrlKey) && e.key === "a") {
        e.preventDefault();
        setMultiSel(
          new Set(
            document.body.querySelectorAll("[data-asset-row]").length
              ? [...document.body.querySelectorAll("[data-asset-row]")].map(
                  (el) => el.getAttribute("data-asset-row") ?? "",
                )
              : [],
          ),
        );
      }
      if (e.key === "Escape") {
        setCtxMenu(null);
        setMultiSel((prev) => (prev.size > 0 ? new Set() : prev));
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
  // Assets can drop on collections, teams, or repos; collections on teams
  // and repos; repos on teams.
  useEffect(() => {
    const finish = () => {
      pendingDragRef.current = null;
      dragRef.current = null;
      dropCollectionRef.current = "";
      dropTeamRef.current = "";
      dropRepoRef.current = "";
      setAssetDrag(null);
      setDropCollection("");
      setDropTeam("");
      setDropRepo("");
      document.body.style.userSelect = "";
      document.body.style.cursor = "";
    };
    const onMove = (e: MouseEvent) => {
      const pending = pendingDragRef.current;
      if (!dragRef.current) {
        if (!pending) return;
        if (Math.hypot(e.clientX - pending.x, e.clientY - pending.y) < 6)
          return;
        dragRef.current = {
          kind: pending.kind,
          name: pending.name,
          names: pending.names,
          label: pending.label,
        };
        dragHappenedRef.current = true;
        document.body.style.userSelect = "none";
        document.body.style.cursor = "grabbing";
      }
      const drag = dragRef.current;
      setAssetDrag({
        name: drag.label ?? drag.name,
        x: e.clientX,
        y: e.clientY,
      });
      const selector =
        drag.kind === "asset"
          ? "[data-drop-collection], [data-drop-team], [data-drop-repo]"
          : drag.kind === "collection"
            ? "[data-drop-team], [data-drop-repo]"
            : "[data-drop-team]";
      const hit = document
        .elementFromPoint(e.clientX, e.clientY)
        ?.closest(selector);
      // A collection must not highlight as a drop target for itself.
      const collection = hit?.getAttribute("data-drop-collection") ?? "";
      const team = hit?.getAttribute("data-drop-team") ?? "";
      const repo = hit?.getAttribute("data-drop-repo") ?? "";
      dropCollectionRef.current = collection;
      dropTeamRef.current = team;
      dropRepoRef.current = repo;
      setDropCollection(collection);
      setDropTeam(team);
      setDropRepo(repo);
    };
    const onUp = () => {
      const drag = dragRef.current;
      const collection = dropCollectionRef.current;
      const team = dropTeamRef.current;
      const repo = dropRepoRef.current;
      finish();
      // The click event (if any) fires synchronously after mouseup; clear
      // the suppress flag right after so the NEXT click isn't swallowed
      // when a drag ends off its origin row and no click fires at all.
      setTimeout(() => {
        dragHappenedRef.current = false;
      }, 0);
      if (!drag) return;
      // The dragged unit may be a multi-selection: apply the drop action
      // to every dragged asset and report once. An `applyAll` batch runner
      // replaces the per-name loop when the backend can take the whole
      // batch in one transaction (one git commit instead of N).
      const dropAssets = (
        each: ((name: string) => Promise<void>) | null,
        message: (label: string) => string,
        ask: (label: string) => string,
        applyAll?: (names: string[]) => Promise<void>,
      ) => {
        const names = drag.names ?? [drag.name];
        // Sequential on purpose when looping: some mutations
        // read-modify-write shared state, and parallel writes lose
        // updates. Git vaults commit+push per mutation, so larger batches
        // get a running count instead of looking hung.
        void (async () => {
          // A drop that mutates several assets confirms first — a drag is
          // too easy a gesture to change N things silently.
          if (names.length > 1) {
            const ok = await confirmAction(
              `${ask(`${names.length} skills`)}?`,
              "Apply",
            );
            if (!ok) return;
          }
          const label =
            names.length === 1 ? names[0] : `${names.length} skills`;
          if (applyAll) {
            try {
              await applyAll(names);
            } catch (e) {
              setToastMessage(String(e));
              return;
            }
            load();
            setToastMessage(message(label));
            return;
          }
          let failed = 0;
          for (const [i, n] of names.entries()) {
            if (names.length > 3) {
              setToastMessage(`Working… ${i + 1}/${names.length}`);
            }
            try {
              await each!(n);
            } catch {
              failed++;
            }
          }
          load();
          setToastMessage(
            failed === 0
              ? message(label)
              : `${message(label)} — ${failed} failed`,
          );
        })();
      };
      if (drag.kind === "asset" && collection) {
        dropAssets(
          null,
          (label) => `Added ${label} to ${collection}`,
          (label) => `Add ${label} to ${collection}`,
          (names) => SetCollectionMembershipBulk(collection, names, true),
        );
      } else if (drag.kind === "asset" && team) {
        dropAssets(
          (n) => SetAssetTeamSharing(n, team, true),
          (label) => `Shared ${label} with ${team}`,
          (label) => `Share ${label} with ${team}`,
        );
      } else if (drag.kind === "collection" && team) {
        SetCollectionTeamSharing(drag.name, team, true)
          .then(() => {
            load();
            setToastMessage(`Shared everything in ${drag.name} with ${team}`);
          })
          .catch((e) => setToastMessage(String(e)));
      } else if (drag.kind === "asset" && repo) {
        dropAssets(
          (n) => AddAssetRepoScope(n, repo),
          (label) => `${label} now installs in ${repoLabel(repo)}`,
          (label) => `Install ${label} in ${repoLabel(repo)}`,
        );
      } else if (drag.kind === "collection" && repo) {
        AddCollectionRepoScope(drag.name, repo)
          .then(() => {
            load();
            setToastMessage(
              `Everything in ${drag.name} now installs in ${repoLabel(repo)}`,
            );
          })
          .catch((e) => setToastMessage(String(e)));
      } else if (drag.kind === "repo" && team) {
        SetTeamRepository(team, drag.name, true)
          .then(() => {
            load();
            setToastMessage(
              `${team}'s assets now install in ${repoLabel(drag.name)}`,
            );
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

  useEffect(() => {
    if (!sortMenuOpen) return;
    const onClick = (e: MouseEvent) => {
      if (
        !(e.target instanceof Node) ||
        !sortMenuRef.current?.contains(e.target)
      )
        setSortMenuOpen(false);
    };
    window.addEventListener("mousedown", onClick);
    return () => window.removeEventListener("mousedown", onClick);
  }, [sortMenuOpen]);

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

  // A repo view can't outlive repo tracking being switched off.
  useEffect(() => {
    if (!vault.trackRepos && scope.kind === "repo") setScope({ kind: "all" });
  }, [vault.trackRepos, scope]);

  // Native menu → frontend views (Settings Cmd+,; File → New …)
  useEffect(() => {
    EventsOn("open-settings", () => setShowSettings(true));
    EventsOn("new-skill", () => setShowAddAsset(true));
    EventsOn("new-collection", () => setShowCollectionModal(true));
    EventsOn("new-library", () => setShowSettings(true));
    EventsOn("command-palette", togglePalette);
    return () =>
      EventsOff(
        "open-settings",
        "new-skill",
        "new-collection",
        "new-library",
        "command-palette",
      );
  }, [togglePalette]);

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

  // Repo URLs, sorted by their display label so the sidebar and browse
  // list read alphabetically.
  const repoUrls = useMemo(
    () =>
      Object.keys(repoAssets ?? {}).sort((a, b) =>
        repoLabel(a).localeCompare(repoLabel(b)),
      ),
    [repoAssets],
  );

  // Never-pinned libraries default to the first few so the sidebar isn't
  // empty; an explicit pin/unpin takes over from there.
  const shownCollectionPins =
    pinnedCollections ?? collections.slice(0, 5).map((c) => c.name);
  const shownTeamPins = pinnedTeams ?? teams.slice(0, 5).map((t) => t.name);
  const shownRepoPins = pinnedRepos ?? repoUrls.slice(0, 5);

  const teamAssetCounts = useMemo(() => {
    const counts: Record<string, number> = {};
    for (const t of teams) counts[t.name] = (teamAssets[t.name] ?? []).length;
    return counts;
  }, [teams, teamAssets]);

  const pinShown: Record<PinKind, string[]> = {
    collections: shownCollectionPins,
    teams: shownTeamPins,
    repos: shownRepoPins,
  };
  const pinSetters: Record<PinKind, (v: string[]) => void> = {
    collections: setPinnedCollections,
    teams: setPinnedTeams,
    repos: setPinnedRepos,
  };

  function setPins(kind: PinKind, next: string[]) {
    localStorage.setItem(
      `sx-pins-${kind}:${vault.location}`,
      JSON.stringify(next),
    );
    pinSetters[kind](next);
  }

  function togglePin(kind: PinKind, name: string) {
    const current = pinShown[kind];
    setPins(
      kind,
      current.includes(name)
        ? current.filter((n) => n !== name)
        : [...current, name],
    );
  }

  function ensurePinned(kind: PinKind, name: string) {
    const current = pinShown[kind];
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
        case "personal":
          if (!personalAssets.includes(a.name)) return false;
          break;
        case "repo":
          if (!(repoAssets?.[scope.name] ?? []).includes(a.name)) return false;
          break;
        case "all":
          break;
      }
      if (!q) return true;
      return (
        a.name.toLowerCase().includes(q) ||
        a.description.toLowerCase().includes(q) ||
        // Full-text hits inside the asset's markdown count too.
        (contentHits?.has(a.name) ?? false)
      );
    });
    return list.sort((a, b) => {
      if (sort === "name") return a.name.localeCompare(b.name);
      return (b.updatedAt || "").localeCompare(a.updatedAt || "");
    });
  }, [
    assets,
    query,
    contentHits,
    scope,
    installed,
    activeCollection,
    teamAssets,
    personalAssets,
    repoAssets,
    sort,
    typeFilter,
  ]);

  // Collections fully shared with the viewed team — every asset in the
  // collection is installed for the team, so the collection itself
  // belongs in the team's list.
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
      case "dashboard":
        return "Dashboard";
      case "plugin-view":
        return (
          mainViews.find((v) => v.pluginId + ":" + v.spec.id === scope.name)
            ?.spec.title ?? "View"
        );
      case "installed":
        return "In your AI tools";
      case "drafts":
        return "Drafts";
      case "collection":
        return scope.name;
      case "team":
        return scope.name;
      case "repo":
        return repoLabel(scope.name);
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
        personalCount={personalAssets.length}
        draftCount={drafts.length}
        collections={collections}
        teams={teams}
        teamAssetCounts={teamAssetCounts}
        repoAssets={vault.trackRepos ? (repoAssets ?? {}) : null}
        pinnedCollections={shownCollectionPins}
        pinnedTeams={shownTeamPins}
        pinnedRepos={shownRepoPins}
        onNewCollection={() => setShowCollectionModal(true)}
        onNewTeam={() => setShowNewTeam(true)}
        onBrowseCollections={() => setBrowse("collections")}
        onBrowseTeams={() => setBrowse("teams")}
        onBrowseRepos={() => setBrowse("repos")}
        onSettings={() => setShowSettings(true)}
        dropCollection={dropCollection}
        dropTeam={dropTeam}
        dropRepo={dropRepo}
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
        onRepoDragHandle={(url, e) => {
          if (e.button !== 0) return;
          dragHappenedRef.current = false;
          pendingDragRef.current = {
            kind: "repo",
            name: url,
            label: repoLabel(url),
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
          {/* flex-wrap: when the window is too narrow for one row, the
              controls drop to a second line instead of collapsing the
              title or clipping buttons off-screen. */}
          <div className="flex flex-wrap items-center gap-x-3 gap-y-2 px-5 pb-3 pt-9">
            <h1
              className="min-w-0 max-w-full truncate whitespace-nowrap text-sm font-semibold"
              title={scopeTitle}
            >
              {scopeTitle}
            </h1>
            <span className="shrink-0 text-xs text-ink-faint">
              {visible.length + visibleDrafts.length + teamCollections.length}
            </span>
            {scope.kind === "installed" && aiClients.length > 0 && (
              <span className="hidden min-w-0 truncate text-xs text-ink-faint xl:inline">
                · delivered to {aiClients.map((c) => c.name).join(", ")}
              </span>
            )}

            <div
              className="ml-auto flex h-9 shrink-0 items-center gap-2"
              style={{ ["--wails-draggable" as never]: "no-drag" }}
            >
              {/* The palette's visible front door: a hidden-only ⌘K is
                  never discovered (GitHub retired theirs over exactly
                  this). The chip is clickable AND teaches the shortcut. */}
              <button
                onClick={() => setPaletteOpen((v) => !v)}
                aria-label="Command palette"
                title="Command palette — every action, plus your extensions"
                className="flex h-full items-center rounded-lg border border-line px-2.5 font-mono text-[11px] text-ink-faint transition hover:border-accent hover:text-ink"
              >
                {navigator.platform.includes("Mac") ? "⌘K" : "Ctrl K"}
              </button>

              {/* Search, type filter, sort, and list/grid act on the
                  asset list — on full-page surfaces (dashboard, plugin
                  views) they'd be dead controls, so they hide. */}
              {scope.kind !== "dashboard" && scope.kind !== "plugin-view" && (
                <>
                <div className="relative h-full">
                  <input
                    ref={searchRef}
                    value={query}
                    onChange={(e) => setQuery(e.target.value)}
                    placeholder="Search…"
                    className="peer h-full w-36 rounded-lg border border-line bg-canvas px-3 pr-8 text-sm outline-none focus:border-accent lg:w-56"
                  />
                  {!query && (
                    <kbd className="pointer-events-none absolute right-2 top-1/2 -translate-y-1/2 rounded border border-line bg-surface px-1.5 py-0.5 font-mono text-[10px] text-ink-faint peer-focus:hidden">
                      /
                    </kbd>
                  )}
                  {contentSearching && (
                    <span
                      className="pointer-events-none absolute right-2.5 top-1/2 h-2 w-2 -translate-y-1/2 animate-pulse rounded-full bg-accent"
                      title="Searching inside assets…"
                    />
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

                {/* Sort is a view preference, not a filter — it lives in a
                    quiet icon menu (the Linear "display options" pattern)
                    instead of crowding the filter row. */}
                <div className="relative h-full" ref={sortMenuRef}>
                  <button
                    onClick={() => setSortMenuOpen((v) => !v)}
                    title={`Sort: ${sort === "name" ? "Name" : "Recently updated"}`}
                    aria-label="Sort"
                    aria-expanded={sortMenuOpen}
                    className={`flex h-full items-center rounded-lg px-2 transition hover:bg-canvas hover:text-ink ${
                      sortMenuOpen ? "bg-canvas text-ink" : "text-ink-faint"
                    }`}
                  >
                    <svg
                      aria-hidden="true"
                      className="h-4 w-4"
                      viewBox="0 0 16 16"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="1.5"
                      strokeLinecap="round"
                      strokeLinejoin="round"
                    >
                      <path d="M5 3v10M5 13l-2.5-2.5M5 13l2.5-2.5M11 13V3M11 3 8.5 5.5M11 3l2.5 2.5" />
                    </svg>
                  </button>
                  {sortMenuOpen && (
                    <div className="absolute right-0 z-40 mt-1.5 w-48 overflow-hidden rounded-xl border border-line bg-surface py-1 shadow-xl">
                      <div className="px-3 pb-1 pt-1.5 text-[11px] font-semibold tracking-wide text-ink-faint">
                        SORT BY
                      </div>
                      {(
                        [
                          ["updated", "Recently updated"],
                          ["name", "Name"],
                        ] as [SortMode, string][]
                      ).map(([value, label]) => (
                        <button
                          key={value}
                          onClick={() => {
                            setSort(value);
                            setSortMenuOpen(false);
                          }}
                          className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm text-ink-soft transition hover:bg-canvas hover:text-ink"
                        >
                          <span className="w-3.5 text-accent">
                            {sort === value ? "✓" : ""}
                          </span>
                          {label}
                        </button>
                      ))}
                    </div>
                  )}
                </div>

                {/* List/grid: icon-only segments — one of the few controls
                    where icons alone are unambiguous. */}
                <div className="flex h-full items-center overflow-hidden rounded-lg border border-line">
                  <button
                    onClick={() => setView("list")}
                    title="List view"
                    aria-label="List view"
                    aria-pressed={view === "list"}
                    className={`flex h-full items-center px-2 transition ${
                      view === "list"
                        ? "bg-canvas text-ink"
                        : "text-ink-faint hover:text-ink"
                    }`}
                  >
                    <svg
                      aria-hidden="true"
                      className="h-3.5 w-3.5"
                      viewBox="0 0 16 16"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="1.5"
                      strokeLinecap="round"
                    >
                      <path d="M2.5 4h11M2.5 8h11M2.5 12h11" />
                    </svg>
                  </button>
                  <button
                    onClick={() => setView("grid")}
                    title="Grid view"
                    aria-label="Grid view"
                    aria-pressed={view === "grid"}
                    className={`flex h-full items-center px-2 transition ${
                      view === "grid"
                        ? "bg-canvas text-ink"
                        : "text-ink-faint hover:text-ink"
                    }`}
                  >
                    <svg
                      aria-hidden="true"
                      className="h-3.5 w-3.5"
                      viewBox="0 0 16 16"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="1.5"
                      strokeLinejoin="round"
                    >
                      <rect x="2.5" y="2.5" width="4.5" height="4.5" rx="1" />
                      <rect x="9" y="2.5" width="4.5" height="4.5" rx="1" />
                      <rect x="2.5" y="9" width="4.5" height="4.5" rx="1" />
                      <rect x="9" y="9" width="4.5" height="4.5" rx="1" />
                    </svg>
                  </button>
                </div>
              </>
              )}

              {/* The library-level `sx install`: deliver everything scoped
                  to this machine into the AI tools, clean up what's stale. */}
              <button
                onClick={() => void syncAITools()}
                disabled={syncing}
                title="Sync your AI tools — install everything scoped to you (like sx install)"
                className="flex h-full items-center gap-1.5 rounded-lg border border-line px-3 text-sm font-medium text-ink-soft transition hover:border-accent hover:text-ink disabled:opacity-60"
              >
                <svg
                  aria-hidden="true"
                  className={`h-3.5 w-3.5 ${syncing ? "animate-spin" : ""}`}
                  viewBox="0 0 16 16"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="1.5"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                >
                  <path d="M13.5 8a5.5 5.5 0 0 1-9.6 3.7M2.5 8a5.5 5.5 0 0 1 9.6-3.7" />
                  <path d="M13.7 1.9v2.7H11M2.3 14.1v-2.7H5" />
                </svg>
                <span className="hidden lg:inline">
                  {syncing ? "Syncing…" : "Sync"}
                </span>
              </button>

              <div className="relative h-full" ref={newMenuRef}>
                <button
                  onClick={() => setNewMenuOpen((v) => !v)}
                  className="flex h-full items-center gap-1.5 rounded-lg bg-accent px-3.5 text-sm font-medium text-white transition hover:opacity-90"
                >
                  <span className="text-base leading-none">+</span> New
                  <span className="text-[10px] opacity-70">▾</span>
                </button>
                {newMenuOpen && (
                  <div className="absolute right-0 z-40 mt-1.5 w-64 overflow-visible rounded-xl border border-line bg-surface py-1 shadow-xl">
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
                    {/* Creation-shaped extension commands (menu: "new")
                        live under ONE flyout so they don't dilute the two
                        core actions. */}
                    {newMenuCommands.length > 0 && (
                      <div
                        className="relative"
                        onMouseEnter={() => setNewSubOpen(true)}
                        onMouseLeave={() => setNewSubOpen(false)}
                      >
                        <button
                          onClick={() => setNewSubOpen((v) => !v)}
                          className="flex w-full items-center gap-2 px-3.5 py-2 text-left text-sm transition hover:bg-accent-soft"
                        >
                          <span className="min-w-0 flex-1 truncate font-medium">
                            From extensions
                          </span>
                          <span className="shrink-0 text-xs text-ink-faint">
                            ▸
                          </span>
                        </button>
                        {newSubOpen && (
                          <div className="absolute right-full top-0 z-50 mr-1 w-72 overflow-hidden rounded-xl border border-line bg-surface py-1 shadow-xl">
                            {/* Extension titles run long ("Template:
                                team conventions…"), so these stack the
                                hint under the title instead of racing
                                it for one line. */}
                            {newMenuCommands.map((c) => (
                              <button
                                key={c.id}
                                onClick={() => {
                                  setNewMenuOpen(false);
                                  setNewSubOpen(false);
                                  void c.run();
                                }}
                                className="block w-full px-3.5 py-2 text-left transition hover:bg-accent-soft"
                              >
                                <span className="block truncate text-sm font-medium">
                                  {c.title}
                                </span>
                                {c.hint && (
                                  <span className="block truncate text-xs text-ink-faint">
                                    {c.hint}
                                  </span>
                                )}
                              </button>
                            ))}
                          </div>
                        )}
                      </div>
                    )}
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
                disabled={busyAction}
                onClick={() => startRename("collection", activeCollection.name)}
                className="rounded-md border border-line bg-surface px-2.5 py-1 font-medium text-ink-soft transition hover:border-accent hover:text-ink disabled:opacity-50"
              >
                Rename…
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
                  void (async () => {
                    const ok = await confirmAction(
                      `Delete ${activeCollection.name}? Its assets stay in the library`,
                      "Delete",
                    );
                    if (!ok) return;
                    DeleteCollection(activeCollection.name)
                      .then(() => {
                        setScope({ kind: "all" });
                        load();
                        setToastMessage(
                          "Collection removed — its assets are still in the library",
                        );
                      })
                      .catch((e) => setToastMessage(String(e)));
                  })();
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
                onClick={() => startRename("team", activeTeam.name)}
                className="rounded-md border border-line bg-surface px-2.5 py-1 font-medium text-ink-soft transition hover:border-accent hover:text-ink"
              >
                Rename…
              </button>
              <button
                onClick={() => setOpenTeam(activeTeam)}
                className="rounded-md border border-line bg-surface px-2.5 py-1 font-medium text-ink-soft transition hover:border-accent hover:text-ink"
              >
                Manage team…
              </button>
              <button
                onClick={() => {
                  void (async () => {
                    const ok = await confirmAction(
                      `Delete team ${activeTeam.name}? Assets stay in the library, but anything shared only with this team stops installing for its members`,
                      "Delete",
                    );
                    if (!ok) return;
                    DeleteTeam(activeTeam.name)
                      .then(() => {
                        setScope({ kind: "all" });
                        load();
                        setToastMessage(`Team ${activeTeam.name} deleted`);
                      })
                      .catch((e) => setToastMessage(String(e)));
                  })();
                }}
                className="rounded-md px-2 py-1 font-medium text-ink-faint transition hover:text-danger"
              >
                Delete
              </button>
            </div>
          )}
        </header>

        {/* Content */}
        <main className="flex-1 overflow-y-auto" onMouseDown={startMarquee}>
          {error && (
            <div className="m-5 rounded-lg bg-danger-soft px-4 py-3 text-sm text-danger">
              {error}{" "}
              <button className="underline" onClick={load}>
                Try again
              </button>
            </div>
          )}

          {/* Extension collection tabs: only when a collection scope is
              open AND at least one view is registered — otherwise the
              default experience is untouched. */}
          {activeCollection && collectionViews.length > 0 && (
            <div
              className="flex items-center gap-1 border-b border-line px-6 pt-2"
              data-collection-tabs
            >
              {[
                { key: "", title: "Assets" },
                ...collectionViews.map((v) => ({
                  key: v.pluginId + ":" + v.spec.id,
                  title: v.spec.title,
                })),
              ].map((t) => (
                <button
                  key={t.key}
                  data-collection-tab={t.key || "assets"}
                  onClick={() => setCollectionTab(t.key)}
                  className={`rounded-t-lg border-b-2 px-3 py-1.5 text-xs font-medium transition ${
                    (currentCollectionView ? collectionTab : "") === t.key
                      ? "border-accent text-ink"
                      : "border-transparent text-ink-faint hover:text-ink"
                  }`}
                >
                  {t.title}
                </button>
              ))}
            </div>
          )}

          {activeCollection && currentCollectionView ? (
            <div className="h-full overflow-y-auto p-5">
              <CollectionViewMount
                key={activeCollection.name + ":" + collectionTab}
                pluginId={currentCollectionView.pluginId}
                spec={currentCollectionView.spec}
                collection={activeCollection.name}
              />
            </div>
          ) : scope.kind === "dashboard" ? (
            <Dashboard />
          ) : scope.kind === "plugin-view" ? (
            (() => {
              const entry = mainViews.find(
                (v) => v.pluginId + ":" + v.spec.id === scope.name,
              );
              return entry ? (
                <div className="h-full overflow-y-auto p-5">
                  <PluginMount
                    key={scope.name}
                    pluginId={entry.pluginId}
                    mount={entry.spec.mount}
                  />
                </div>
              ) : (
                <EmptyState scope={scope} hasAssets={true} />
              );
            })()
          ) : assets === null ? (
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
                  checked={multiSel.has(a.name)}
                  excerpt={query.trim() ? contentHits?.get(a.name) : undefined}
                  onClick={(e) =>
                    handleRowClick(
                      a.name,
                      e,
                      shown.map((s) => s.name),
                    )
                  }
                  onContextMenu={(e) => handleRowContextMenu(a.name, e)}
                  onDragHandle={startAssetDrag}
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
                  data-asset-row={a.name}
                  onClick={(e) =>
                    handleRowClick(
                      a.name,
                      e,
                      shown.map((s) => s.name),
                    )
                  }
                  onContextMenu={(e) => handleRowContextMenu(a.name, e)}
                  onMouseDown={(e) => startAssetDrag(a.name, e)}
                  className={`rounded-xl border p-4 text-left transition hover:-translate-y-px hover:border-accent hover:shadow-sm ${
                    multiSel.has(a.name)
                      ? "border-accent bg-accent-soft"
                      : "border-line bg-surface"
                  }`}
                >
                  <div className="flex items-start justify-between gap-2">
                    <div className="truncate text-sm font-semibold">
                      {a.name}
                    </div>
                    <TypeBadge type={a.type} label={a.typeLabel} />
                  </div>
                  <div className="mt-1.5 line-clamp-2 min-h-10 text-sm text-ink-soft">
                    {query.trim() && contentHits?.get(a.name) ? (
                      <ContentExcerpt m={contentHits.get(a.name)!} />
                    ) : (
                      a.description || "No description yet."
                    )}
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

      {confirmState && (
        <div
          className="fixed inset-0 z-[90] flex items-center justify-center bg-black/40"
          onMouseDown={(e) => {
            if (e.target === e.currentTarget) answerConfirm(false);
          }}
          onKeyDown={(e) => {
            if (e.key === "Escape") {
              e.stopPropagation();
              answerConfirm(false);
            }
          }}
        >
          <div className="w-[400px] rounded-2xl border border-line bg-surface p-5 shadow-2xl">
            <p className="text-sm text-ink-soft">{confirmState.message}</p>
            <div className="mt-4 flex justify-end gap-2">
              <button
                onClick={() => answerConfirm(false)}
                className="rounded-lg border border-line px-4 py-2 text-sm font-medium text-ink-soft transition hover:text-ink"
              >
                Cancel
              </button>
              <button
                autoFocus
                onClick={() => answerConfirm(true)}
                className="rounded-lg bg-accent px-4 py-2 text-sm font-medium text-white transition hover:opacity-90"
              >
                {confirmState.action}
              </button>
            </div>
          </div>
        </div>
      )}

      {marquee && (
        <div
          className="pointer-events-none fixed z-40 border border-accent bg-accent/10"
          style={{
            left: Math.min(marquee.x0, marquee.x1),
            top: Math.min(marquee.y0, marquee.y1),
            width: Math.abs(marquee.x1 - marquee.x0),
            height: Math.abs(marquee.y1 - marquee.y0),
          }}
        />
      )}

      {ctxMenu && multiSel.size > 0 && (
        <div
          className="fixed inset-0 z-50"
          onMouseDown={() => setCtxMenu(null)}
          onContextMenu={(e) => {
            e.preventDefault();
            setCtxMenu(null);
          }}
        >
          <div
            onMouseDown={(e) => e.stopPropagation()}
            className="absolute w-56 overflow-hidden rounded-xl border border-line bg-surface py-1 shadow-xl"
            style={{
              left: Math.min(ctxMenu.x, window.innerWidth - 240),
              top: Math.min(ctxMenu.y, window.innerHeight - 120),
            }}
          >
            <button
              onClick={() => {
                setCtxMenu(null);
                setBulkShare(true);
              }}
              className="block w-full px-3 py-1.5 text-left text-sm text-ink-soft transition hover:bg-canvas hover:text-ink"
            >
              Share{" "}
              {multiSel.size === 1
                ? [...multiSel][0]
                : `${multiSel.size} skills`}
              …
            </button>
            <div className="mx-3 my-1 border-t border-line" />
            <button
              onClick={() => {
                setCtxMenu(null);
                setConfirmBulkDelete(true);
              }}
              className="block w-full px-3 py-1.5 text-left text-sm text-ink-soft transition hover:bg-canvas hover:text-danger"
            >
              Delete{" "}
              {multiSel.size === 1
                ? [...multiSel][0]
                : `${multiSel.size} skills`}
              …
            </button>
          </div>
        </div>
      )}

      {bulkShare && multiSel.size > 0 && (
        <ShareModal
          title={
            multiSel.size === 1
              ? `Manage installations — ${[...multiSel][0]}`
              : `Manage installations — ${multiSel.size} skills`
          }
          teams={teams}
          getInstallations={async () => {
            // A single selection shows the asset's real install rows
            // (matching the detail panel). A true multi-selection shows
            // the intersection: a row appears only when EVERY selected
            // asset already has it — rows compare by their scope fields,
            // never by server entity ids, which differ per asset.
            const names = [...multiSel];
            if (names.length === 1) {
              return GetAssetInstallations(names[0]);
            }
            const all = await Promise.all(
              names.map((n) => GetAssetInstallations(n)),
            );
            const key = (i: main.AssetInstallation) =>
              JSON.stringify([i.kind, i.repo, i.paths, i.team, i.user, i.bot]);
            const shared = (all[0]?.installations ?? [])
              .filter((row) =>
                all.every((v) =>
                  (v.installations ?? []).some((i) => key(i) === key(row)),
                ),
              )
              // Entity ids belong to ONE asset's rows; a bulk remove must
              // address every asset by scope fields instead.
              .map(
                (row) =>
                  ({
                    ...row,
                    entityId: "",
                    monoRepoConfigId: "",
                  }) as main.AssetInstallation,
              );
            return {
              everyone: all.every((v) => v.everyone),
              installations: shared,
            } as main.InstallationsView;
          }}
          addInstallation={async (inst) => {
            if (
              multiSel.size > 1 &&
              !(await confirmAction(
                `Install ${multiSel.size} skills there?`,
                "Install",
              ))
            ) {
              return;
            }
            for (const n of multiSel) {
              await AddAssetInstallation(n, inst);
            }
          }}
          removeInstallation={async (inst) => {
            if (
              multiSel.size > 1 &&
              !(await confirmAction(
                `Remove this installation from ${multiSel.size} skills?`,
                "Remove",
              ))
            ) {
              return;
            }
            for (const n of multiSel) {
              await RemoveAssetInstallationRow(n, inst);
            }
          }}
          onClose={() => setBulkShare(false)}
          onChanged={load}
        />
      )}

      {confirmBulkDelete && multiSel.size > 0 && (
        <Modal
          title={
            multiSel.size === 1
              ? `Delete ${[...multiSel][0]}?`
              : `Delete ${multiSel.size} skills?`
          }
          onClose={() => setConfirmBulkDelete(false)}
        >
          <p className="text-sm text-ink-soft">
            This permanently removes {multiSel.size === 1 ? "it" : "them"} from
            the library — every revision, for everyone who uses this library.
            Installed copies are cleaned up on the next sync.
          </p>
          <div className="mt-4 flex justify-end gap-2">
            <button
              onClick={() => setConfirmBulkDelete(false)}
              disabled={deleting}
              className="rounded-lg border border-line px-4 py-2 text-sm font-medium text-ink-soft transition hover:text-ink disabled:opacity-50"
            >
              Cancel
            </button>
            <button
              onClick={() => {
                setDeleting(true);
                DeleteAssets([...multiSel])
                  .then(() => {
                    setToastMessage(
                      multiSel.size === 1
                        ? "Deleted 1 skill from the library"
                        : `Deleted ${multiSel.size} skills from the library`,
                    );
                    setMultiSel(new Set());
                    setSelected(null);
                    setConfirmBulkDelete(false);
                    load();
                  })
                  .catch((e) => setToastMessage(String(e)))
                  .finally(() => setDeleting(false));
              }}
              disabled={deleting}
              className="rounded-lg bg-danger px-4 py-2 text-sm font-medium text-white transition hover:opacity-90 disabled:opacity-50"
            >
              {deleting ? "Deleting…" : "Delete"}
            </button>
          </div>
        </Modal>
      )}

      <CommandPalette
        open={paletteOpen}
        onClose={() => setPaletteOpen(false)}
        coreCommands={coreCommands}
      />

      {assetDrag && (
        <div
          className="pointer-events-none fixed z-50 -translate-y-1/2 rounded-full bg-accent px-3 py-1 text-xs font-medium text-white shadow-lg"
          style={{ left: assetDrag.x + 12, top: assetDrag.y }}
        >
          {assetDrag.name}
          {dropCollection || dropTeam || dropRepo
            ? ` → ${dropCollection || dropTeam || repoLabel(dropRepo)}`
            : ""}
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
          onDelete={() => {
            const name = selected;
            void (async () => {
              const ok = await confirmAction(
                `Delete ${name}? This permanently removes it from the library for everyone`,
                "Delete",
              );
              if (!ok) return;
              try {
                await DeleteAssets([name]);
              } catch (e) {
                setToastMessage(String(e));
                return;
              }
              setSelected(null);
              setMultiSel(new Set());
              load();
              setToastMessage(`Deleted ${name}`);
            })();
          }}
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
          title={`Manage installations — ${shareCollection}`}
          teams={teams}
          getInstallations={() => GetCollectionInstallations(shareCollection)}
          addInstallation={(inst) =>
            AddCollectionInstallation(shareCollection, inst)
          }
          removeInstallation={(inst) =>
            RemoveCollectionInstallationRow(shareCollection, inst)
          }
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

      {browse === "repos" && (
        <BrowseModal
          title="All repositories"
          items={repoUrls.map((url) => {
            const count = (repoAssets?.[url] ?? []).length;
            return {
              name: url,
              label: repoLabel(url),
              count,
              countLabel: count === 1 ? "asset" : "assets",
            };
          })}
          pinned={shownRepoPins}
          onTogglePin={(name) => togglePin("repos", name)}
          onSelect={(name) => {
            setBrowse("");
            setScope({ kind: "repo", name });
            setSelected(null);
          }}
          onClose={() => setBrowse("")}
        />
      )}

      {openTeam && (
        <TeamModal
          team={openTeam}
          showRepos={vault.trackRepos}
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

      {renameTarget && (
        <Modal
          title={`Rename ${renameTarget.kind}`}
          onClose={() => setRenameTarget(null)}
        >
          <form
            onSubmit={(e) => {
              e.preventDefault();
              void confirmRename();
            }}
          >
            <input
              autoFocus
              value={renameValue}
              onChange={(e) => setRenameValue(e.target.value)}
              className="w-full rounded-lg border border-line bg-canvas px-3 py-2 text-sm outline-none focus:border-accent"
            />
            <p className="mt-2 text-xs text-ink-faint">
              {renameTarget.kind === "team"
                ? "Everything shared with this team follows the new name."
                : "The collection keeps its assets under the new name."}
            </p>
            <div className="mt-4 flex justify-end gap-2">
              <button
                type="button"
                onClick={() => setRenameTarget(null)}
                className="rounded-lg border border-line px-4 py-2 text-sm font-medium text-ink-soft transition hover:text-ink"
              >
                Cancel
              </button>
              <button
                type="submit"
                disabled={
                  busyAction ||
                  !renameValue.trim() ||
                  renameValue.trim() === renameTarget.name
                }
                className="rounded-lg bg-accent px-4 py-2 text-sm font-medium text-white transition hover:opacity-90 disabled:opacity-50"
              >
                {busyAction ? "Renaming…" : "Rename"}
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
            // Extensions are per-library: swap in the new library's
            // extension set, policy, and enablement.
            void syncVaultExtensions();
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

/** The highlighted context line for a full-text search hit. */
function ContentExcerpt({ m }: { m: main.ContentMatch }) {
  return (
    <>
      {m.before}
      <mark className="rounded-sm bg-accent-soft px-0.5 text-accent">
        {m.match}
      </mark>
      {m.after}
      {m.matches > 1 && (
        <span className="ml-1.5 text-xs text-ink-faint">×{m.matches}</span>
      )}
    </>
  );
}

function AssetRow({
  asset,
  installed,
  badgeWidth,
  checked,
  excerpt,
  onClick,
  onContextMenu,
  onDragHandle,
}: {
  asset: main.AssetCard;
  installed: boolean;
  badgeWidth: string;
  checked: boolean;
  excerpt?: main.ContentMatch;
  onClick: (e: React.MouseEvent) => void;
  onContextMenu: (e: React.MouseEvent) => void;
  onDragHandle: (name: string, e: React.MouseEvent) => void;
}) {
  return (
    <button
      onClick={onClick}
      onContextMenu={onContextMenu}
      onMouseDown={(e) => onDragHandle(asset.name, e)}
      data-asset-row={asset.name}
      title="Drag onto a collection in the sidebar to add it"
      className={`group flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left transition ${
        checked ? "bg-accent-soft" : "hover:bg-surface"
      }`}
    >
      <span className={`flex shrink-0 ${badgeWidth}`}>
        <TypeBadge type={asset.type} label={asset.typeLabel} />
      </span>
      <span className="w-52 shrink-0 truncate text-sm font-medium">
        {asset.name}
      </span>
      <span className="min-w-0 flex-1 truncate text-sm text-ink-soft">
        {excerpt ? (
          <ContentExcerpt m={excerpt} />
        ) : (
          asset.description || "No description yet."
        )}
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
  if (scope.kind === "personal") {
    return (
      <Centered>
        <div className="text-sm font-medium">Nothing just for you yet</div>
        <div className="mt-1 max-w-sm text-sm text-ink-faint">
          Skills installed only for your account show up here — use{" "}
          <span className="font-medium text-ink-soft">Share…</span> on an
          asset and pick Personal.
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

/**
 * Mount wrapper for one extension collection view: memoizes the mount
 * closure so PluginMount doesn't remount on every Library render, and
 * threads the collection name through the CollectionViewSpec contract
 * (the same pattern as AssetDetail's AssetTabMount).
 */
function CollectionViewMount({
  pluginId,
  spec,
  collection,
}: {
  pluginId: string;
  spec: CollectionViewSpec;
  collection: string;
}) {
  const mount = useCallback(
    (view: ViewMount) => spec.mount(view, { collection }),
    [spec, collection],
  );
  return <PluginMount pluginId={pluginId} mount={mount} />;
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
