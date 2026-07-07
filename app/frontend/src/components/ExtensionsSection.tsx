import { useEffect, useMemo, useState, useSyncExternalStore } from "react";
import {
  AddExtensionFromFolder,
  DeleteAssets,
  GetVaultInfo,
  SetPluginDecision,
  VaultSupportsExtensions,
} from "../../wailsjs/go/main/App";
import { refreshVaultPlugins } from "../plugins/boot";
import {
  disablePlugin,
  enablePlugin,
  listPlugins,
  loaderPreflight,
  subscribeHost,
  unregisterVaultPlugin,
  type LoaderPreflight,
} from "../plugins/host";
import {
  applyUpdate,
  findUpdates,
  getCatalog,
  loadCatalog,
  subscribeCatalog,
  type UpdateInfo,
} from "../plugins/updates";
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
  // Consent prompts queue: Update all can stage several
  // permission-changed extensions; they re-prompt one at a time.
  const [consentQueue, setConsentQueue] = useState<PluginManifest[]>([]);
  const consentFor = consentQueue[0] ?? null;
  const [removeFor, setRemoveFor] = useState<PluginManifest | null>(null);
  const [preflight, setPreflight] = useState<LoaderPreflight | null>(null);
  // Update-all progress ("Updating 2/5…").
  const [batch, setBatch] = useState<{ done: number; total: number } | null>(
    null,
  );

  // Extensions are PER-LIBRARY — name the library so the list can't
  // read as app-wide state, and gate publish paths on backends that
  // can't store app-plugin assets yet (skills.new until P5).
  const [libraryName, setLibraryName] = useState("");
  const [supported, setSupported] = useState(true);
  useEffect(() => {
    GetVaultInfo()
      .then((v) => setLibraryName(v.name || ""))
      .catch(() => {});
    VaultSupportsExtensions()
      .then(setSupported)
      // Fail CLOSED like the backend gate: if we can't tell whether the
      // library stores extensions, a publish would fail anyway.
      .catch(() => setSupported(false));
  }, []);

  useEffect(() => {
    void loaderPreflight().then(setPreflight);
  }, []);
  // Update availability derives REACTIVELY from the shared catalog and
  // the live plugin list, so updating from either panel clears the
  // button in both. The fetch is guarded: with no vault-installed
  // extensions there is nothing to update, and the catalog fetch (git
  // fetch + unpack every bundle) would be pure waste.
  const catalog = useSyncExternalStore(subscribeCatalog, getCatalog);
  useEffect(() => {
    if (plugins.some((p) => !p.builtIn)) {
      loadCatalog().catch(() => {});
    }
  }, [plugins]);
  const updates = useMemo(
    () => findUpdates(catalog ?? []),
    // plugins is the reactive half of the comparison findUpdates makes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [catalog, plugins],
  );

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
          setConsentQueue((q) => [...q, manifest]);
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
    setConsentQueue((q) => q.slice(1));
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

  async function updateOne(info: UpdateInfo) {
    setBusy(info.entry.id);
    setError("");
    try {
      const outcome = await applyUpdate(info.entry);
      if (outcome.state === "needs-consent") {
        setConsentQueue((q) => [...q, outcome.manifest]);
      }
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy("");
    }
  }

  // Sequential on purpose: each update is a vault publish (a commit and
  // push on git vaults) and concurrent pushes to one remote just fight.
  async function updateAll() {
    setError("");
    const queue = [...updates];
    setBatch({ done: 0, total: queue.length });
    for (const [i, info] of queue.entries()) {
      setBatch({ done: i, total: queue.length });
      setBusy(info.entry.id);
      try {
        const outcome = await applyUpdate(info.entry);
        if (outcome.state === "needs-consent") {
          setConsentQueue((q) => [...q, outcome.manifest]);
        }
      } catch (e) {
        setError(String(e));
        break;
      }
    }
    setBatch(null);
    setBusy("");
  }

  async function reallyRemove() {
    if (!removeFor) return;
    const manifest = removeFor;
    setRemoveFor(null);
    setBusy(manifest.id);
    setError("");
    try {
      // Disable first (full teardown), then delete the vault asset —
      // publish forces asset name = plugin id, so the id addresses it.
      disablePlugin(manifest.id);
      await SetPluginDecision(manifest.id, false);
      await DeleteAssets([manifest.id]);
      unregisterVaultPlugin(manifest.id);
      await refreshVaultPlugins();
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
      <div className="mb-1 flex items-center justify-between gap-2">
        <span className="text-xs font-semibold tracking-wide text-ink-faint">
          EXTENSIONS{libraryName ? ` — ${libraryName.toUpperCase()}` : ""}
        </span>
        <span className="flex items-center gap-2">
          {(updates.length > 0 || batch) && (
            <button
              onClick={() => void updateAll()}
              disabled={busy !== ""}
              data-update-all
              className="rounded-lg bg-accent px-2.5 py-1 text-xs font-medium text-white transition hover:opacity-90 disabled:opacity-50"
            >
              {batch
                ? `Updating ${Math.min(batch.done + 1, batch.total)}/${batch.total}…`
                : `Update all (${updates.length})`}
            </button>
          )}
          {supported && (
            <button
              onClick={() => void addFromFolder()}
              disabled={busy === "add"}
              title="Publish an extension folder (plugin.json + main.js) to this library"
              className="rounded-lg border border-line px-2.5 py-1 text-xs font-medium text-ink-soft transition hover:border-accent hover:text-ink disabled:opacity-50"
            >
              {busy === "add" ? "Publishing…" : "Add extension…"}
            </button>
          )}
        </span>
      </div>
      <p className="mb-3 text-xs text-ink-faint">
        Extensions belong to this library
        {libraryName ? ` (${libraryName})` : ""}: installing one shares it
        with everyone who uses it; turning it on is each person's own
        choice. Disabling one removes everything it added, immediately.
      </p>
      {!supported && (
        <p className="mb-3 rounded-lg bg-amber-50 px-3 py-2 text-xs text-amber-800 dark:bg-amber-950 dark:text-amber-200">
          This library's server can't store extensions yet, so built-ins
          are all that runs here. Installing from the marketplace needs a
          git or local library — skills.new support is on the way.
        </p>
      )}
      <ul className="space-y-2">
        {plugins.map((p) => {
          const blocked = policyBlocks(p.manifest.id, p.builtIn);
          const update = updates.find((u) => u.entry.id === p.manifest.id);
          return (
            <li
              key={p.manifest.id}
              data-extension={p.manifest.id}
              className="flex items-center gap-3 rounded-xl border border-line p-3"
            >
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2 text-sm font-medium">
                  {p.manifest.name}
                  <span className="text-[10px] font-normal text-ink-faint">
                    v{p.manifest.version}
                  </span>
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
              {update && (
                <button
                  onClick={() => void updateOne(update)}
                  disabled={busy !== ""}
                  data-update={p.manifest.id}
                  title={`Update from v${update.installedVersion} to v${update.entry.version}`}
                  className="shrink-0 rounded-lg border border-accent px-2.5 py-1 text-xs font-medium text-accent transition hover:bg-accent-soft disabled:opacity-50"
                >
                  {busy === p.manifest.id
                    ? "Updating…"
                    : `Update to v${update.entry.version}`}
                </button>
              )}
              {!p.builtIn && (
                <button
                  onClick={() => setRemoveFor(p.manifest)}
                  disabled={busy === p.manifest.id}
                  title={`Remove ${p.manifest.name} from this library`}
                  aria-label={`Remove ${p.manifest.name}`}
                  className="shrink-0 rounded-lg px-1.5 py-1 text-xs text-ink-faint transition hover:text-danger disabled:opacity-50"
                >
                  Remove…
                </button>
              )}
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

      {removeFor && (
        <div
          data-remove-sheet
          className="fixed inset-0 z-[95] flex items-center justify-center bg-black/40 p-6"
          onMouseDown={(e) => {
            if (e.target === e.currentTarget) setRemoveFor(null);
          }}
        >
          <div className="w-[420px] rounded-2xl border border-line bg-surface p-5 shadow-2xl">
            <h3 className="text-sm font-semibold">
              Remove {removeFor.name}?
            </h3>
            <p className="mt-2 text-xs text-ink-faint">
              This disables the extension and deletes it from the library —
              anyone it's shared with loses it too. You can reinstall it from
              the marketplace or a folder later.
            </p>
            <div className="mt-4 flex justify-end gap-2">
              <button
                onClick={() => setRemoveFor(null)}
                className="rounded-lg px-3 py-1.5 text-sm font-medium text-ink-faint transition hover:text-ink"
              >
                Cancel
              </button>
              <button
                autoFocus
                onClick={() => void reallyRemove()}
                className="rounded-lg bg-danger px-4 py-1.5 text-sm font-medium text-white transition hover:opacity-90"
              >
                Remove
              </button>
            </div>
          </div>
        </div>
      )}

      {consentFor && (
        <div
          data-consent-sheet
          className="fixed inset-0 z-[95] flex items-center justify-center bg-black/40 p-6"
          onMouseDown={(e) => {
            if (e.target === e.currentTarget) setConsentQueue((q) => q.slice(1));
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
                onClick={() => setConsentQueue((q) => q.slice(1))}
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
