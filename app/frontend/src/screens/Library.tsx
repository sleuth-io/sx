import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  CreateBlankDraft,
  CreateDraftFromAsset,
  CreateDraftFromPaths,
  DeleteCollection,
  GetDraft,
  InstallCollection,
  ListAssets,
  ListCollections,
  ListDrafts,
  PickFilesForDraft,
  PickFolderForDraft,
} from "../../wailsjs/go/main/App";
import { OnFileDrop, OnFileDropOff } from "../../wailsjs/runtime/runtime";
import type { main } from "../../wailsjs/go/models";
import AssetDetail from "../components/AssetDetail";
import CollectionModal from "../components/CollectionModal";
import DraftSheet from "../components/DraftSheet";
import SettingsModal from "../components/SettingsModal";
import TypeBadge from "../components/TypeBadge";

/** Library: everything in the vault plus local drafts, one card each. */
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
  const [error, setError] = useState("");
  const [query, setQuery] = useState("");
  const [typeFilter, setTypeFilter] = useState<string>("");
  const [collectionFilter, setCollectionFilter] = useState<string>("");
  const [showCollectionModal, setShowCollectionModal] = useState(false);
  const [showSettings, setShowSettings] = useState(false);
  const [newMenuOpen, setNewMenuOpen] = useState(false);
  const newMenuRef = useRef<HTMLDivElement>(null);
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
  }, []);

  useEffect(load, [load]);

  // Reload when the window regains focus so remote changes appear.
  useEffect(() => {
    const onFocus = () => load();
    window.addEventListener("focus", onFocus);
    return () => window.removeEventListener("focus", onFocus);
  }, [load]);

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

  function setToastMessage(message: string) {
    setToast(message);
    window.setTimeout(() => setToast(""), 4000);
  }

  const types = useMemo(() => {
    const seen = new Map<string, string>();
    for (const a of assets ?? []) {
      if (!seen.has(a.type)) seen.set(a.type, a.typeLabel);
    }
    return [...seen.entries()].sort();
  }, [assets]);

  const activeCollection = useMemo(
    () => collections.find((c) => c.name === collectionFilter) ?? null,
    [collections, collectionFilter],
  );

  const visible = useMemo(() => {
    const q = query.trim().toLowerCase();
    return (assets ?? []).filter((a) => {
      if (typeFilter && a.type !== typeFilter) return false;
      if (activeCollection && !(activeCollection.assets ?? []).includes(a.name))
        return false;
      if (!q) return true;
      return (
        a.name.toLowerCase().includes(q) ||
        a.description.toLowerCase().includes(q)
      );
    });
  }, [assets, query, typeFilter, activeCollection]);

  const visibleDrafts = useMemo(() => {
    const q = query.trim().toLowerCase();
    return drafts.filter((d) => {
      if (typeFilter && d.type !== typeFilter) return false;
      if (!q) return true;
      return (
        d.name.toLowerCase().includes(q) ||
        d.description.toLowerCase().includes(q)
      );
    });
  }, [drafts, query, typeFilter]);

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

  const nothingToShow =
    visible.length === 0 && visibleDrafts.length === 0 && !error;

  return (
    <div
      className="flex h-full flex-col bg-canvas"
      style={{ ["--wails-drop-target" as never]: "drop" }}
    >
      {/* Header — draggable strip doubles as the toolbar */}
      <header className="titlebar-drag shrink-0 border-b border-line bg-surface">
        <div className="flex items-center gap-4 px-5 pb-3 pt-9">
          <div className="flex items-center gap-2.5">
            <div className="flex h-7 w-7 items-center justify-center rounded-lg bg-accent text-xs font-semibold text-white">
              sx
            </div>
            <div>
              <div className="text-sm font-semibold leading-tight">
                Library
              </div>
              <div
                className="max-w-56 truncate text-xs text-ink-faint"
                title={vault.location}
              >
                {vault.location}
              </div>
            </div>
          </div>

          <div className="flex-1" />

          <div
            className="flex items-center gap-2"
            style={{ ["--wails-draggable" as never]: "no-drag" }}
          >
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Search…"
              className="w-64 rounded-lg border border-line bg-canvas px-3 py-1.5 text-sm outline-none focus:border-accent"
            />

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

            <button
              onClick={() => setShowSettings(true)}
              title="Settings"
              className="rounded-lg border border-line px-2.5 py-1.5 text-sm text-ink-soft transition hover:border-accent hover:text-ink"
            >
              ⚙
            </button>
          </div>
        </div>

        <div
          className="flex flex-wrap items-center gap-1.5 px-5 pb-3"
          style={{ ["--wails-draggable" as never]: "no-drag" }}
        >
          {types.length > 1 && (
            <>
              <FilterChip
                label="All"
                active={typeFilter === "" && collectionFilter === ""}
                onClick={() => {
                  setTypeFilter("");
                  setCollectionFilter("");
                }}
              />
              {types.map(([key, label]) => (
                <FilterChip
                  key={key}
                  label={label + "s"}
                  active={typeFilter === key}
                  onClick={() => {
                    setTypeFilter(typeFilter === key ? "" : key);
                    setCollectionFilter("");
                  }}
                />
              ))}
            </>
          )}
          {(collections.length > 0 || (assets ?? []).length > 0) && (
            <span className="mx-1 h-4 w-px bg-line" />
          )}
          {collections.map((c) => (
            <FilterChip
              key={"c-" + c.name}
              label={`${c.name} (${(c.assets ?? []).length})`}
              active={collectionFilter === c.name}
              onClick={() => {
                setCollectionFilter(collectionFilter === c.name ? "" : c.name);
                setTypeFilter("");
              }}
            />
          ))}
        </div>

        {activeCollection && (
          <div
            className="flex items-center gap-3 border-t border-line bg-accent-soft/50 px-5 py-2 text-xs"
            style={{ ["--wails-draggable" as never]: "no-drag" }}
          >
            <span className="font-medium">{activeCollection.name}</span>
            <span className="text-ink-soft">
              {activeCollection.description ||
                `${(activeCollection.assets ?? []).length} asset${(activeCollection.assets ?? []).length === 1 ? "" : "s"}`}
            </span>
            <div className="flex-1" />
            <button
              disabled={busyAction || (activeCollection.assets ?? []).length === 0}
              onClick={() => {
                setBusyAction(true);
                InstallCollection(activeCollection.name)
                  .then((r) =>
                    setToastMessage(`Ready to use in ${r.clients.join(", ")}`),
                  )
                  .catch((e) => setToastMessage(String(e)))
                  .finally(() => setBusyAction(false));
              }}
              className="rounded-md bg-accent px-2.5 py-1 font-medium text-white transition hover:opacity-90 disabled:opacity-50"
            >
              {busyAction ? "Setting up…" : "Use in my AI tools"}
            </button>
            <button
              disabled={busyAction}
              onClick={() => {
                DeleteCollection(activeCollection.name)
                  .then(() => {
                    setCollectionFilter("");
                    load();
                    setToastMessage("Collection removed — its assets are still in the library");
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

      {/* Body */}
      <main className="flex-1 overflow-y-auto px-5 py-5">
        {error && (
          <div className="mb-4 rounded-lg bg-danger-soft px-4 py-3 text-sm text-danger">
            {error}{" "}
            <button className="underline" onClick={load}>
              Try again
            </button>
          </div>
        )}

        {assets === null ? (
          <CardGridSkeleton />
        ) : nothingToShow ? (
          <EmptyState hasAssets={(assets ?? []).length + drafts.length > 0} />
        ) : (
          <div className="grid grid-cols-[repeat(auto-fill,minmax(240px,1fr))] gap-3">
            {visibleDrafts.map((d) => (
              <button
                key={"draft-" + d.id}
                onClick={() => void openExistingDraft(d.id)}
                className="group rounded-xl border border-dashed border-amber-300 bg-surface p-4 text-left transition hover:-translate-y-px hover:shadow-sm dark:border-amber-700"
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
                <div className="mt-3 text-xs text-ink-faint">
                  {d.targetAsset
                    ? `Unpublished changes to ${d.targetAsset}`
                    : "Only on this computer until you publish"}
                </div>
              </button>
            ))}
            {visible.map((a) => (
              <button
                key={a.name}
                onClick={() => setSelected(a.name)}
                className="group rounded-xl border border-line bg-surface p-4 text-left transition hover:-translate-y-px hover:border-accent hover:shadow-sm"
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
                <div className="mt-3 text-xs text-ink-faint">
                  {a.versions === 1
                    ? "1 revision"
                    : `${a.versions} revisions`}
                  {a.updatedAt ? ` · updated ${timeAgo(a.updatedAt)}` : ""}
                </div>
              </button>
            ))}
          </div>
        )}
      </main>

      {dragging && (
        <div className="pointer-events-none fixed inset-0 z-30 flex items-center justify-center bg-accent-soft/80">
          <div className="rounded-2xl border-2 border-dashed border-accent bg-surface px-10 py-8 text-center shadow-lg">
            <div className="text-2xl">📥</div>
            <div className="mt-2 text-sm font-semibold">
              Drop to add to your library
            </div>
            <div className="mt-1 text-xs text-ink-soft">
              Markdown files or a whole folder
            </div>
          </div>
        </div>
      )}

      {selected && (
        <AssetDetail
          name={selected}
          collections={collections}
          onClose={() => setSelected(null)}
          onEdit={() => void editAsset(selected)}
          onChanged={() => {
            load();
            setToastMessage("Restored — it's now the current revision");
          }}
          onToast={setToastMessage}
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
            setCollectionFilter(name);
            load();
          }}
        />
      )}

      {showSettings && (
        <SettingsModal
          onClose={() => setShowSettings(false)}
          onProfileChanged={() => {
            setShowSettings(false);
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

function FilterChip({
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
      className={`rounded-full px-3 py-1 text-xs font-medium transition ${
        active
          ? "bg-accent text-white"
          : "bg-canvas text-ink-soft hover:text-ink border border-line"
      }`}
    >
      {label}
    </button>
  );
}

function EmptyState({ hasAssets }: { hasAssets: boolean }) {
  if (hasAssets) {
    return (
      <div className="flex h-64 flex-col items-center justify-center text-center">
        <div className="text-sm font-medium text-ink-soft">
          Nothing matches your search
        </div>
        <div className="mt-1 text-sm text-ink-faint">
          Try a different word, or clear the type filter.
        </div>
      </div>
    );
  }
  return (
    <div className="flex h-72 flex-col items-center justify-center text-center">
      <div className="mb-3 text-3xl">📚</div>
      <div className="text-sm font-medium">Your library is empty</div>
      <div className="mt-1 max-w-sm text-sm text-ink-faint">
        Drop a markdown file, folder, or zip anywhere in this window — or use
        the <span className="font-medium text-ink-soft">+ New</span> button —
        to add your first asset.
      </div>
    </div>
  );
}

function CardGridSkeleton() {
  return (
    <div className="grid grid-cols-[repeat(auto-fill,minmax(240px,1fr))] gap-3">
      {Array.from({ length: 6 }).map((_, i) => (
        <div
          key={i}
          className="h-32 animate-pulse rounded-xl border border-line bg-surface"
        />
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
