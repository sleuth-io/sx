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
 * can be removed, and anything the vault permits can be added; the
 * vault's RBAC is the gate and its errors surface verbatim. Changes
 * apply immediately. The caller supplies the read/write operations so
 * the same dialog serves assets and collections.
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
  { key: "repo", label: "Repo" },
  { key: "team", label: "Team" },
  { key: "bot", label: "Bot" },
  { key: "org", label: "Org" },
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
  const [repoInput, setRepoInput] = useState("");
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
  const teamCandidates = teams
    .filter((t) => !has((i) => i.kind === "team" && i.team === t.name))
    .filter((t) => !needle || t.name.toLowerCase().includes(needle));
  const botCandidates = bots.filter(
    (b) => !has((i) => i.kind === "bot" && i.bot === b),
  );
  const personalAlready = has(
    (i) => i.kind === "user" && (i.user ?? "").toLowerCase() === self.toLowerCase(),
  );

  return (
    <Modal title={title} onClose={onClose} width="w-[480px]">
      {!view ? (
        <div className="h-20 animate-pulse rounded-lg bg-canvas" />
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
            {ADD_KINDS.map((k) => (
              <button
                key={k.key}
                onClick={() => {
                  setAddKind(k.key);
                  setSearch("");
                }}
                data-add-kind={k.key}
                className={`flex-1 rounded-md px-2 py-1 text-xs font-medium transition ${
                  addKind === k.key
                    ? "bg-accent text-white"
                    : "text-ink-soft hover:text-ink"
                }`}
              >
                {k.label}
              </button>
            ))}
          </div>

          {addKind === "repo" && (
            <div className="flex gap-2">
              <input
                value={repoInput}
                onChange={(e) => setRepoInput(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && repoInput.trim()) {
                    void add({ kind: "repo", repo: repoInput.trim() });
                    setRepoInput("");
                  }
                }}
                list="share-repo-suggestions"
                placeholder="Repository URL (e.g. github.com/acme/api)…"
                disabled={busy}
                className="w-full rounded-lg border border-line bg-canvas px-3 py-2 text-sm outline-none focus:border-accent"
              />
              <datalist id="share-repo-suggestions">
                {repoSuggestions.map((r) => (
                  <option key={r} value={r} />
                ))}
              </datalist>
              <button
                onClick={() => {
                  void add({ kind: "repo", repo: repoInput.trim() });
                  setRepoInput("");
                }}
                disabled={busy || !repoInput.trim()}
                className="shrink-0 rounded-lg bg-accent px-3 py-2 text-sm font-medium text-white transition hover:opacity-90 disabled:opacity-50"
              >
                Add
              </button>
            </div>
          )}

          {addKind === "team" &&
            (teams.length > 0 ? (
              <>
                <input
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                  placeholder="Search teams…"
                  disabled={busy}
                  className="mb-1.5 w-full rounded-lg border border-line bg-canvas px-3 py-2 text-sm outline-none focus:border-accent"
                />
                <ul className="max-h-40 space-y-1 overflow-y-auto">
                  {teamCandidates.map((t) => (
                    <li key={t.name}>
                      <button
                        onClick={() => void add({ kind: "team", team: t.name })}
                        disabled={busy}
                        className="flex w-full items-center rounded-lg px-3 py-1.5 text-left text-sm transition hover:bg-accent-soft disabled:opacity-50"
                      >
                        <span className="min-w-0 flex-1 truncate">
                          {t.name}
                        </span>
                        <span className="text-xs text-ink-faint">
                          {(t.members ?? []).length}{" "}
                          {(t.members ?? []).length === 1
                            ? "member"
                            : "members"}
                        </span>
                      </button>
                    </li>
                  ))}
                  {teamCandidates.length === 0 && (
                    <li className="px-3 py-1.5 text-sm text-ink-faint">
                      {search
                        ? "No teams match"
                        : "Already installed for every team"}
                    </li>
                  )}
                </ul>
              </>
            ) : (
              <p className="text-sm text-ink-faint">
                No teams yet — create one from the sidebar to share with a
                specific group.
              </p>
            ))}

          {addKind === "bot" &&
            (botCandidates.length > 0 ? (
              <ul className="max-h-40 space-y-1 overflow-y-auto">
                {botCandidates.map((b) => (
                  <li key={b}>
                    <button
                      onClick={() => void add({ kind: "bot", bot: b })}
                      disabled={busy}
                      className="flex w-full items-center rounded-lg px-3 py-1.5 text-left text-sm transition hover:bg-accent-soft disabled:opacity-50"
                    >
                      {b}
                    </button>
                  </li>
                ))}
              </ul>
            ) : (
              <p className="text-sm text-ink-faint">
                {bots.length > 0
                  ? "Already installed for every bot."
                  : "No bots in this library — create them with the sx CLI (sx bot add)."}
              </p>
            ))}

          {addKind === "org" && (
            <button
              onClick={() => void add({ kind: "org" })}
              disabled={busy || view.everyone}
              className="w-full rounded-lg border border-dashed border-line px-3 py-2 text-left text-sm text-ink-soft transition hover:border-accent hover:text-ink disabled:opacity-50"
            >
              {view.everyone
                ? "Already installed for everyone (the default)"
                : "Install for everyone in this library"}
              {!view.everyone && (
                <span className="ml-1.5 text-xs text-ink-faint">
                  replaces every row above
                </span>
              )}
            </button>
          )}

          {addKind === "user" &&
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
