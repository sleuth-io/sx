import { useState } from "react";
import { SetupGitVault, SetupLocalVault } from "../../wailsjs/go/main/App";

type Choice = "solo" | "team";

/**
 * First-launch screen. One decision, framed in plain language: where does
 * your library live? No vault/manifest/scope vocabulary.
 */
export default function Onboarding({ onDone }: { onDone: () => void }) {
  const [choice, setChoice] = useState<Choice | null>(null);
  const [gitUrl, setGitUrl] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function pickSolo() {
    setBusy(true);
    setError("");
    try {
      await SetupLocalVault();
      onDone();
    } catch (e) {
      setError(String(e));
      setBusy(false);
    }
  }

  async function connectTeam() {
    if (!gitUrl.trim()) return;
    setBusy(true);
    setError("");
    try {
      await SetupGitVault(gitUrl.trim());
      onDone();
    } catch (e) {
      setError(String(e));
      setBusy(false);
    }
  }

  return (
    <div className="h-full flex flex-col bg-canvas">
      <div className="titlebar-drag h-10 shrink-0" />
      <div className="flex-1 flex items-center justify-center px-8">
        <div className="w-full max-w-md">
          <div className="mb-10 text-center">
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

          <div className="space-y-3">
            <button
              onClick={() => {
                setChoice("solo");
                void pickSolo();
              }}
              disabled={busy}
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
                choice === "team" ? "border-accent" : "border-line"
              }`}
            >
              <button
                onClick={() => setChoice("team")}
                disabled={busy}
                className="w-full text-left"
              >
                <div className="text-sm font-semibold">My team</div>
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
                    disabled={busy || !gitUrl.trim()}
                    className="rounded-lg bg-accent px-4 py-2 text-sm font-medium text-white transition hover:opacity-90 disabled:opacity-50"
                  >
                    {busy ? "Connecting…" : "Connect"}
                  </button>
                </form>
              )}
            </div>

            <div className="rounded-xl border border-dashed border-line p-4">
              <div className="text-sm font-semibold text-ink-soft">
                skills.new
              </div>
              <div className="mt-0.5 text-sm text-ink-faint">
                Company-managed libraries with sign-in are coming to the app
                soon. Until then, run{" "}
                <code className="rounded bg-accent-soft px-1 py-0.5 font-mono text-xs">
                  sx init
                </code>{" "}
                in a terminal and the app will pick it up.
              </div>
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
