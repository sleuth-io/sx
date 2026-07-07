import { useEffect, useState, useSyncExternalStore } from "react";
import {
  AddExtensionFromFolder,
  SetPluginDecision,
} from "../../wailsjs/go/main/App";
import { refreshVaultPlugins } from "../plugins/boot";
import {
  disablePlugin,
  enablePlugin,
  listPlugins,
  loaderPreflight,
  subscribeHost,
  type LoaderPreflight,
} from "../plugins/host";
import {
  currentPolicy,
  hasConsent,
  PERMISSION_DESCRIPTIONS,
  policyBlocks,
  recordConsent,
} from "../plugins/policy";
import type { PluginManifest } from "../plugins/api";

/**
 * The Extensions section in Settings. Toggling persists INTENT (a load
 * failure never demotes a wanted extension) and enabling an extension the
 * user hasn't consented to — or whose permissions changed since consent —
 * shows the permission sheet first. Org policy renders as blocked state,
 * never as silently-missing rows.
 */
export default function ExtensionsSection() {
  const plugins = useSyncExternalStore(subscribeHost, listPlugins);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [consentFor, setConsentFor] = useState<PluginManifest | null>(null);
  const [preflight, setPreflight] = useState<LoaderPreflight | null>(null);

  useEffect(() => {
    void loaderPreflight().then(setPreflight);
  }, []);

  if (currentPolicy().mode === "disabled") {
    return (
      <>
        <div className="mb-1 mt-6 text-xs font-semibold tracking-wide text-ink-faint">
          EXTENSIONS
        </div>
        <p className="text-xs text-ink-faint">
          Extensions are disabled by your organization.
        </p>
      </>
    );
  }

  async function reallyEnable(manifest: PluginManifest) {
    await enablePlugin(manifest.id);
    await SetPluginDecision(manifest.id, true);
  }

  async function toggle(manifest: PluginManifest, enabled: boolean) {
    setBusy(manifest.id);
    setError("");
    try {
      if (enabled) {
        // Consent gate: first enable, or permissions changed since the
        // user last agreed.
        if (!hasConsent(manifest)) {
          setConsentFor(manifest);
          return;
        }
        await reallyEnable(manifest);
      } else {
        disablePlugin(manifest.id);
        await SetPluginDecision(manifest.id, false);
      }
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy("");
    }
  }

  async function consentAndEnable() {
    if (!consentFor) return;
    const manifest = consentFor;
    setConsentFor(null);
    setBusy(manifest.id);
    try {
      await recordConsent(manifest);
      await reallyEnable(manifest);
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy("");
    }
  }

  async function addFromFolder() {
    setBusy("add");
    setError("");
    try {
      const name = await AddExtensionFromFolder();
      if (name) {
        await refreshVaultPlugins();
      }
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy("");
    }
  }

  return (
    <>
      <div className="mb-1 flex items-center justify-between">
        <span className="text-xs font-semibold tracking-wide text-ink-faint">
          EXTENSIONS
        </span>
        <button
          onClick={() => void addFromFolder()}
          disabled={busy === "add"}
          title="Publish an extension folder (plugin.json + main.js) to this library"
          className="rounded-lg border border-line px-2.5 py-1 text-xs font-medium text-ink-soft transition hover:border-accent hover:text-ink disabled:opacity-50"
        >
          {busy === "add" ? "Publishing…" : "Add extension…"}
        </button>
      </div>
      <p className="mb-3 text-xs text-ink-faint">
        Optional features that run inside sx. Disabling one removes
        everything it added, immediately.
      </p>
      <ul className="space-y-2">
        {plugins.map((p) => {
          const blocked = policyBlocks(p.manifest.id, p.builtIn);
          return (
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
                {blocked && (
                  <p className="mt-0.5 text-xs text-amber-700 dark:text-amber-300">
                    {blocked}
                  </p>
                )}
                {p.error && !blocked && (
                  <p className="mt-0.5 text-xs text-danger">{p.error}</p>
                )}
              </div>
              <button
                onClick={() => void toggle(p.manifest, !p.enabled)}
                disabled={busy === p.manifest.id || !!blocked}
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
          );
        })}
      </ul>
      {preflight && (!preflight.blobImport || !preflight.cssInjection) && (
        <p className="mt-2 text-xs text-danger" data-extension-diagnostics>
          Extension loader problem on this platform — Blob import:{" "}
          {String(preflight.blobImport)}, CSS: {String(preflight.cssInjection)}
          {preflight.detail ? ` (${preflight.detail})` : ""}
        </p>
      )}
      {error && (
        <div className="mt-2 rounded-lg bg-danger-soft px-3 py-2 text-xs text-danger">
          {error}
        </div>
      )}

      {consentFor && (
        <div
          data-consent-sheet
          className="fixed inset-0 z-[95] flex items-center justify-center bg-black/40 p-6"
          onMouseDown={(e) => {
            if (e.target === e.currentTarget) setConsentFor(null);
          }}
        >
          <div className="w-[420px] rounded-2xl border border-line bg-surface p-5 shadow-2xl">
            <h3 className="text-sm font-semibold">
              Enable {consentFor.name}?
            </h3>
            <p className="mt-1 text-xs text-ink-faint">
              This extension can:
            </p>
            <ul className="mt-2 space-y-1.5">
              {consentFor.permissions.map((perm) => (
                <li key={perm} className="flex gap-2 text-sm">
                  <span className="text-accent">•</span>
                  {PERMISSION_DESCRIPTIONS[perm] ?? perm}
                </li>
              ))}
            </ul>
            {!plugins.find((p) => p.manifest.id === consentFor.id)
              ?.builtIn && (
              <p className="mt-3 rounded-lg bg-amber-50 px-3 py-2 text-xs text-amber-800 dark:bg-amber-950 dark:text-amber-200">
                This extension was published to your library and runs inside
                sx with the app's own access. The list above is what it says
                it uses — only enable extensions from people you trust, the
                same way you'd treat their code.
              </p>
            )}
            <div className="mt-4 flex justify-end gap-2">
              <button
                onClick={() => setConsentFor(null)}
                className="rounded-lg px-3 py-1.5 text-sm font-medium text-ink-faint transition hover:text-ink"
              >
                Cancel
              </button>
              <button
                autoFocus
                onClick={() => void consentAndEnable()}
                className="rounded-lg bg-accent px-4 py-1.5 text-sm font-medium text-white transition hover:opacity-90"
              >
                Enable
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
