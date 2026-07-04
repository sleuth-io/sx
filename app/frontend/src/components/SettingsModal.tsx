import { useEffect, useState } from "react";
import {
  AddLibrary,
  CompleteSleuthLogin,
  GetSettings,
  PickDirectory,
  StartSleuthLogin,
  SwitchProfile,
} from "../../wailsjs/go/main/App";
import type { main } from "../../wailsjs/go/models";
import Modal from "./Modal";

const TYPE_LABELS: Record<string, string> = {
  git: "Team git repository",
  path: "Local folder",
  sleuth: "skills.new",
};

type AddKind = "path" | "git" | "sleuth" | null;

/**
 * Settings: which libraries exist, which one is active, and adding new
 * ones. Shared with the sx CLI — changes here change it everywhere.
 */
export default function SettingsModal({
  onClose,
  onProfileChanged,
}: {
  onClose: () => void;
  onProfileChanged: () => void;
}) {
  const [settings, setSettings] = useState<main.Settings | null>(null);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");

  // Add-library form state
  const [addKind, setAddKind] = useState<AddKind>(null);
  const [newName, setNewName] = useState("");
  const [newLocation, setNewLocation] = useState("");
  const [loginCode, setLoginCode] = useState<main.SleuthLoginStart | null>(
    null,
  );

  const load = () => {
    GetSettings()
      .then(setSettings)
      .catch((e) => setError(String(e)));
  };
  useEffect(load, []);

  async function switchTo(name: string) {
    setBusy(name);
    setError("");
    try {
      await SwitchProfile(name);
      load();
      onProfileChanged();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy("");
    }
  }

  async function addLibrary() {
    setBusy("add");
    setError("");
    try {
      await AddLibrary(newName, addKind ?? "", newLocation, "");
      onProfileChanged();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy("");
    }
  }

  async function signIn() {
    setBusy("add");
    setError("");
    try {
      const start = await StartSleuthLogin(newLocation);
      setLoginCode(start);
      // Waits for the browser authorization; server URL defaults inside.
      await CompleteSleuthLogin(newLocation, start.deviceCode, newName);
      onProfileChanged();
    } catch (e) {
      setError(String(e));
      setLoginCode(null);
    } finally {
      setBusy("");
    }
  }

  function startAdd(kind: AddKind) {
    setAddKind(kind);
    setError("");
    setNewName("");
    setNewLocation(kind === "sleuth" ? "https://app.skills.new" : "");
    setLoginCode(null);
  }

  return (
    <Modal title="Settings" onClose={onClose} width="w-[540px]">
      {!settings ? (
        <div className="h-20 animate-pulse rounded-lg bg-canvas" />
      ) : (
        <>
          <div className="mb-1 text-xs font-semibold tracking-wide text-ink-faint">
            LIBRARIES
          </div>
          <p className="mb-3 text-xs text-ink-faint">
            Shared with the <code className="font-mono">sx</code> command line
            — switching here switches everywhere.
          </p>
          <ul className="space-y-2">
            {settings.profiles.map((p) => (
              <li
                key={p.name}
                className={`flex items-center gap-3 rounded-xl border p-3 ${
                  p.default ? "border-accent bg-accent-soft/40" : "border-line"
                }`}
              >
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-semibold">{p.name}</span>
                    <span className="text-xs text-ink-faint">
                      {TYPE_LABELS[p.type] ?? p.type}
                    </span>
                  </div>
                  <div
                    className="truncate text-xs text-ink-soft"
                    title={p.location}
                  >
                    {p.location}
                  </div>
                  {p.identity && (
                    <div className="text-xs text-ink-faint">
                      {p.type === "sleuth" ? "Signed in as " : ""}
                      {p.identity}
                    </div>
                  )}
                </div>
                {p.default ? (
                  <span className="shrink-0 rounded-full bg-accent px-2.5 py-0.5 text-[11px] font-medium text-white">
                    Active
                  </span>
                ) : (
                  <button
                    onClick={() => void switchTo(p.name)}
                    disabled={busy !== ""}
                    className="shrink-0 rounded-lg border border-line px-3 py-1.5 text-xs font-medium text-ink-soft transition hover:border-accent hover:text-ink disabled:opacity-50"
                  >
                    {busy === p.name ? "Switching…" : "Use this"}
                  </button>
                )}
              </li>
            ))}
            {settings.profiles.length === 0 && (
              <li className="rounded-xl border border-dashed border-line p-4 text-sm text-ink-faint">
                No libraries configured yet.
              </li>
            )}
          </ul>

          {/* Add a library */}
          <div className="mt-4 border-t border-line pt-3">
            <div className="mb-2 text-xs font-semibold tracking-wide text-ink-faint">
              ADD A LIBRARY
            </div>
            {addKind === null ? (
              <div className="flex gap-2">
                <AddChoice
                  label="Local folder"
                  hint="Just on this computer"
                  onClick={() => startAdd("path")}
                />
                <AddChoice
                  label="Git repository"
                  hint="Shared with a team"
                  onClick={() => startAdd("git")}
                />
                <AddChoice
                  label="skills.new"
                  hint="Sign in to your org"
                  onClick={() => startAdd("sleuth")}
                />
              </div>
            ) : (
              <form
                className="space-y-2.5"
                onSubmit={(e) => {
                  e.preventDefault();
                  if (addKind === "sleuth") void signIn();
                  else void addLibrary();
                }}
              >
                <div className="flex items-center gap-2">
                  <input
                    autoFocus
                    value={newName}
                    onChange={(e) => setNewName(e.target.value)}
                    placeholder="Name (e.g. personal, acme)"
                    disabled={busy !== ""}
                    className="w-44 rounded-lg border border-line bg-canvas px-3 py-2 text-sm outline-none focus:border-accent"
                  />
                  {addKind === "path" && (
                    <>
                      <input
                        value={newLocation}
                        readOnly
                        placeholder="Choose a folder…"
                        className="min-w-0 flex-1 rounded-lg border border-line bg-canvas px-3 py-2 text-sm text-ink-soft outline-none"
                      />
                      <button
                        type="button"
                        disabled={busy !== ""}
                        onClick={() =>
                          PickDirectory()
                            .then((dir) => dir && setNewLocation(dir))
                            .catch(() => {})
                        }
                        className="shrink-0 rounded-lg border border-line px-3 py-2 text-sm text-ink-soft transition hover:border-accent hover:text-ink"
                      >
                        Browse…
                      </button>
                    </>
                  )}
                  {addKind === "git" && (
                    <input
                      value={newLocation}
                      onChange={(e) => setNewLocation(e.target.value)}
                      placeholder="https://github.com/acme/skills.git"
                      disabled={busy !== ""}
                      className="min-w-0 flex-1 rounded-lg border border-line bg-canvas px-3 py-2 text-sm outline-none focus:border-accent"
                    />
                  )}
                  {addKind === "sleuth" && (
                    <input
                      value={newLocation}
                      onChange={(e) => setNewLocation(e.target.value)}
                      disabled={busy !== ""}
                      className="min-w-0 flex-1 rounded-lg border border-line bg-canvas px-3 py-2 text-sm outline-none focus:border-accent"
                    />
                  )}
                </div>
                {loginCode && (
                  <div className="rounded-lg bg-accent-soft px-3 py-2 text-xs">
                    Your browser opened — confirm the code{" "}
                    <span className="font-mono font-semibold">
                      {loginCode.userCode}
                    </span>{" "}
                    to finish signing in.
                  </div>
                )}
                <div className="flex justify-end gap-2">
                  <button
                    type="button"
                    onClick={() => setAddKind(null)}
                    disabled={busy !== ""}
                    className="rounded-lg px-3 py-1.5 text-xs font-medium text-ink-faint transition hover:text-ink"
                  >
                    Cancel
                  </button>
                  <button
                    type="submit"
                    disabled={
                      busy !== "" ||
                      !newName.trim() ||
                      (addKind !== "sleuth" && !newLocation.trim())
                    }
                    className="rounded-lg bg-accent px-4 py-1.5 text-xs font-medium text-white transition hover:opacity-90 disabled:opacity-50"
                  >
                    {busy === "add"
                      ? addKind === "sleuth"
                        ? "Waiting for sign-in…"
                        : "Connecting…"
                      : addKind === "sleuth"
                        ? "Sign in…"
                        : "Add library"}
                  </button>
                </div>
              </form>
            )}
          </div>

          <div className="mt-4 border-t border-line pt-3 text-xs text-ink-faint">
            Configuration file:{" "}
            <code className="break-all font-mono">{settings.configPath}</code>
          </div>
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

function AddChoice({
  label,
  hint,
  onClick,
}: {
  label: string;
  hint: string;
  onClick: () => void;
}) {
  return (
    <button
      onClick={onClick}
      className="flex-1 rounded-xl border border-dashed border-line px-3 py-2.5 text-left transition hover:border-accent"
    >
      <div className="text-sm font-medium">{label}</div>
      <div className="text-xs text-ink-faint">{hint}</div>
    </button>
  );
}
