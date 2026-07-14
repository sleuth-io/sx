import { useState } from "react";
import { SetBotTeam, UpdateBotDescription } from "../../wailsjs/go/main/App";
import type { main } from "../../wailsjs/go/models";
import Modal from "./Modal";

/**
 * Manage one bot: its description and which teams it belongs to (team
 * membership gives the bot the team's skills and repo context). Skills
 * are installed into a bot from any asset's Share… panel, or by dragging
 * an asset onto the bot in the sidebar.
 */
export default function BotModal({
  bot,
  teams,
  onClose,
  onChanged,
}: {
  bot: main.BotInfo;
  teams: main.TeamInfo[];
  onClose: () => void;
  onChanged: () => void;
}) {
  const [description, setDescription] = useState(bot.description ?? "");
  const [botTeams, setBotTeams] = useState<string[]>(bot.teams ?? []);
  const [newTeam, setNewTeam] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [savedDescription, setSavedDescription] = useState(
    bot.description ?? "",
  );

  const joinable = teams
    .map((t) => t.name)
    .filter((name) => !botTeams.includes(name));

  async function saveDescription() {
    if (description === savedDescription) return;
    setBusy(true);
    setError("");
    try {
      await UpdateBotDescription(bot.name, description);
      setSavedDescription(description);
      onChanged();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }

  // Escape/backdrop dismissal doesn't reliably blur the description
  // input first — flush an unsaved edit on the way out so closing the
  // modal never silently drops it.
  function close() {
    void saveDescription();
    onClose();
  }

  async function addTeam() {
    const team = newTeam.trim();
    if (!team) return;
    setBusy(true);
    setError("");
    try {
      await SetBotTeam(bot.name, team, true);
      setBotTeams((t) => [...new Set([...t, team])].sort());
      setNewTeam("");
      onChanged();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }

  async function removeTeam(team: string) {
    setBusy(true);
    setError("");
    try {
      await SetBotTeam(bot.name, team, false);
      setBotTeams((t) => t.filter((x) => x !== team));
      onChanged();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal title={`Bot: ${bot.name}`} onClose={close} width="w-[480px]">
      <div className="mb-2 text-xs font-semibold tracking-wide text-ink-faint">
        DESCRIPTION
      </div>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          void saveDescription();
        }}
      >
        <input
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          onBlur={() => void saveDescription()}
          placeholder="What this bot does"
          disabled={busy}
          className="w-full rounded-lg border border-line bg-canvas px-3 py-2 text-sm outline-none focus:border-accent"
        />
      </form>

      <div className="mb-2 mt-5 text-xs font-semibold tracking-wide text-ink-faint">
        TEAMS
      </div>
      <p className="mb-2 text-xs text-ink-faint">
        The bot gets every skill shared with these teams, plus anything
        installed on it directly.
      </p>
      {botTeams.length === 0 ? (
        <div className="rounded-lg border border-dashed border-line px-3 py-3 text-sm text-ink-faint">
          Not on any team yet.
        </div>
      ) : (
        <ul className="max-h-48 space-y-1 overflow-y-auto">
          {botTeams.map((team) => (
            <li
              key={team}
              className="flex items-center gap-2 rounded-lg px-2 py-1.5 hover:bg-canvas"
            >
              <span className="min-w-0 flex-1 truncate text-sm">{team}</span>
              <button
                onClick={() => void removeTeam(team)}
                disabled={busy}
                title="Remove bot from team"
                className="shrink-0 rounded px-1.5 text-sm text-ink-faint transition hover:text-danger"
              >
                ✕
              </button>
            </li>
          ))}
        </ul>
      )}

      {joinable.length > 0 && (
        <form
          className="mt-3 flex gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            void addTeam();
          }}
        >
          <select
            value={newTeam}
            onChange={(e) => setNewTeam(e.target.value)}
            disabled={busy}
            className="h-[38px] min-w-0 flex-1 rounded-lg border border-line bg-canvas py-0 pl-3 pr-7 text-sm outline-none focus:border-accent"
          >
            <option value="">Add to a team…</option>
            {joinable.map((name) => (
              <option key={name} value={name}>
                {name}
              </option>
            ))}
          </select>
          <button
            type="submit"
            disabled={busy || !newTeam.trim()}
            className="rounded-lg bg-accent px-4 py-2 text-sm font-medium text-white transition hover:opacity-90 disabled:opacity-50"
          >
            Add
          </button>
        </form>
      )}

      {error && (
        <div className="mt-3 rounded-lg bg-danger-soft px-3 py-2 text-sm text-danger">
          {error}
        </div>
      )}
    </Modal>
  );
}
