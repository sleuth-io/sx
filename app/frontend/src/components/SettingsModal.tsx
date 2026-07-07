import { useEffect, useRef, useState } from "react";
import {
  AddLibrary,
  CancelSleuthLogin,
  ChooseLibraryIcon,
  ClearLibraryIcon,
  CompleteSleuthLogin,
  CreateGitRepo,
  DescribeLibraryRemoval,
  GetSettings,
  GitHubAccount,
  GitStatus,
  ListSyncFolders,
  PickDirectory,
  RemoveLibrary,
  SetLibraryActive,
  SetLibraryRepoTracking,
  StartSleuthLogin,
  SwitchProfile,
} from "../../wailsjs/go/main/App";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import type { main } from "../../wailsjs/go/models";
import ExtensionsSection from "./ExtensionsSection";
import Modal from "./Modal";
import RepoPicker, { CreateRepoCard, suggestRepoName } from "./RepoPicker";

const TYPE_LABELS: Record<string, string> = {
  git: "Team git repository",
  path: "Local folder",
  sleuth: "skills.new",
};

type AddKind = "path" | "folder" | "git" | "sleuth" | null;

/**
 * Settings: which libraries exist, which one is active, and adding or
 * removing them. Shared with the sx CLI — changes here change it
 * everywhere. onProfileChanged fires when the ACTIVE library changed
 * (caller closes settings and reloads); onLibrariesChanged when the list
 * changed but the active library didn't (caller refreshes in place).
 */
export default function SettingsModal({
  onClose,
  onProfileChanged,
  onLibrariesChanged,
}: {
  onClose: () => void;
  onProfileChanged: () => void;
  onLibrariesChanged?: () => void;
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
  const signInCancelled = useRef(false);
  const [gitStatus, setGitStatus] = useState<main.GitStatusInfo | null>(null);
  const gitUsable = gitStatus === null || gitStatus.available;
  const [syncFolders, setSyncFolders] = useState<main.SyncFolderOption[]>([]);
  // GitHub login for the create-a-repo offer: null = not looked up yet.
  const [ghLogin, setGhLogin] = useState<string | null>(null);

  // Remove-library confirmation state.
  const [removal, setRemoval] = useState<main.LibraryRemoval | null>(null);
  const [removeSource, setRemoveSource] = useState(false);
  const [removeError, setRemoveError] = useState("");

  // Which library's ⋯ menu is open.
  const [menuFor, setMenuFor] = useState<string | null>(null);
  useEffect(() => {
    if (menuFor === null) return;
    const onDown = (e: globalThis.MouseEvent) => {
      const root = (e.target as Element | null)?.closest?.(
        `[data-library-menu="${CSS.escape(menuFor)}"]`,
      );
      if (!root) setMenuFor(null);
    };
    window.addEventListener("mousedown", onDown);
    return () => window.removeEventListener("mousedown", onDown);
  }, [menuFor]);

  const load = () => {
    GetSettings()
      .then(setSettings)
      .catch((e) => setError(String(e)));
  };
  useEffect(() => {
    load();
    GitStatus()
      .then(setGitStatus)
      .catch(() => setGitStatus(null));
    ListSyncFolders()
      .then((folders) => setSyncFolders(folders ?? []))
      .catch(() => setSyncFolders([]));
    // Org icons resolve in the background; repaint rows when one lands.
    // (Unsubscribe removes only this listener, not other subscribers.)
    return EventsOn("library-icons-updated", load);
  }, []);

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
      // A shared folder is a plain path library; the sync app shares it.
      const kind = addKind === "folder" ? "path" : (addKind ?? "");
      await AddLibrary(newName, kind, newLocation, "");
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
    signInCancelled.current = false;
    try {
      const start = await StartSleuthLogin(newLocation);
      setLoginCode(start);
      // Waits for the browser authorization; server URL defaults inside.
      await CompleteSleuthLogin(newLocation, start.deviceCode, newName);
      onProfileChanged();
    } catch (e) {
      // A user-initiated cancel is not an error worth showing.
      if (!signInCancelled.current) setError(String(e));
      setLoginCode(null);
    } finally {
      setBusy("");
    }
  }

  function cancelAdd() {
    if (busy === "add" && loginCode) {
      signInCancelled.current = true;
      void CancelSleuthLogin();
    }
    setAddKind(null);
    setLoginCode(null);
    setError("");
  }

  function startAdd(kind: AddKind) {
    setAddKind(kind);
    setError("");
    setNewName("");
    setNewLocation(kind === "sleuth" ? "https://app.skills.new" : "");
    setLoginCode(null);
    if (kind === "git" && ghLogin === null) {
      GitHubAccount()
        .then(setGhLogin)
        .catch(() => setGhLogin(""));
    }
  }

  // Create a fresh private repo under the user's GitHub account and connect
  // it as the library in one step — the common case for a new git library.
  async function createRepoAndAdd() {
    setBusy("add");
    setError("");
    try {
      const repo = await CreateGitRepo(suggestRepoName(newName));
      await AddLibrary(newName, "git", repo.url, "");
      onProfileChanged();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy("");
    }
  }

  // A library's icon replaces the sx mark in the sidebar switcher.
  async function chooseIcon(name: string) {
    setError("");
    try {
      const icon = await ChooseLibraryIcon(name);
      if (!icon) return; // picker cancelled
      load();
      onLibrariesChanged?.();
    } catch (e) {
      setError(String(e));
    }
  }

  async function clearIcon(name: string) {
    setError("");
    try {
      await ClearLibraryIcon(name);
      load();
      onLibrariesChanged?.();
    } catch (e) {
      setError(String(e));
    }
  }

  // Multi-vault sync: any number of libraries can be in the active set;
  // Sync merges assets from all of them. Viewing/writing stays with the
  // current (default) library.
  async function toggleActive(p: main.ProfileInfo) {
    setError("");
    try {
      await SetLibraryActive(p.name, !p.active);
      load();
      onLibrariesChanged?.();
    } catch (e) {
      setError(String(e));
    }
  }

  // Repository views are opt-in per library — technical users want them,
  // everyone else shouldn't have to know they exist.
  async function toggleRepoTracking(name: string, enabled: boolean) {
    setError("");
    try {
      await SetLibraryRepoTracking(name, enabled);
      load();
      onLibrariesChanged?.();
    } catch (e) {
      setError(String(e));
    }
  }

  function openRemoval(name: string) {
    setError("");
    setRemoveError("");
    setRemoveSource(false);
    DescribeLibraryRemoval(name)
      .then(setRemoval)
      .catch((e) => setError(String(e)));
  }

  async function confirmRemoval() {
    if (!removal) return;
    setBusy("remove");
    setRemoveError("");
    try {
      await RemoveLibrary(removal.name, removeSource);
      const wasActive = removal.active;
      setRemoval(null);
      load();
      // Removing the active library changes the whole app's context;
      // removing any other one is just list housekeeping.
      if (wasActive) onProfileChanged();
      else onLibrariesChanged?.();
    } catch (e) {
      setRemoveError(String(e));
    } finally {
      setBusy("");
    }
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
            Shared with the <code className="font-mono">sx</code> command line —
            switching here switches everywhere.
          </p>
          <ul className="space-y-2">
            {settings.profiles.map((p) => (
              <li
                key={p.name}
                className={`flex items-center gap-3 rounded-xl border p-3 ${
                  p.default ? "border-accent bg-accent-soft/40" : "border-line"
                }`}
              >
                {p.icon ? (
                  <img
                    src={p.icon}
                    alt=""
                    className="h-8 w-8 shrink-0 rounded-lg border border-line object-cover"
                  />
                ) : (
                  <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-white/10 bg-gradient-to-b from-[#2e3138] to-[#15171b] text-[10px] font-bold text-[#8fa6ff]">
                    sx
                  </div>
                )}
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
                {!p.default && p.active && (
                  <span
                    className="shrink-0 rounded-full border border-line px-2 py-0.5 text-[11px] text-ink-faint"
                    title="Sync also installs this library's assets"
                  >
                    Synced
                  </span>
                )}
                {p.default ? (
                  <span className="shrink-0 rounded-full bg-accent px-2.5 py-0.5 text-[11px] font-medium text-white">
                    Current
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
                <div className="relative shrink-0" data-library-menu={p.name}>
                  <button
                    onClick={() =>
                      setMenuFor(menuFor === p.name ? null : p.name)
                    }
                    disabled={busy !== ""}
                    title={`Options for ${p.name}`}
                    aria-label={`Options for ${p.name}`}
                    aria-expanded={menuFor === p.name}
                    className={`rounded-lg p-1.5 transition hover:bg-canvas hover:text-ink disabled:opacity-50 ${
                      menuFor === p.name
                        ? "bg-canvas text-ink"
                        : "text-ink-faint"
                    }`}
                  >
                    <svg
                      aria-hidden="true"
                      className="h-3.5 w-3.5"
                      viewBox="0 0 16 16"
                      fill="currentColor"
                    >
                      <circle cx="3" cy="8" r="1.4" />
                      <circle cx="8" cy="8" r="1.4" />
                      <circle cx="13" cy="8" r="1.4" />
                    </svg>
                  </button>
                  {menuFor === p.name && (
                    <div className="absolute right-0 top-full z-40 mt-1 w-60 overflow-hidden rounded-xl border border-line bg-surface py-1 shadow-xl">
                      {p.type === "sleuth" ? (
                        <div className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm text-ink-faint">
                          <span className="w-3.5" />
                          <span className="flex-1">
                            <span className="block">Icon</span>
                            <span className="block text-xs">
                              Uses your skills.new organization's icon
                            </span>
                          </span>
                        </div>
                      ) : (
                        <>
                          <button
                            onClick={() => {
                              setMenuFor(null);
                              void chooseIcon(p.name);
                            }}
                            className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm text-ink-soft transition hover:bg-canvas hover:text-ink"
                          >
                            <span className="w-3.5" />
                            <span className="flex-1">
                              <span className="block">
                                {p.icon ? "Change icon…" : "Set icon…"}
                              </span>
                              <span className="block text-xs text-ink-faint">
                                Shown in the sidebar for this library
                              </span>
                            </span>
                          </button>
                          {p.icon && (
                            <button
                              onClick={() => {
                                setMenuFor(null);
                                void clearIcon(p.name);
                              }}
                              className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm text-ink-soft transition hover:bg-canvas hover:text-ink"
                            >
                              <span className="w-3.5" />
                              Remove icon
                            </button>
                          )}
                        </>
                      )}
                      <div className="mx-3 my-1 border-t border-line" />
                      <button
                        onClick={() => {
                          // Always-synced is a fact for the current
                          // library, not a togglable state.
                          if (p.default) return;
                          setMenuFor(null);
                          void toggleActive(p);
                        }}
                        disabled={p.default}
                        className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm text-ink-soft transition enabled:hover:bg-canvas enabled:hover:text-ink disabled:cursor-default disabled:opacity-70"
                      >
                        <span className="w-3.5 text-accent">
                          {p.active ? "✓" : ""}
                        </span>
                        <span className="flex-1">
                          <span className="block">Include in sync</span>
                          <span className="block text-xs text-ink-faint">
                            {p.default
                              ? "The current library is always synced"
                              : "Sync installs this library's assets too"}
                          </span>
                        </span>
                      </button>
                      <button
                        onClick={() => {
                          setMenuFor(null);
                          void toggleRepoTracking(p.name, !p.trackRepos);
                        }}
                        className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm text-ink-soft transition hover:bg-canvas hover:text-ink"
                      >
                        <span className="w-3.5 text-accent">
                          {p.trackRepos ? "✓" : ""}
                        </span>
                        <span className="flex-1">
                          <span className="block">Track repositories</span>
                          <span className="block text-xs text-ink-faint">
                            See which repos assets are scoped to
                          </span>
                        </span>
                      </button>
                      {settings.profiles.length > 1 && (
                        <>
                          <div className="mx-3 my-1 border-t border-line" />
                          <button
                            onClick={() => {
                              setMenuFor(null);
                              openRemoval(p.name);
                            }}
                            className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm text-ink-soft transition hover:bg-canvas hover:text-danger"
                          >
                            <span className="w-3.5" />
                            Remove…
                          </button>
                        </>
                      )}
                    </div>
                  )}
                </div>
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
                  label="Shared folder"
                  hint="Dropbox, Drive, OneDrive"
                  onClick={() => startAdd("folder")}
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
                    className={`rounded-lg border border-line bg-canvas px-3 py-2 text-sm outline-none focus:border-accent ${
                      addKind === "git" ? "min-w-0 flex-1" : "w-44"
                    }`}
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
                  {addKind === "folder" && (
                    <>
                      <input
                        value={newLocation}
                        onChange={(e) => setNewLocation(e.target.value)}
                        placeholder="Pick a synced folder…"
                        disabled={busy !== ""}
                        className="min-w-0 flex-1 rounded-lg border border-line bg-canvas px-3 py-2 text-sm outline-none focus:border-accent"
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
                  {addKind === "sleuth" && (
                    <input
                      value={newLocation}
                      onChange={(e) => setNewLocation(e.target.value)}
                      disabled={busy !== ""}
                      className="min-w-0 flex-1 rounded-lg border border-line bg-canvas px-3 py-2 text-sm outline-none focus:border-accent"
                    />
                  )}
                </div>
                {addKind === "folder" && syncFolders.length > 0 && (
                  <div className="flex flex-wrap gap-2">
                    {syncFolders.map((f) => (
                      <button
                        key={f.path}
                        type="button"
                        disabled={busy !== ""}
                        onClick={() => setNewLocation(f.suggested)}
                        className={`rounded-lg border px-3 py-1.5 text-xs transition hover:border-accent ${
                          newLocation === f.suggested
                            ? "border-accent text-ink"
                            : "border-line text-ink-soft"
                        }`}
                      >
                        {f.provider}
                      </button>
                    ))}
                  </div>
                )}
                {addKind === "git" && gitUsable && (
                  <>
                    {ghLogin && (
                      <>
                        <CreateRepoCard
                          title={
                            busy === "add"
                              ? "Creating repository…"
                              : newName.trim()
                                ? `Create ${ghLogin}/${suggestRepoName(newName)}`
                                : "Create a new repository"
                          }
                          subtitle={
                            newName.trim()
                              ? "New private GitHub repository, connected as this library"
                              : "Name the library above to see the suggested repository"
                          }
                          disabled={busy !== "" || !newName.trim()}
                          onCreate={() => void createRepoAndAdd()}
                        />
                        <div className="text-center text-[11px] text-ink-faint">
                          or connect an existing repository
                        </div>
                      </>
                    )}
                    <RepoPicker
                      value={newLocation}
                      onChange={setNewLocation}
                      disabled={busy !== ""}
                    />
                  </>
                )}
                {addKind === "git" && !gitUsable && (
                  <div className="rounded-lg bg-canvas px-3 py-2 text-xs text-ink-soft">
                    {gitStatus?.reason} A local folder or skills.new library
                    works without git.
                  </div>
                )}
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
                    onClick={cancelAdd}
                    disabled={busy !== "" && !(busy === "add" && loginCode)}
                    className="rounded-lg px-3 py-1.5 text-xs font-medium text-ink-faint transition hover:text-ink disabled:opacity-50"
                  >
                    Cancel
                  </button>
                  <button
                    type="submit"
                    disabled={
                      busy !== "" ||
                      !newName.trim() ||
                      (addKind === "git" && !gitUsable) ||
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
        </>
      )}
      <ExtensionsSection />

      {error && (
        <div className="mt-3 rounded-lg bg-danger-soft px-3 py-2 text-sm text-danger">
          {error}
        </div>
      )}

      {/* Remove-library confirmation. Deleting the source is opt-in and
          spelled out; plain removal just disconnects. */}
      {removal && (
        <div
          className="fixed inset-0 z-[70] flex items-center justify-center bg-black/50"
          onMouseDown={(e) => {
            if (e.target === e.currentTarget && busy !== "remove") {
              setRemoval(null);
            }
          }}
        >
          <div className="w-[440px] rounded-2xl border border-line bg-surface p-5 shadow-2xl">
            <div className="text-sm font-semibold">
              Remove “{removal.name}”?
            </div>
            <p className="mt-2 text-xs leading-relaxed text-ink-soft">
              This disconnects the library from the app and the{" "}
              <code className="font-mono">sx</code> command line.
              {removal.active && " Another library becomes active."}
              {!removeSource &&
                (removal.type === "sleuth"
                  ? " Your skills.new account and its assets are untouched."
                  : " Its assets stay where they are.")}
            </p>
            {removal.canDeleteSource && (
              <label className="mt-3 flex cursor-pointer items-start gap-2 rounded-lg border border-line px-3 py-2.5 transition hover:border-danger/50">
                <input
                  type="checkbox"
                  checked={removeSource}
                  onChange={(e) => setRemoveSource(e.target.checked)}
                  disabled={busy === "remove"}
                  className="mt-0.5 accent-[--color-danger]"
                />
                <span
                  className={`text-xs leading-relaxed ${removeSource ? "text-danger" : "text-ink-soft"}`}
                >
                  {removal.type === "git" ? (
                    <>
                      Also permanently delete{" "}
                      <span className="font-mono font-medium">
                        {removal.sourceLabel}
                      </span>{" "}
                      — this deletes it for everyone who uses it
                    </>
                  ) : (
                    <>
                      Also permanently delete the folder{" "}
                      <span className="font-mono font-medium">
                        {removal.sourceLabel}
                      </span>{" "}
                      and everything in it
                      {removal.sharedSource &&
                        ` — it syncs through ${removal.sourceProvider}, so this deletes it for everyone it's shared with`}
                    </>
                  )}
                </span>
              </label>
            )}
            {removeError && (
              <div className="mt-3 rounded-lg bg-danger-soft px-3 py-2 text-xs text-danger">
                {removeError}
              </div>
            )}
            <div className="mt-4 flex justify-end gap-2">
              <button
                onClick={() => setRemoval(null)}
                disabled={busy === "remove"}
                className="rounded-lg px-3 py-1.5 text-xs font-medium text-ink-faint transition hover:text-ink disabled:opacity-50"
              >
                Cancel
              </button>
              <button
                onClick={() => void confirmRemoval()}
                disabled={busy === "remove"}
                className="rounded-lg bg-danger px-4 py-1.5 text-xs font-medium text-white transition hover:opacity-90 disabled:opacity-50"
              >
                {busy === "remove"
                  ? "Removing…"
                  : removeSource
                    ? "Remove and delete"
                    : "Remove library"}
              </button>
            </div>
          </div>
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
