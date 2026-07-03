import { useCallback, useEffect, useMemo, useState } from "react";
import { ListAssets } from "../../wailsjs/go/main/App";
import type { main } from "../../wailsjs/go/models";
import AssetDetail from "../components/AssetDetail";
import TypeBadge from "../components/TypeBadge";

/** Library: everything in the vault, one card per asset. */
export default function Library({
  vault,
}: {
  vault: main.VaultInfo;
  onVaultChanged: () => void;
}) {
  const [assets, setAssets] = useState<main.AssetCard[] | null>(null);
  const [error, setError] = useState("");
  const [query, setQuery] = useState("");
  const [typeFilter, setTypeFilter] = useState<string>("");
  const [selected, setSelected] = useState<string | null>(null);

  const load = useCallback(() => {
    setError("");
    ListAssets()
      .then(setAssets)
      .catch((e) => {
        setError(String(e));
        setAssets([]);
      });
  }, []);

  useEffect(load, [load]);

  // Reload when the window regains focus so remote changes appear.
  useEffect(() => {
    const onFocus = () => load();
    window.addEventListener("focus", onFocus);
    return () => window.removeEventListener("focus", onFocus);
  }, [load]);

  const types = useMemo(() => {
    const seen = new Map<string, string>();
    for (const a of assets ?? []) {
      if (!seen.has(a.type)) seen.set(a.type, a.typeLabel);
    }
    return [...seen.entries()].sort();
  }, [assets]);

  const visible = useMemo(() => {
    const q = query.trim().toLowerCase();
    return (assets ?? []).filter((a) => {
      if (typeFilter && a.type !== typeFilter) return false;
      if (!q) return true;
      return (
        a.name.toLowerCase().includes(q) ||
        a.description.toLowerCase().includes(q)
      );
    });
  }, [assets, query, typeFilter]);

  return (
    <div className="flex h-full flex-col bg-canvas">
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

          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search…"
            className="w-64 rounded-lg border border-line bg-canvas px-3 py-1.5 text-sm outline-none focus:border-accent"
            style={{ ["--wails-draggable" as never]: "no-drag" }}
          />
        </div>

        {types.length > 1 && (
          <div
            className="flex gap-1.5 px-5 pb-3"
            style={{ ["--wails-draggable" as never]: "no-drag" }}
          >
            <FilterChip
              label="All"
              active={typeFilter === ""}
              onClick={() => setTypeFilter("")}
            />
            {types.map(([key, label]) => (
              <FilterChip
                key={key}
                label={label + "s"}
                active={typeFilter === key}
                onClick={() => setTypeFilter(key)}
              />
            ))}
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
        ) : visible.length === 0 && !error ? (
          <EmptyState hasAssets={(assets ?? []).length > 0} />
        ) : (
          <div className="grid grid-cols-[repeat(auto-fill,minmax(240px,1fr))] gap-3">
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

      {selected && (
        <AssetDetail name={selected} onClose={() => setSelected(null)} />
      )}
    </div>
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
        Drop a markdown file anywhere in this window to add your first asset —
        a skill, a rule, anything your AI tools should know.
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
