import { useMemo, useState } from "react";
import {
  AddTeamMember,
  RemoveTeamMember,
} from "../../wailsjs/go/main/App";
import type { main } from "../../wailsjs/go/models";
import Modal from "./Modal";

/** Manage one team: its members, and who's an admin. */
export default function TeamModal({
  team,
  onClose,
  onChanged,
}: {
  team: main.TeamInfo;
  onClose: () => void;
  onChanged: () => void;
}) {
  const [members, setMembers] = useState<string[]>(team.members ?? []);
  const [admins, setAdmins] = useState<string[]>(team.admins ?? []);
  const [newEmail, setNewEmail] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const adminSet = useMemo(() => new Set(admins), [admins]);

  async function add() {
    const email = newEmail.trim();
    if (!email) return;
    setBusy(true);
    setError("");
    try {
      await AddTeamMember(team.name, email, false);
      setMembers((m) => [...new Set([...m, email.toLowerCase()])].sort());
      setNewEmail("");
      onChanged();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }

  async function remove(email: string) {
    setBusy(true);
    setError("");
    try {
      await RemoveTeamMember(team.name, email);
      setMembers((m) => m.filter((x) => x !== email));
      setAdmins((a) => a.filter((x) => x !== email));
      onChanged();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }

  async function setAdmin(email: string, admin: boolean) {
    setBusy(true);
    setError("");
    try {
      await AddTeamMember(team.name, email, admin);
      setAdmins((a) =>
        admin ? [...new Set([...a, email])].sort() : a.filter((x) => x !== email),
      );
      onChanged();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal title={`Team: ${team.name}`} onClose={onClose} width="w-[480px]">
      <div className="mb-2 text-xs font-semibold tracking-wide text-ink-faint">
        MEMBERS
      </div>
      {members.length === 0 ? (
        <div className="rounded-lg border border-dashed border-line px-3 py-3 text-sm text-ink-faint">
          No members yet — add teammates below.
        </div>
      ) : (
        <ul className="max-h-64 space-y-1 overflow-y-auto">
          {members.map((email) => (
            <li
              key={email}
              className="flex items-center gap-2 rounded-lg px-2 py-1.5 hover:bg-canvas"
            >
              <span className="min-w-0 flex-1 truncate text-sm">{email}</span>
              <button
                onClick={() => void setAdmin(email, !adminSet.has(email))}
                disabled={busy}
                title={
                  adminSet.has(email)
                    ? "Remove admin role"
                    : "Make team admin"
                }
                className={`shrink-0 rounded-full px-2 py-0.5 text-[11px] font-medium transition ${
                  adminSet.has(email)
                    ? "bg-accent text-white"
                    : "border border-line text-ink-faint hover:text-ink"
                }`}
              >
                admin
              </button>
              <button
                onClick={() => void remove(email)}
                disabled={busy}
                title="Remove from team"
                className="shrink-0 rounded px-1.5 text-sm text-ink-faint transition hover:text-danger"
              >
                ✕
              </button>
            </li>
          ))}
        </ul>
      )}

      <form
        className="mt-3 flex gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          void add();
        }}
      >
        <input
          value={newEmail}
          onChange={(e) => setNewEmail(e.target.value)}
          placeholder="teammate@company.com"
          type="email"
          disabled={busy}
          className="min-w-0 flex-1 rounded-lg border border-line bg-canvas px-3 py-2 text-sm outline-none focus:border-accent"
        />
        <button
          type="submit"
          disabled={busy || !newEmail.trim()}
          className="rounded-lg bg-accent px-4 py-2 text-sm font-medium text-white transition hover:opacity-90 disabled:opacity-50"
        >
          Add
        </button>
      </form>

      {error && (
        <div className="mt-3 rounded-lg bg-danger-soft px-3 py-2 text-sm text-danger">
          {error}
        </div>
      )}
    </Modal>
  );
}
