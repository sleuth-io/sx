import { useEffect, useState } from "react";
import {
  GetVaultInfo,
  ListKnownRepos,
  ListVaultBots,
} from "../../wailsjs/go/main/App";
import type { main } from "../../wailsjs/go/models";
import Modal from "./Modal";

/**
 * Manage every install row on an asset or a whole collection — org,
 * repo, path, team, user, bot — not just the kinds the app can create.
 * Rows written by the CLI or a skills.new server show up faithfully and
 * can be removed, and every scope EXCEPT path can be added here (path
 * installs — repo + subpaths, the advanced monorepo case — stay
 * CLI-only; they render and remove fine). The vault's RBAC is the gate
 * and its errors surface verbatim. Changes apply immediately. The
 * caller supplies the read/write operations so the same dialog serves
 * assets and collections.
 */

/** Strip protocol/.git noise so repo rows read like repo names. */
function shortRepo(url: string): string {
  return url
    .replace(/^[a-z+]+:\/\//i, "")
    .replace(/^git@/, "")
    .replace(/\.git$/, "");
}

function rowText(
  inst: main.AssetInstallation,
  self: string,
): { label: string; kind: string } {
  switch (inst.kind) {
    case "org":
      return { label: "Everyone in this library", kind: "Library" };
    case "repo":
      return { label: shortRepo(inst.repo ?? ""), kind: "Repo" };
    case "path":
      return {
        label: `${shortRepo(inst.repo ?? "")} › ${(inst.paths ?? []).join(", ")}`,
        kind: "Paths",
      };
    case "team":
      return { label: inst.team ?? "", kind: "Team" };
    case "user": {
      const email = inst.user ?? "";
      const you = self && email.toLowerCase() === self.toLowerCase();
      return { label: you ? `${email} (you)` : email, kind: "Personal" };
    }
    case "bot":
      return { label: inst.bot ?? "", kind: "Bot" };
    default:
      return { label: JSON.stringify(inst), kind: inst.kind };
  }
}

const ADD_KINDS = [
  { key: "org", label: "Org" },
  { key: "team", label: "Team" },
  { key: "bot", label: "Bot" },
  { key: "repo", label: "Repo" },
  { key: "user", label: "Personal" },
] as const;
type AddKind = (typeof ADD_KINDS)[number]["key"];

export default function ShareModal({
  title,
  teams,
  getInstallations,
  addInstallation,
  removeInstallation,
  onClose,
  onChanged,
}: {
  title: string;
  teams: main.TeamInfo[];
  getInstallations: () => Promise<main.InstallationsView>;
  addInstallation: (inst: main.AssetInstallation) => Promise<void>;
  removeInstallation: (inst: main.AssetInstallation) => Promise<void>;
  onClose: () => void;
  onChanged: () => void;
}) {
  const [view, setView] = useState<main.InstallationsView | null>(null);
  const [addKind, setAddKind] = useState<AddKind>("repo");
  const [search, setSearch] = useState("");
  const [repoSuggestions, setRepoSuggestions] = useState<string[]>([]);
  const [bots, setBots] = useState<string[]>([]);
  const [self, setSelf] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const refresh = () =>
    getInstallations()
      .then(setView)
      .catch((e) => setError(String(e)));
  useEffect(() => {
    void refresh();
    ListKnownRepos()
      .then((r) => setRepoSuggestions(r ?? []))
      .catch(() => {});
    ListVaultBots()
      .then((b) => setBots(b ?? []))
      .catch(() => {});
    GetVaultInfo()
      .then((v) => setSelf((v.identity ?? "").trim()))
      .catch(() => {});
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function run(action: () => Promise<void>) {
    setBusy(true);
    setError("");
    try {
      await action();
      await refresh();
      onChanged();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }

  const add = (inst: Partial<main.AssetInstallation>) =>
    run(() => addInstallation(inst as main.AssetInstallation));

  const rows = view?.installations ?? [];
  const has = (pred: (i: main.AssetInstallation) => boolean) =>
    rows.some(pred);
  const needle = search.trim().toLowerCase();
  const personalAlready = has(
    (i) => i.kind === "user" && (i.user ?? "").toLowerCase() === self.toLowerCase(),
  );

  // Keep the picker simple: repo/team/bot options appear only once such
  // things exist in the library. Org and Personal always apply.
  const visibleKinds = ADD_KINDS.filter((k) => {
    if (k.key === "repo") return repoSuggestions.length > 0;
    if (k.key === "team") return teams.length > 0;
    if (k.key === "bot") return bots.length > 0;
    return true;
  });
  const activeKind: AddKind = visibleKinds.some((k) => k.key === addKind)
    ? addKind
    : (visibleKinds[0]?.key ?? "org");

  // Repo, team, and bot pick through ONE control: a search box over a
  // candidate list. Candidates are what exists in the library minus
  // what's already installed, filtered by the search.
  type Candidate = {
    key: string;
    label: string;
    hint?: string;
    inst: Partial<main.AssetInstallation>;
  };
  const pickerCandidates: Candidate[] = (() => {
    switch (activeKind) {
      case "repo":
        return repoSuggestions
          .filter((r) => !has((i) => i.kind === "repo" && i.repo === r))
          .filter(
            (r) =>
              !needle ||
              r.toLowerCase().includes(needle) ||
              shortRepo(r).toLowerCase().includes(needle),
          )
          .map((r) => ({
            key: r,
            label: shortRepo(r),
            inst: { kind: "repo", repo: r },
          }));
      case "team":
        return teams
          .filter((t) => !has((i) => i.kind === "team" && i.team === t.name))
          .filter((t) => !needle || t.name.toLowerCase().includes(needle))
          .map((t) => ({
            key: t.name,
            label: t.name,
            hint: `${(t.members ?? []).length} ${
              (t.members ?? []).length === 1 ? "member" : "members"
            }`,
            inst: { kind: "team", team: t.name },
          }));
      case "bot":
        return bots
          .filter((b) => !has((i) => i.kind === "bot" && i.bot === b))
          .filter((b) => !needle || b.toLowerCase().includes(needle))
          .map((b) => ({ key: b, label: b, inst: { kind: "bot", bot: b } }));
      default:
        return [];
    }
  })();
  // A typed repo URL that matches no suggestion is still addable — as a
  // row in the same list, so the control reads the same as team/bot.
  const freeRepoEntry =
    activeKind === "repo" &&
    search.trim() !== "" &&
    !repoSuggestions.some((r) => r.toLowerCase() === needle)
      ? search.trim()
      : "";

  // Narrowing works both ways and deserves a heads-up: an org install
  // removes every narrower row, and adding a narrower row while everyone
  // receives it takes it away from everyone else.
  const orgRowPresent = has((i) => i.kind === "org");
  const coversEveryone = (view?.everyone ?? false) || orgRowPresent;
  const narrowingWarning = (() => {
    if (!view) return "";
    if (activeKind === "org" && rows.length > 0 && !view.everyone) {
      const n = rows.filter((i) => i.kind !== "org").length;
      if (n > 0) {
        return `Installing for everyone removes the ${n} narrower installation${n === 1 ? "" : "s"} above — the whole library receives it instead.`;
      }
      return "";
    }
    if (activeKind !== "org" && coversEveryone) {
      return "This is currently installed for everyone. Adding a narrower installation replaces that — only the rows listed here will receive it.";
    }
    return "";
  })();

  return (
    <Modal title={title} onClose={onClose} width="w-[480px]">
      {!view ? (
        // Shaped like the loaded layout (installations list, kind tabs,
        // picker) so the modal doesn't grow abruptly when data lands.
        <div className="space-y-3">
          <div className="h-20 animate-pulse rounded-lg bg-canvas" />
          <div className="h-7 animate-pulse rounded-lg bg-canvas" />
          <div className="h-32 animate-pulse rounded-lg bg-canvas" />
        </div>
      ) : (
        <>
          <div className="mb-1.5 text-xs font-semibold tracking-wide text-ink-faint">
            CURRENT INSTALLATIONS
          </div>
          <ul className="max-h-52 space-y-1.5 overflow-y-auto">
            {view.everyone ? (
              <li className="flex items-center rounded-lg border border-line px-3 py-2">
                <span className="flex-1 text-sm">
                  Everyone in this library
                </span>
                <span className="text-xs text-ink-faint">default</span>
              </li>
            ) : (
              rows.map((inst, idx) => {
                const { label, kind } = rowText(inst, self);
                return (
                  <li
                    key={`${inst.kind}-${idx}`}
                    data-install-row={inst.kind}
                    className="flex items-center gap-2 rounded-lg border border-line px-3 py-2"
                  >
                    <span
                      className="min-w-0 flex-1 truncate text-sm"
                      title={label}
                    >
                      {label}
                      <span className="ml-1.5 text-xs text-ink-faint">
                        {kind}
                      </span>
                    </span>
                    <button
                      onClick={() => void run(() => removeInstallation(inst))}
                      disabled={busy}
                      className="text-xs font-medium text-danger transition hover:underline disabled:opacity-50"
                    >
                      Remove
                    </button>
                  </li>
                );
              })
            )}
          </ul>

          <div className="mb-1.5 mt-4 text-xs font-semibold tracking-wide text-ink-faint">
            ADD NEW INSTALLATIONS
          </div>
          <div className="mb-2 flex rounded-lg border border-line p-0.5">
            {visibleKinds.map((k) => (
              <button
                key={k.key}
                onClick={() => {
                  setAddKind(k.key);
                  setSearch("");
                }}
                data-add-kind={k.key}
                className={`flex-1 rounded-md px-2 py-1 text-xs font-medium transition ${
                  activeKind === k.key
                    ? "bg-accent text-white"
                    : "text-ink-soft hover:text-ink"
                }`}
              >
                {k.label}
              </button>
            ))}
          </div>

          {narrowingWarning && (
            <p
              data-narrowing-warning
              className="mb-2 rounded-lg bg-amber-50 px-3 py-2 text-xs text-amber-800 dark:bg-amber-950 dark:text-amber-200"
            >
              {narrowingWarning}
            </p>
          )}

          {(activeKind === "repo" ||
            activeKind === "team" ||
            activeKind === "bot") && (
            <>
              <input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder={
                  activeKind === "repo"
                    ? "Search repositories, or enter a URL…"
                    : activeKind === "team"
                      ? "Search teams…"
                      : "Search bots…"
                }
                disabled={busy}
                data-picker-search
                className="mb-1.5 w-full rounded-lg border border-line bg-canvas px-3 py-2 text-sm outline-none focus:border-accent"
              />
              <ul className="max-h-40 space-y-1 overflow-y-auto">
                {pickerCandidates.map((c) => (
                  <li key={c.key}>
                    <button
                      onClick={() => void add(c.inst)}
                      disabled={busy}
                      className="flex w-full items-center rounded-lg px-3 py-1.5 text-left text-sm transition hover:bg-accent-soft disabled:opacity-50"
                    >
                      <span className="min-w-0 flex-1 truncate">
                        {c.label}
                      </span>
                      {c.hint && (
                        <span className="text-xs text-ink-faint">
                          {c.hint}
                        </span>
                      )}
                    </button>
                  </li>
                ))}
                {freeRepoEntry && (
                  <li>
                    <button
                      onClick={() => {
                        void add({ kind: "repo", repo: freeRepoEntry });
                        setSearch("");
                      }}
                      disabled={busy}
                      data-add-free-repo
                      className="flex w-full items-center rounded-lg border border-dashed border-line px-3 py-1.5 text-left text-sm text-ink-soft transition hover:border-accent hover:text-ink disabled:opacity-50"
                    >
                      <span className="min-w-0 flex-1 truncate">
                        Install in “{freeRepoEntry}”
                      </span>
                      <span className="text-xs text-ink-faint">new repo</span>
                    </button>
                  </li>
                )}
                {pickerCandidates.length === 0 && !freeRepoEntry && (
                  <li className="px-3 py-1.5 text-sm text-ink-faint">
                    {search
                      ? `No ${
                          activeKind === "repo"
                            ? "repositories"
                            : activeKind === "team"
                              ? "teams"
                              : "bots"
                        } match`
                      : `Already installed for every ${
                          activeKind === "repo"
                            ? "known repository"
                            : activeKind
                        }`}
                  </li>
                )}
              </ul>
            </>
          )}

          {activeKind === "org" && (
            <button
              onClick={() => void add({ kind: "org" })}
              disabled={busy || view.everyone || orgRowPresent}
              className="w-full rounded-lg border border-dashed border-line px-3 py-2 text-left text-sm text-ink-soft transition hover:border-accent hover:text-ink disabled:opacity-50"
            >
              {view.everyone || orgRowPresent
                ? "Already installed for everyone"
                : "Install for everyone in this library"}
            </button>
          )}

          {activeKind === "user" &&
            (self ? (
              <button
                onClick={() => void add({ kind: "user" })}
                disabled={busy || personalAlready}
                className="w-full rounded-lg border border-dashed border-line px-3 py-2 text-left text-sm text-ink-soft transition hover:border-accent hover:text-ink disabled:opacity-50"
              >
                {personalAlready
                  ? `Already installed for you (${self})`
                  : `Install just for you (${self})`}
              </button>
            ) : (
              <p className="text-sm text-ink-faint">
                Set your email in Settings first — personal installs are
                scoped to you.
              </p>
            ))}
        </>
      )}
      {error && (
        <div className="mt-3 rounded-lg bg-danger-soft px-3 py-2 text-sm text-danger">
          {error}
        </div>
      )}
    </Modal>
  );
}
