import { useEffect, useState } from "react";
import {
  CompleteSleuthLogin,
  HasIdentity,
  ListSyncFolders,
  PickDirectory,
  SetupGitVault,
  SetupLocalVault,
  SetupSharedFolderVault,
  StartSleuthLogin,
} from "../../wailsjs/go/main/App";
import type { main } from "../../wailsjs/go/models";

type Choice = "solo" | "folder" | "team" | "sleuth";

const EMAIL_SHAPE = /^[^@\s]+@[^@\s]+\.[^@\s]+$/;

/**
 * First-launch screen. One decision, framed in plain language: where does
 * your library live? No vault/manifest/scope vocabulary. When the machine
 * has no git identity, a single email field appears — changes to a shared
 * library need a name on them.
 */
export default function Onboarding({ onDone }: { onDone: () => void }) {
  const [choice, setChoice] = useState<Choice | null>(null);
  const [gitUrl, setGitUrl] = useState("");
  const [email, setEmail] = useState("");
  const [needsEmail, setNeedsEmail] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  // Shared-folder path.
  const [syncFolders, setSyncFolders] = useState<main.SyncFolderOption[]>([]);
  const [folderPath, setFolderPath] = useState("");

  // skills.new sign-in.
  const [signIn, setSignIn] = useState<main.SleuthLoginStart | null>(null);

  useEffect(() => {
    HasIdentity()
      .then((has) => setNeedsEmail(!has))
      // If the check itself fails, asking for an email is the safe default.
      .catch(() => setNeedsEmail(true));
    ListSyncFolders()
      .then((folders) => setSyncFolders(folders ?? []))
      .catch(() => setSyncFolders([]));
  }, []);

  const emailOK = !needsEmail || EMAIL_SHAPE.test(email.trim());

  async function pickSolo() {
    if (!emailOK) return;
    setBusy(true);
    setError("");
    try {
      await SetupLocalVault(email.trim());
      onDone();
    } catch (e) {
      setError(String(e));
      setBusy(false);
    }
  }

  async function connectTeam() {
    if (!gitUrl.trim() || !emailOK) return;
    setBusy(true);
    setError("");
    try {
      await SetupGitVault(gitUrl.trim(), email.trim());
      onDone();
    } catch (e) {
      setError(String(e));
      setBusy(false);
    }
  }

  async function connectSharedFolder() {
    if (!folderPath.trim() || !emailOK) return;
    setBusy(true);
    setError("");
    try {
      await SetupSharedFolderVault(folderPath.trim(), email.trim());
      onDone();
    } catch (e) {
      setError(String(e));
      setBusy(false);
    }
  }

  async function browseForFolder() {
    try {
      const dir = await PickDirectory();
      if (dir) setFolderPath(dir);
    } catch {
      // Cancelled picker is not an error.
    }
  }

  async function signInToSleuth() {
    setChoice("sleuth");
    setBusy(true);
    setError("");
    setSignIn(null);
    try {
      const start = await StartSleuthLogin("");
      setSignIn(start);
      // Waits for the user to approve in the browser.
      await CompleteSleuthLogin("", start.deviceCode, "skills-new");
      onDone();
    } catch (e) {
      setError(String(e));
      setSignIn(null);
      setBusy(false);
    }
  }

  return (
    <div className="h-full flex flex-col bg-canvas">
      <div className="titlebar-drag h-10 shrink-0" />
      <div className="flex-1 flex items-center justify-center px-8">
        <div className="w-full max-w-md">
          <div className="mb-8 text-center">
            <div className="mx-auto mb-5 flex h-12 w-12 items-center justify-center rounded-xl bg-accent text-xl font-semibold text-white">
              sx
            </div>
            <h1 className="text-2xl font-semibold tracking-tight">
              Your library for AI&nbsp;assets
            </h1>
            <p className="mt-2 text-sm text-ink-soft">
              Skills, rules, and commands your AI tools can use — kept in one
              place, shared with the people who need them.
            </p>
          </div>

          {needsEmail && (
            <label className="mb-4 block">
              <span className="mb-1 block text-xs font-medium text-ink-soft">
                Your email
              </span>
              <input
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                placeholder="you@company.com"
                disabled={busy}
                className="w-full rounded-lg border border-line bg-surface px-3 py-2 text-sm outline-none focus:border-accent"
              />
              <span className="mt-1 block text-xs text-ink-faint">
                Changes you publish are signed with it.
              </span>
            </label>
          )}

          <div className="space-y-3">
            <button
              onClick={() => {
                setChoice("solo");
                void pickSolo();
              }}
              disabled={busy || !emailOK}
              className="group w-full rounded-xl border border-line bg-surface p-4 text-left transition hover:border-accent disabled:opacity-60"
            >
              <div className="text-sm font-semibold">Just me</div>
              <div className="mt-0.5 text-sm text-ink-soft">
                Keep a private library on this computer. You can share it
                later.
              </div>
            </button>

            <div
              className={`rounded-xl border bg-surface p-4 transition ${
                choice === "folder" ? "border-accent" : "border-line"
              }`}
            >
              <button
                onClick={() => setChoice("folder")}
                disabled={busy}
                className="w-full text-left"
              >
                <div className="text-sm font-semibold">
                  My team — shared folder
                </div>
                <div className="mt-0.5 text-sm text-ink-soft">
                  Share through a folder you already sync — Dropbox, Google
                  Drive, OneDrive, iCloud. No accounts, no setup.
                </div>
              </button>
              {choice === "folder" && (
                <div className="mt-3 space-y-2">
                  {syncFolders.length > 0 && (
                    <div className="flex flex-wrap gap-2">
                      {syncFolders.map((f) => (
                        <button
                          key={f.path}
                          type="button"
                          disabled={busy}
                          onClick={() => setFolderPath(f.suggested)}
                          className={`rounded-lg border px-3 py-1.5 text-xs transition hover:border-accent ${
                            folderPath === f.suggested
                              ? "border-accent text-ink"
                              : "border-line text-ink-soft"
                          }`}
                        >
                          {f.provider}
                        </button>
                      ))}
                    </div>
                  )}
                  <form
                    className="flex gap-2"
                    onSubmit={(e) => {
                      e.preventDefault();
                      void connectSharedFolder();
                    }}
                  >
                    <input
                      autoFocus
                      value={folderPath}
                      onChange={(e) => setFolderPath(e.target.value)}
                      placeholder="Pick a synced folder…"
                      className="min-w-0 flex-1 rounded-lg border border-line bg-canvas px-3 py-2 text-sm outline-none focus:border-accent"
                      disabled={busy}
                    />
                    <button
                      type="button"
                      onClick={() => void browseForFolder()}
                      disabled={busy}
                      className="rounded-lg border border-line px-3 py-2 text-sm text-ink-soft transition hover:border-accent"
                    >
                      Browse…
                    </button>
                    <button
                      type="submit"
                      disabled={busy || !folderPath.trim() || !emailOK}
                      className="rounded-lg bg-accent px-4 py-2 text-sm font-medium text-white transition hover:opacity-90 disabled:opacity-50"
                    >
                      {busy ? "Connecting…" : "Connect"}
                    </button>
                  </form>
                  <p className="text-xs text-ink-faint">
                    Teammates connect by pointing sx at the same folder once
                    it syncs to their computer.
                  </p>
                </div>
              )}
            </div>

            <div
              className={`rounded-xl border bg-surface p-4 transition ${
                choice === "team" ? "border-accent" : "border-line"
              }`}
            >
              <button
                onClick={() => setChoice("team")}
                disabled={busy}
                className="w-full text-left"
              >
                <div className="text-sm font-semibold">
                  My team — git repository
                </div>
                <div className="mt-0.5 text-sm text-ink-soft">
                  Connect to the library your team already shares in a git
                  repository.
                </div>
              </button>
              {choice === "team" && (
                <form
                  className="mt-3 flex gap-2"
                  onSubmit={(e) => {
                    e.preventDefault();
                    void connectTeam();
                  }}
                >
                  <input
                    autoFocus
                    value={gitUrl}
                    onChange={(e) => setGitUrl(e.target.value)}
                    placeholder="https://github.com/acme/skills.git"
                    className="min-w-0 flex-1 rounded-lg border border-line bg-canvas px-3 py-2 text-sm outline-none focus:border-accent"
                    disabled={busy}
                  />
                  <button
                    type="submit"
                    disabled={busy || !gitUrl.trim() || !emailOK}
                    className="rounded-lg bg-accent px-4 py-2 text-sm font-medium text-white transition hover:opacity-90 disabled:opacity-50"
                  >
                    {busy ? "Connecting…" : "Connect"}
                  </button>
                </form>
              )}
            </div>

            <div
              className={`rounded-xl border bg-surface p-4 transition ${
                choice === "sleuth" ? "border-accent" : "border-line"
              }`}
            >
              <button
                onClick={() => void signInToSleuth()}
                disabled={busy}
                className="w-full text-left"
              >
                <div className="text-sm font-semibold">skills.new</div>
                <div className="mt-0.5 text-sm text-ink-soft">
                  Company-managed libraries with sign-in, teams, and usage
                  insights.
                </div>
              </button>
              {choice === "sleuth" && signIn && (
                <div className="mt-3 rounded-lg bg-canvas px-4 py-3">
                  <div className="text-sm text-ink-soft">
                    {signIn.browserOpened
                      ? "We opened your browser — approve the sign-in there."
                      : `Open ${signIn.verificationUri} and enter this code:`}
                  </div>
                  <div className="mt-2 font-mono text-lg font-semibold tracking-widest">
                    {signIn.userCode}
                  </div>
                  <div className="mt-2 flex items-center gap-2 text-xs text-ink-faint">
                    <span className="inline-block h-2 w-2 animate-pulse rounded-full bg-accent" />
                    Waiting for approval…
                  </div>
                </div>
              )}
            </div>
          </div>

          {error && (
            <div className="mt-4 rounded-lg bg-danger-soft px-4 py-3 text-sm text-danger">
              {error}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
