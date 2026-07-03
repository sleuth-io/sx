import { useEffect, useState } from "react";
import { GetSettings, SwitchProfile } from "../../wailsjs/go/main/App";
import type { main } from "../../wailsjs/go/models";
import Modal from "./Modal";

const TYPE_LABELS: Record<string, string> = {
  git: "Team git repository",
  path: "Local folder",
  sleuth: "skills.new",
};

/**
 * Settings: which configuration the app is using and which profile is
 * active. Shared with the sx CLI — switching the profile here switches it
 * everywhere.
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

  return (
    <Modal title="Settings" onClose={onClose} width="w-[520px]">
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
                    <div className="text-xs text-ink-faint">{p.identity}</div>
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

          <div className="mt-4 border-t border-line pt-3 text-xs text-ink-faint">
            Configuration file:{" "}
            <code className="break-all font-mono">{settings.configPath}</code>
            <br />
            Add or edit libraries with{" "}
            <code className="font-mono">sx init</code> in a terminal.
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
