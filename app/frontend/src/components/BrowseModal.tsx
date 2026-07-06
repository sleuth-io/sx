import { useState } from "react";
import Modal from "./Modal";

export type BrowseItem = {
  name: string;
  // Display text when it differs from name (e.g. "owner/repo" for a URL).
  label?: string;
  count: number;
  countLabel: string;
};

/**
 * Browse the full list of collections, teams, or repositories when there
 * are too many for the sidebar: search, open, pin the ones you use, and —
 * for kinds that support it — create new ones.
 */
export default function BrowseModal({
  title,
  items,
  pinned,
  onTogglePin,
  onSelect,
  onCreate,
  createLabel,
  onClose,
}: {
  title: string;
  items: BrowseItem[];
  pinned: string[];
  onTogglePin: (name: string) => void;
  onSelect: (name: string) => void;
  onCreate?: () => void;
  createLabel?: string;
  onClose: () => void;
}) {
  const [search, setSearch] = useState("");
  const q = search.trim().toLowerCase();
  const visible = items.filter(
    (item) =>
      !q ||
      item.name.toLowerCase().includes(q) ||
      (item.label ?? "").toLowerCase().includes(q),
  );

  return (
    <Modal title={title} onClose={onClose} width="w-[440px]">
      {onCreate && (
        <button
          onClick={onCreate}
          className="mb-3 w-full rounded-lg border border-dashed border-line px-3 py-2 text-left text-sm font-medium text-accent transition hover:border-accent"
        >
          + {createLabel}
        </button>
      )}

      <input
        autoFocus
        value={search}
        onChange={(e) => setSearch(e.target.value)}
        placeholder="Search…"
        className="mb-2 w-full rounded-lg border border-line bg-canvas px-3 py-2 text-sm outline-none focus:border-accent"
      />

      <ul className="max-h-72 space-y-0.5 overflow-y-auto">
        {visible.map((item) => {
          const isPinned = pinned.includes(item.name);
          return (
            <li key={item.name} className="group flex items-center gap-1">
              <button
                onClick={() => onSelect(item.name)}
                title={item.label ? item.name : undefined}
                className="flex min-w-0 flex-1 items-center gap-2 rounded-lg px-3 py-1.5 text-left text-sm transition hover:bg-accent-soft"
              >
                <span className="min-w-0 flex-1 truncate">
                  {item.label ?? item.name}
                </span>
                <span className="text-xs text-ink-faint">
                  {item.count} {item.countLabel}
                </span>
              </button>
              <button
                onClick={() => onTogglePin(item.name)}
                title={isPinned ? "Unpin from sidebar" : "Pin to sidebar"}
                className={`shrink-0 rounded px-1.5 py-1 text-sm transition ${
                  isPinned
                    ? "text-accent"
                    : "text-ink-faint opacity-0 hover:text-ink group-hover:opacity-100"
                }`}
              >
                {isPinned ? "★" : "☆"}
              </button>
            </li>
          );
        })}
        {visible.length === 0 && (
          <li className="px-3 py-2 text-sm text-ink-faint">Nothing matches</li>
        )}
      </ul>
      <p className="mt-2 text-xs text-ink-faint">
        ★ pinned items stay in your sidebar.
      </p>
    </Modal>
  );
}
