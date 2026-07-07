import { useEffect, useState, useSyncExternalStore } from "react";
import { SetEnabledPlugins } from "../../wailsjs/go/main/App";
import {
  disablePlugin,
  enablePlugin,
  listPlugins,
  subscribeHost,
} from "../plugins/host";

/**
 * The Extensions section in Settings (P1's minimal management surface —
 * the full Extensions screen with browse/publish/dev-load arrives in P2).
 * Toggling takes effect immediately: enabling loads through the host,
 * disabling tears down every registration and mounted view.
 */
export default function ExtensionsSection() {
  const plugins = useSyncExternalStore(subscribeHost, listPlugins);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");

  useEffect(() => setError(""), [plugins.length]);

  async function toggle(id: string, enabled: boolean) {
    setBusy(id);
    setError("");
    try {
      if (enabled) {
        await enablePlugin(id);
      } else {
        disablePlugin(id);
      }
      // Persist the host's full current state — the single source of
      // truth for enabled extensions (no default-list duplication in Go).
      await SetEnabledPlugins(
        listPlugins()
          .filter((p) => p.enabled)
          .map((p) => p.manifest.id),
      );
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy("");
    }
  }

  if (plugins.length === 0) return null;

  return (
    <>
      <div className="mb-1 mt-6 text-xs font-semibold tracking-wide text-ink-faint">
        EXTENSIONS
      </div>
      <p className="mb-3 text-xs text-ink-faint">
        Optional features that run inside sx. Disabling one removes
        everything it added, immediately.
      </p>
      <ul className="space-y-2">
        {plugins.map((p) => (
          <li
            key={p.manifest.id}
            data-extension={p.manifest.id}
            className="flex items-center gap-3 rounded-xl border border-line p-3"
          >
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2 text-sm font-medium">
                {p.manifest.name}
                {p.builtIn && (
                  <span className="rounded-full border border-line px-1.5 text-[10px] text-ink-faint">
                    built-in
                  </span>
                )}
              </div>
              {p.manifest.description && (
                <p className="mt-0.5 text-xs text-ink-faint">
                  {p.manifest.description}
                </p>
              )}
              {p.error && (
                <p className="mt-0.5 text-xs text-danger">{p.error}</p>
              )}
            </div>
            <button
              onClick={() => void toggle(p.manifest.id, !p.enabled)}
              disabled={busy === p.manifest.id}
              role="switch"
              aria-checked={p.enabled}
              aria-label={`${p.enabled ? "Disable" : "Enable"} ${p.manifest.name}`}
              className={`relative h-5 w-9 shrink-0 rounded-full transition ${
                p.enabled ? "bg-accent" : "bg-line"
              } disabled:opacity-50`}
            >
              <span
                className={`absolute top-0.5 h-4 w-4 rounded-full bg-white shadow transition-all ${
                  p.enabled ? "left-[18px]" : "left-0.5"
                }`}
              />
            </button>
          </li>
        ))}
      </ul>
      {error && (
        <div className="mt-2 rounded-lg bg-danger-soft px-3 py-2 text-xs text-danger">
          {error}
        </div>
      )}
    </>
  );
}
