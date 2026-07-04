import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  CreateBlankDraft,
  CreateDraftFromAsset,
  CreateDraftFromPaths,
  DeleteCollection,
  GetDraft,
  InstallCollection,
  InstalledAssets,
  ListAIClients,
  ListAssets,
  ListCollections,
  ListDrafts,
  PickFilesForDraft,
  PickFolderForDraft,
} from "../../wailsjs/go/main/App";
import {
  EventsOff,
  EventsOn,
  OnFileDrop,
  OnFileDropOff,
} from "../../wailsjs/runtime/runtime";
import type { main } from "../../wailsjs/go/models";
import AssetDetail from "../components/AssetDetail";
import CollectionModal from "../components/CollectionModal";
import DraftSheet from "../components/DraftSheet";
import SettingsModal from "../components/SettingsModal";
import Sidebar, { Scope } from "../components/Sidebar";
import TypeBadge from "../components/TypeBadge";

type ViewMode = "list" | "grid";
type SortMode = "updated" | "name";

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
  const [installedInfo, setInstalledInfo] = useState<
    main.InstalledAssetInfo[]
  >([]);
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
  const [showSettings, setShowSettings] = useState(false);
  const [newMenuOpen, setNewMenuOpen] = useState(false);
  const newMenuRef = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [openDraft, setOpenDraft] = useState<main.Draft | null>(null);
  const [dragging, setDragging] = useState(false);
  const [toast, setToast] = useState("");
  const [busyAction, setBusyAction] = useState(false);

  const load = useCallback(() => {
    setError("");
    ListAssets()
      .then(setAssets)
      .catch((e) => {
        setError(String(e));
        setAssets([]);
      });
    ListDrafts()
      .then(setDrafts)
      .catch(() => setDrafts([]));
    ListCollections()
      .then(setCollections)
      .catch(() => setCollections([]));
    InstalledAssets()
      .then(setInstalledInfo)
      .catch(() => setInstalledInfo([]));
  }, []);

  useEffect(load, [load]);
  useEffect(() => {
    ListAIClients().then(setAiClients);
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
      if (((e.metaKey || e.ctrlKey) && e.key === "f") || (!inField && e.key === "/")) {
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
      CreateDraftFromPaths(paths)
        .then((draft) => {
          setOpenDraft(draft);
          load();
        })
        .catch((e) => setToastMessage(String(e)));
    }, false);
    const over = (e: DragEvent) => {
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

  // Close the New menu on outside clicks.
  useEffect(() => {
    if (!newMenuOpen) return;
    const onClick = (e: MouseEvent) => {
      if (!newMenuRef.current?.contains(e.target as Node))
        setNewMenuOpen(false);
    };
    window.addEventListener("mousedown", onClick);
    return () => window.removeEventListener("mousedown", onClick);
  }, [newMenuOpen]);

  function setToastMessage(message: string) {
    setToast(message);
    window.setTimeout(() => setToast(""), 4000);
  }

  const installed = useMemo(
    () => new Set(installedInfo.map((i) => i.name)),
    [installedInfo],
  );
  const installedScopes = useMemo(() => {
    const m = new Map<string, string[]>();
    for (const i of installedInfo) m.set(i.name, i.scopes ?? []);
    return m;
  }, [installedInfo]);

  // Native menu → Settings (Cmd+, / Ctrl+,)
  useEffect(() => {
    EventsOn("open-settings", () => setShowSettings(true));
    return () => EventsOff("open-settings");
  }, []);

  const types = useMemo(() => {
    const seen = new Map<string, string>();
    for (const a of assets ?? []) {
      if (!seen.has(a.type)) seen.set(a.type, a.typeLabel);
    }
    return [...seen.entries()].sort();
  }, [assets]);

  const typeCounts = useMemo(() => {
    const counts: Record<string, number> = {};
    for (const a of assets ?? []) counts[a.type] = (counts[a.type] ?? 0) + 1;
    return counts;
  }, [assets]);

  const activeCollection = useMemo(
    () =>
      scope.kind === "collection"
        ? (collections.find((c) => c.name === scope.name) ?? null)
        : null,
    [collections, scope],
  );

  const visible = useMemo(() => {
    const q = query.trim().toLowerCase();
    const list = (assets ?? []).filter((a) => {
      switch (scope.kind) {
        case "type":
          if (a.type !== scope.type) return false;
          break;
        case "installed":
          if (!installed.has(a.name)) return false;
          break;
        case "drafts":
          return false;
        case "collection":
          if (!(activeCollection?.assets ?? []).includes(a.name)) return false;
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
  }, [assets, query, scope, installed, activeCollection, sort]);

  const visibleDrafts = useMemo(() => {
    if (scope.kind !== "all" && scope.kind !== "drafts") return [];
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
        return "All assets";
      case "type":
        return scope.label + "s";
      case "installed":
        return "In your AI tools";
      case "drafts":
        return "Drafts";
      case "collection":
        return scope.name;
    }
  })();

  const nothingToShow =
    visible.length === 0 && visibleDrafts.length === 0 && !error;

  return (
    <div
      className="flex h-full bg-canvas"
      style={{ ["--wails-drop-target" as never]: "drop" }}
    >
      <Sidebar
        vault={vault}
        scope={scope}
        onScope={(s) => {
          setScope(s);
          setSelected(null);
        }}
        types={types}
        typeCounts={typeCounts}
        totalCount={(assets ?? []).length}
        installedCount={installedInfo.length}
        draftCount={drafts.length}
        collections={collections}
        aiClients={aiClients}
        onNewCollection={() => setShowCollectionModal(true)}
        onSettings={() => setShowSettings(true)}
      />

      <div className="flex min-w-0 flex-1 flex-col">
        {/* Toolbar */}
        <header className="titlebar-drag shrink-0 border-b border-line bg-surface">
          <div className="flex items-center gap-3 px-5 pb-3 pt-9">
            <h1 className="text-sm font-semibold">{scopeTitle}</h1>
            <span className="text-xs text-ink-faint">
              {visible.length + visibleDrafts.length}
            </span>

            <div className="flex-1" />

            <div
              className="flex items-center gap-2"
              style={{ ["--wails-draggable" as never]: "no-drag" }}
            >
              <input
                ref={searchRef}
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder="Search…  ( / )"
                className="w-56 rounded-lg border border-line bg-canvas px-3 py-1.5 text-sm outline-none focus:border-accent"
              />

              <select
                value={sort}
                onChange={(e) => setSort(e.target.value as SortMode)}
                title="Sort"
                className="rounded-lg border border-line bg-canvas px-2 py-1.5 text-xs text-ink-soft outline-none"
              >
                <option value="updated">Recently updated</option>
                <option value="name">Name</option>
              </select>

              <div className="flex overflow-hidden rounded-lg border border-line">
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

              <div className="relative" ref={newMenuRef}>
                <button
                  onClick={() => setNewMenuOpen((v) => !v)}
                  className="flex items-center gap-1.5 rounded-lg bg-accent px-3.5 py-1.5 text-sm font-medium text-white transition hover:opacity-90"
                >
                  <span className="text-base leading-none">+</span> New
                  <span className="text-[10px] opacity-70">▾</span>
                </button>
                {newMenuOpen && (
                  <div className="absolute right-0 z-40 mt-1.5 w-56 overflow-hidden rounded-xl border border-line bg-surface py-1 shadow-xl">
                    <MenuItem
                      label="Add files…"
                      hint="Markdown or zip"
                      onClick={() => {
                        setNewMenuOpen(false);
                        PickFilesForDraft()
                          .then((d) => {
                            setOpenDraft(d);
                            load();
                          })
                          .catch((e) => {
                            if (!String(e).includes("cancelled"))
                              setToastMessage(String(e));
                          });
                      }}
                    />
                    <MenuItem
                      label="Add a folder…"
                      hint="Multi-file asset"
                      onClick={() => {
                        setNewMenuOpen(false);
                        PickFolderForDraft()
                          .then((d) => {
                            setOpenDraft(d);
                            load();
                          })
                          .catch((e) => {
                            if (!String(e).includes("cancelled"))
                              setToastMessage(String(e));
                          });
                      }}
                    />
                    <MenuItem
                      label="Write from scratch"
                      hint="Blank skill"
                      onClick={() => {
                        setNewMenuOpen(false);
                        CreateBlankDraft("skill")
                          .then((d) => {
                            setOpenDraft(d);
                            load();
                          })
                          .catch((e) => setToastMessage(String(e)));
                      }}
                    />
                    <div className="my-1 border-t border-line" />
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
              {visibleDrafts.map((d) => (
                <DraftRow
                  key={"draft-" + d.id}
                  draft={d}
                  onClick={() => void openExistingDraft(d.id)}
                />
              ))}
              {visible.map((a) => (
                <AssetRow
                  key={a.name}
                  asset={a}
                  installed={installed.has(a.name)}
                  onClick={() => setSelected(a.name)}
                />
              ))}
            </div>
          ) : (
            <div className="grid grid-cols-[repeat(auto-fill,minmax(240px,1fr))] gap-3 p-5">
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
              {visible.map((a) => (
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
            </div>
          )}
        </main>
      </div>

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

      {showCollectionModal && (
        <CollectionModal
          onClose={() => setShowCollectionModal(false)}
          onCreated={(name) => {
            setShowCollectionModal(false);
            setScope({ kind: "collection", name });
            load();
          }}
        />
      )}

      {showSettings && (
        <SettingsModal
          onClose={() => setShowSettings(false)}
          onProfileChanged={() => {
            setShowSettings(false);
            setScope({ kind: "all" });
            onVaultChanged();
          }}
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
  onClick,
}: {
  asset: main.AssetCard;
  installed: boolean;
  onClick: () => void;
}) {
  return (
    <button
      onClick={onClick}
      className="group flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left transition hover:bg-surface"
    >
      <TypeBadge type={asset.type} label={asset.typeLabel} />
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
      <span className="shrink-0 rounded-full bg-amber-50 px-2 py-0.5 text-[11px] font-medium text-amber-700 dark:bg-amber-950 dark:text-amber-300">
        Draft
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
      <span className="font-medium">{label}</span>
      {hint && <span className="text-xs text-ink-faint">{hint}</span>}
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
          <span className="font-medium text-ink-soft">
            Use in my AI tools
          </span>{" "}
          — it lands in Claude Code and the other tools listed in the sidebar.
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
  if (hasAssets) {
    return (
      <Centered>
        <div className="text-sm font-medium text-ink-soft">
          Nothing matches
        </div>
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
        the <span className="font-medium text-ink-soft">+ New</span> button —
        to add your first asset.
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
