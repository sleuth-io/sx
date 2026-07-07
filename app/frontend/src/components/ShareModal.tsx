import { useEffect, useState } from "react";
import type { main } from "../../wailsjs/go/models";
import Modal from "./Modal";

/**
 * Manage who receives an asset or a whole collection. Changes apply
 * immediately: remove a team, add a team (searchable), or return it to
 * everyone in the library. The caller supplies the read/write operations
 * so the same dialog serves assets and collections.
 */
export default function ShareModal({
  title,
  teams,
  getSharing,
  setTeamShared,
  shareEveryone,
  onClose,
  onChanged,
}: {
  title: string;
  teams: main.TeamInfo[];
  getSharing: () => Promise<main.AssetSharing>;
  setTeamShared: (team: string, shared: boolean) => Promise<void>;
  shareEveryone: () => Promise<void>;
  onClose: () => void;
  onChanged: () => void;
}) {
  const [sharing, setSharing] = useState<main.AssetSharing | null>(null);
  const [search, setSearch] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const refresh = () =>
    getSharing()
      .then(setSharing)
      .catch((e) => setError(String(e)));
  useEffect(() => {
    void refresh();
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

  const sharedTeams = sharing?.teams ?? [];
  const candidates = teams
    .filter((t) => !sharedTeams.includes(t.name))
    .filter(
      (t) =>
        !search.trim() ||
        t.name.toLowerCase().includes(search.trim().toLowerCase()),
    );

  return (
    <Modal title={title} onClose={onClose} width="w-[460px]">
      {!sharing ? (
        <div className="h-20 animate-pulse rounded-lg bg-canvas" />
      ) : (
        <>
          <div className="mb-1.5 text-xs font-semibold tracking-wide text-ink-faint">
            CURRENTLY SHARED WITH
          </div>
          <ul className="space-y-1.5">
            {sharing.everyone ? (
              <li className="flex items-center rounded-lg border border-line px-3 py-2">
                <span className="flex-1 text-sm">
                  Everyone in this library
                </span>
                <span className="text-xs text-ink-faint">default</span>
              </li>
            ) : sharedTeams.length === 0 ? (
              <li className="px-1 text-xs text-ink-faint">
                Sharing is mixed — pick a team below to share everything
                with it.
              </li>
            ) : (
              sharedTeams.map((team) => (
                <li
                  key={team}
                  className="flex items-center rounded-lg border border-line px-3 py-2"
                >
                  <span className="min-w-0 flex-1 truncate text-sm">
                    {team}
                    <span className="ml-1.5 text-xs text-ink-faint">
                      team
                    </span>
                  </span>
                  <button
                    onClick={() => void run(() => setTeamShared(team, false))}
                    disabled={busy}
                    className="text-xs font-medium text-danger transition hover:underline disabled:opacity-50"
                  >
                    Remove
                  </button>
                </li>
              ))
            )}
            {sharing.other > 0 && (
              <li className="px-1 text-xs text-ink-faint">
                +{sharing.other} more{" "}
                {sharing.other === 1 ? "place" : "places"} (repos, bots, …) —
                managed with the sx CLI
              </li>
            )}
          </ul>

          <div className="mb-1.5 mt-4 text-xs font-semibold tracking-wide text-ink-faint">
            SHARE WITH
          </div>
          {!sharing.everyone && (
            <button
              onClick={() => void run(shareEveryone)}
              disabled={busy}
              className="mb-2 w-full rounded-lg border border-dashed border-line px-3 py-2 text-left text-sm text-ink-soft transition hover:border-accent hover:text-ink disabled:opacity-50"
            >
              Everyone in this library
              <span className="ml-1.5 text-xs text-ink-faint">
                replaces the team list
              </span>
            </button>
          )}
          {teams.length > 0 ? (
            <>
              <input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="Search teams…"
                disabled={busy}
                className="mb-1.5 w-full rounded-lg border border-line bg-canvas px-3 py-2 text-sm outline-none focus:border-accent"
              />
              <ul className="max-h-44 space-y-1 overflow-y-auto">
                {candidates.map((t) => (
                  <li key={t.name}>
                    <button
                      onClick={() =>
                        void run(() => setTeamShared(t.name, true))
                      }
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
                {candidates.length === 0 && (
                  <li className="px-3 py-1.5 text-sm text-ink-faint">
                    {search
                      ? "No teams match"
                      : "Already shared with every team"}
                  </li>
                )}
              </ul>
            </>
          ) : (
            <p className="text-sm text-ink-faint">
              No teams yet — create one from the sidebar to share with a
              specific group.
            </p>
          )}
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
