import { useEffect, useRef, useState } from "react";
import { ListGitRepos } from "../../wailsjs/go/main/App";
import type { main } from "../../wailsjs/go/models";

/**
 * Git repository input with a searchable picker. When the GitHub CLI is
 * signed in, the user's repositories load as suggestions filtered by
 * what's typed; picking one fills the clone URL. Without gh it's a plain
 * URL input — no capability is lost, one stops being required.
 */
export default function RepoPicker({
  value,
  onChange,
  disabled,
  autoFocus,
}: {
  value: string;
  onChange: (v: string) => void;
  disabled: boolean;
  autoFocus?: boolean;
}) {
  const [repos, setRepos] = useState<main.GitRepoOption[]>([]);
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    ListGitRepos()
      .then((r) => setRepos(r ?? []))
      .catch(() => setRepos([]));
  }, []);

  // Close on outside clicks.
  useEffect(() => {
    const onDown = (e: globalThis.MouseEvent) => {
      if (!rootRef.current?.contains(e.target as Node)) setOpen(false);
    };
    window.addEventListener("mousedown", onDown);
    return () => window.removeEventListener("mousedown", onDown);
  }, []);

  const q = value.trim().toLowerCase();
  const matches =
    q === ""
      ? repos
      : repos.filter((r) => r.name.toLowerCase().includes(q));
  const shown = matches.slice(0, 8);
  // Once the value is a picked/typed URL, suggestions are done.
  const isURL = q.includes("://") || q.startsWith("git@");
  const showList = open && !isURL && shown.length > 0;

  return (
    <div ref={rootRef} className="relative min-w-0 flex-1">
      <input
        autoFocus={autoFocus}
        value={value}
        onChange={(e) => {
          onChange(e.target.value);
          setOpen(true);
        }}
        onFocus={() => setOpen(true)}
        placeholder={
          repos.length > 0
            ? "Search your repositories or paste a URL"
            : "https://github.com/acme/skills.git"
        }
        disabled={disabled}
        className="w-full rounded-lg border border-line bg-canvas px-3 py-2 text-sm outline-none focus:border-accent"
      />
      {showList && (
        <div className="absolute left-0 right-0 top-full z-50 mt-1 max-h-56 overflow-y-auto rounded-lg border border-line bg-surface py-1 shadow-lg">
          {shown.map((r) => (
            <button
              key={r.name}
              type="button"
              onClick={() => {
                onChange(r.url);
                setOpen(false);
              }}
              className="block w-full truncate px-3 py-1.5 text-left text-sm text-ink-soft transition hover:bg-canvas hover:text-ink"
            >
              {r.name}
            </button>
          ))}
          {matches.length > shown.length && (
            <div className="px-3 py-1.5 text-xs text-ink-faint">
              Keep typing to narrow {matches.length - shown.length} more…
            </div>
          )}
        </div>
      )}
    </div>
  );
}
