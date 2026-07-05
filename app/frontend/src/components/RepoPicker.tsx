import { useEffect, useId, useRef, useState } from "react";
import { ListGitRepos } from "../../wailsjs/go/main/App";
import type { main } from "../../wailsjs/go/models";

/**
 * suggestRepoName turns a library name into a repo-name suggestion:
 * "acme" → "acme-skills"; names already about skills pass through; empty
 * input gets the generic default.
 */
export function suggestRepoName(name: string): string {
  const slug = name
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9._-]+/g, "-")
    .replace(/^[-.]+|[-.]+$/g, "");
  if (!slug) return "ai-skills";
  return slug.includes("skill") ? slug : `${slug}-skills`;
}

/**
 * The featured create-a-repository option in git-library forms: nobody
 * has a spare repo lying around, so offer to make one before asking them
 * to pick an existing one.
 */
export function CreateRepoCard({
  title,
  subtitle,
  disabled,
  onCreate,
}: {
  title: string;
  subtitle: string;
  disabled: boolean;
  onCreate: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onCreate}
      disabled={disabled}
      className="w-full rounded-xl border border-accent/50 bg-accent-soft/40 px-3 py-2.5 text-left transition hover:border-accent disabled:cursor-not-allowed disabled:opacity-50"
    >
      <div className="text-sm font-medium">{title}</div>
      <div className="text-xs text-ink-faint">{subtitle}</div>
    </button>
  );
}

/**
 * Git repository combobox (the ARIA combobox-with-list-autocomplete
 * pattern). When the GitHub CLI is signed in, the user's repositories load
 * as suggestions filtered by what's typed; picking one fills the clone
 * URL. The search icon and chevron signal that this is a picker, not just
 * a text field — without gh it degrades to a plain URL input.
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
  const [activeIdx, setActiveIdx] = useState(-1);
  const rootRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const listId = useId();

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
    q === "" ? repos : repos.filter((r) => r.name.toLowerCase().includes(q));
  const shown = matches.slice(0, 8);
  // Once the value is a picked/typed URL, suggestions are done.
  const isURL = q.includes("://") || q.startsWith("git@");
  const hasList = repos.length > 0;
  const showList = open && !isURL && shown.length > 0;

  function pick(r: main.GitRepoOption) {
    onChange(r.url);
    setOpen(false);
    setActiveIdx(-1);
  }

  function onKeyDown(e: React.KeyboardEvent) {
    if (!showList) {
      if (e.key === "ArrowDown" && hasList && !isURL) {
        setOpen(true);
        setActiveIdx(0);
        e.preventDefault();
      }
      return;
    }
    switch (e.key) {
      case "ArrowDown":
        setActiveIdx((i) => (i + 1) % shown.length);
        e.preventDefault();
        break;
      case "ArrowUp":
        setActiveIdx((i) => (i <= 0 ? shown.length - 1 : i - 1));
        e.preventDefault();
        break;
      case "Enter":
        if (activeIdx >= 0 && activeIdx < shown.length) {
          pick(shown[activeIdx]);
          e.preventDefault();
        }
        break;
      case "Escape":
        setOpen(false);
        setActiveIdx(-1);
        e.stopPropagation();
        break;
    }
  }

  return (
    <div ref={rootRef} className="relative min-w-0 flex-1">
      {/* Search icon: this field filters a list, it doesn't just hold text. */}
      <svg
        aria-hidden="true"
        className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-ink-faint"
        viewBox="0 0 16 16"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
      >
        <circle cx="7" cy="7" r="4.5" />
        <path d="m10.5 10.5 3 3" />
      </svg>
      <input
        ref={inputRef}
        autoFocus={autoFocus}
        role="combobox"
        aria-expanded={showList}
        aria-controls={listId}
        aria-autocomplete="list"
        aria-activedescendant={
          activeIdx >= 0 ? `${listId}-${activeIdx}` : undefined
        }
        value={value}
        onChange={(e) => {
          onChange(e.target.value);
          setOpen(true);
          setActiveIdx(-1);
        }}
        onFocus={() => setOpen(true)}
        onKeyDown={onKeyDown}
        placeholder={
          hasList
            ? "Search your repositories or paste a URL"
            : "https://github.com/acme/skills.git"
        }
        disabled={disabled}
        className={`w-full rounded-lg border border-line bg-canvas py-2 pl-8 text-sm outline-none focus:border-accent ${
          hasList ? "pr-8" : "pr-3"
        }`}
      />
      {/* Chevron: there's a list behind this field. */}
      {hasList && (
        <button
          type="button"
          tabIndex={-1}
          aria-label={open ? "Hide repositories" : "Show repositories"}
          disabled={disabled}
          onClick={() => {
            setOpen((o) => !o);
            inputRef.current?.focus();
          }}
          className="absolute right-1.5 top-1/2 -translate-y-1/2 rounded p-1 text-ink-faint transition hover:text-ink"
        >
          <svg
            aria-hidden="true"
            className={`h-3.5 w-3.5 transition-transform ${open && !isURL ? "rotate-180" : ""}`}
            viewBox="0 0 16 16"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.5"
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <path d="m4 6 4 4 4-4" />
          </svg>
        </button>
      )}
      {showList && (
        <div
          id={listId}
          role="listbox"
          className="absolute left-0 right-0 top-full z-50 mt-1 max-h-56 overflow-y-auto rounded-lg border border-line bg-surface py-1 shadow-lg"
        >
          {shown.map((r, i) => (
            <button
              key={r.name}
              id={`${listId}-${i}`}
              role="option"
              aria-selected={i === activeIdx}
              type="button"
              onMouseEnter={() => setActiveIdx(i)}
              onClick={() => pick(r)}
              className={`block w-full truncate px-3 py-1.5 text-left text-sm transition ${
                i === activeIdx
                  ? "bg-canvas text-ink"
                  : "text-ink-soft hover:bg-canvas hover:text-ink"
              }`}
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
