import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
} from "react";
import {
  AddExtensionFromFolder,
  CanInstallForEveryone,
  GetVaultInfo,
  RemoveExtensionAsset,
  SetPluginDecision,
  ShareExtensionWithLibrary,
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
  describePermission,
  hasConsent,
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
  const [notice, setNotice] = useState("");
  // Consent prompts queue: Update all can stage several
  // permission-changed extensions; they re-prompt one at a time.
  const [consentQueue, setConsentQueue] = useState<PluginManifest[]>([]);
  const consentFor = consentQueue[0] ?? null;
  const [removeFor, setRemoveFor] = useState<PluginManifest | null>(null);
  // Which row's overflow (⋯) menu is open ("" = none). Secondary and
  // destructive per-extension actions live here so the resting row keeps
  // only the toggle inline (Firefox about:addons pattern).
  const [menuFor, setMenuFor] = useState("");
  // The ⋯ trigger that opened the current menu, so closing restores
  // focus to it (keyboard users keep their place in the row).
  const menuTriggerRef = useRef<HTMLButtonElement | null>(null);
  const closeMenu = useCallback(() => {
    setMenuFor("");
    menuTriggerRef.current?.focus();
    menuTriggerRef.current = null;
  }, []);
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
  // Sharing a personal install library-wide is org-admin-gated on
  // governed vaults, same as "Install for everyone" in the marketplace.
  const [canEveryone, setCanEveryone] = useState(false);
  useEffect(() => {
    GetVaultInfo()
      .then((v) => setLibraryName(v.name || ""))
      .catch(() => {});
    VaultSupportsExtensions()
      .then(setSupported)
      // Fail CLOSED like the backend gate: if we can't tell whether the
      // library stores extensions, a publish would fail anyway.
      .catch(() => setSupported(false));
    CanInstallForEveryone()
      .then(setCanEveryone)
      .catch(() => setCanEveryone(false));
  }, []);

  // The overflow menu dismisses like a normal popover: Escape closes it
  // and restores focus to the trigger (the backdrop handles click-away).
  useEffect(() => {
    if (!menuFor) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") closeMenu();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [menuFor, closeMenu]);

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
    setNotice("");
    try {
      // Disable first (full teardown), then remove the vault side —
      // publish forces asset name = plugin id, so the id addresses it.
      // The backend removes what the user means: a personal install
      // loses only their scope, a shared one goes for everyone. The
      // refresh re-registers the extension if it still reaches them
      // (e.g. the library shares what they personally dropped).
      disablePlugin(manifest.id);
      await SetPluginDecision(manifest.id, false);
      const summary = await RemoveExtensionAsset(manifest.id);
      unregisterVaultPlugin(manifest.id);
      await refreshVaultPlugins();
      if (summary) setNotice(summary);
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy("");
    }
  }

  async function share(manifest: PluginManifest) {
    setBusy(manifest.id);
    setError("");
    setNotice("");
    try {
      await ShareExtensionWithLibrary(manifest.id);
      await refreshVaultPlugins();
      setNotice(`${manifest.name} is now shared with the whole library.`);
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
        {libraryName ? ` (${libraryName})` : ""}: each one is installed just
        for you or shared with the library — the chip on a row says which.
        Turning one on is each person's own choice. Disabling one removes
        everything it added, immediately.
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
                  {!p.builtIn &&
                    p.scope?.label &&
                    p.scope.label !== "Everyone" && (
                      <span
                        title="Who receives this extension"
                        data-scope-chip={p.manifest.id}
                        className="shrink-0 whitespace-nowrap rounded-full border border-accent/40 px-1.5 text-[10px] font-normal text-accent"
                      >
                        {p.scope.label}
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
              {/* Trailing controls: Update pill (only when available) stays
                  visible, then the primary toggle, then an overflow menu
                  for the occasional/destructive actions — so the resting
                  row has at most two controls and the description keeps
                  full width. */}
              {update && (
                <button
                  onClick={() => void updateOne(update)}
                  disabled={busy !== ""}
                  data-update={p.manifest.id}
                  title={`Update from v${update.installedVersion} to v${update.entry.version}`}
                  className="shrink-0 rounded-lg border border-accent px-2.5 py-1 text-xs font-medium text-accent transition hover:bg-accent-soft disabled:opacity-50"
                >
                  {busy === p.manifest.id ? "Updating…" : "Update"}
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
              {/* Built-ins have neither Share nor Remove, so no menu. */}
              {!p.builtIn && (
                <div className="relative shrink-0">
                  <button
                    onClick={(e) => {
                      if (menuFor === p.manifest.id) {
                        closeMenu();
                      } else {
                        menuTriggerRef.current = e.currentTarget;
                        setMenuFor(p.manifest.id);
                      }
                    }}
                    disabled={busy === p.manifest.id}
                    aria-label={`More actions for ${p.manifest.name}`}
                    aria-haspopup="menu"
                    aria-expanded={menuFor === p.manifest.id}
                    data-ext-menu={p.manifest.id}
                    // relative z-10 keeps the trigger above the click-away
                    // backdrop so a second click closes the menu.
                    className="relative z-10 rounded-lg px-1.5 py-1 text-sm leading-none text-ink-faint transition hover:text-ink disabled:opacity-50"
                  >
                    ⋯
                  </button>
                  {menuFor === p.manifest.id &&
                    (() => {
                      const canShare = !!(
                        p.scope?.personal &&
                        !p.scope?.shared &&
                        canEveryone
                      );
                      return (
                        <>
                          {/* Invisible click-away backdrop, under the menu. */}
                          <span
                            className="fixed inset-0 z-[5]"
                            onMouseDown={() => closeMenu()}
                          />
                          <div
                            role="menu"
                            // Roving focus between items, per the menu ARIA
                            // pattern the roles advertise.
                            onKeyDown={(e) => {
                              const items = Array.from(
                                e.currentTarget.querySelectorAll<HTMLButtonElement>(
                                  '[role="menuitem"]:not(:disabled)',
                                ),
                              );
                              if (items.length === 0) return;
                              const i = items.indexOf(
                                document.activeElement as HTMLButtonElement,
                              );
                              if (e.key === "ArrowDown") {
                                e.preventDefault();
                                items[(i + 1) % items.length]?.focus();
                              } else if (e.key === "ArrowUp") {
                                e.preventDefault();
                                items[(i - 1 + items.length) % items.length]?.focus();
                              } else if (e.key === "Home") {
                                e.preventDefault();
                                items[0]?.focus();
                              } else if (e.key === "End") {
                                e.preventDefault();
                                items[items.length - 1]?.focus();
                              }
                            }}
                            className="absolute right-0 top-full z-10 mt-1 w-44 rounded-lg border border-line bg-surface p-1 shadow-lg"
                          >
                            {canShare && (
                              <button
                                role="menuitem"
                                autoFocus
                                onClick={() => {
                                  closeMenu();
                                  void share(p.manifest);
                                }}
                                disabled={busy === p.manifest.id}
                                data-share={p.manifest.id}
                                className="block w-full rounded-md px-2 py-1.5 text-left text-xs text-ink transition hover:bg-accent-soft disabled:opacity-50"
                              >
                                Share with library
                              </button>
                            )}
                            <button
                              role="menuitem"
                              autoFocus={!canShare}
                              onClick={() => {
                                closeMenu();
                                setRemoveFor(p.manifest);
                              }}
                              disabled={busy === p.manifest.id}
                              data-remove={p.manifest.id}
                              className="block w-full rounded-md px-2 py-1.5 text-left text-xs text-danger transition hover:bg-danger-soft disabled:opacity-50"
                            >
                              Remove…
                            </button>
                          </div>
                        </>
                      );
                    })()}
                </div>
              )}
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
      {notice && (
        <div className="mt-2 rounded-lg bg-accent-soft px-3 py-2 text-xs text-accent">
          {notice}
        </div>
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
              {(() => {
                const scope = plugins.find(
                  (p) => p.manifest.id === removeFor.id,
                )?.scope;
                if (scope?.personal && !scope?.shared) {
                  return "This is installed just for you — removing it doesn't affect anyone else. You can reinstall it from the marketplace later.";
                }
                if (scope?.personal && scope?.shared) {
                  return "This removes your personal install, but the library still shares this extension with you, so it stays available.";
                }
                return "This disables the extension and deletes it from the library — anyone it's shared with loses it too. You can reinstall it from the marketplace or a folder later.";
              })()}
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
                  {describePermission(perm)}
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
