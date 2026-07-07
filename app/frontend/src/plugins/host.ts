// The plugin host: owns extension lifecycle. Everything an extension
// registers is tracked by its id and torn down on disable — panels,
// tabs, widgets, commands, event handlers, and mounted DOM. Teardown is
// mandatory by construction (docs/app-plugins-spec.md), not a convention
// the extension is trusted to follow.

import type { PluginManifest, SxPlugin } from "./api";
import { buildSxAPI, disposePluginMounts } from "./sxapi";
import { unregisterPlugin } from "./registry";
import { unsubscribePlugin } from "./events";

// The running app version, set once at boot (sx.app.version and the
// minAppVersion load gate both read it).
let appVersion = "";

export function setAppVersion(v: string): void {
  appVersion = v;
}

export function getAppVersion(): string {
  return appVersion;
}

/** Dotted-numeric compare: -1/0/1. Non-numeric segments compare as 0. */
export function compareVersions(a: string, b: string): number {
  const pa = a.split(".").map((s) => parseInt(s, 10) || 0);
  const pb = b.split(".").map((s) => parseInt(s, 10) || 0);
  for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
    const d = (pa[i] ?? 0) - (pb[i] ?? 0);
    if (d !== 0) return d < 0 ? -1 : 1;
  }
  return 0;
}

export interface LoadedPlugin {
  manifest: PluginManifest;
  builtIn: boolean;
  enabled: boolean;
  error?: string;
}

interface Registered {
  manifest: PluginManifest;
  builtIn: boolean;
  make: () => Promise<SxPlugin>;
  instance?: SxPlugin;
  enabled: boolean;
  error?: string;
}

const plugins = new Map<string, Registered>();
const listeners = new Set<() => void>();
let snapshot: LoadedPlugin[] | null = null;

function notify() {
  snapshot = null;
  for (const l of listeners) l();
}

export function subscribeHost(cb: () => void): () => void {
  listeners.add(cb);
  return () => listeners.delete(cb);
}

export function listPlugins(): LoadedPlugin[] {
  if (!snapshot) {
    snapshot = [...plugins.values()].map((p) => ({
      manifest: p.manifest,
      builtIn: p.builtIn,
      enabled: p.enabled,
      error: p.error,
    }));
  }
  return snapshot;
}

/** Register a built-in (bundled) extension. Built-ins run through the
 * exact same API/permission/lifecycle path as vault-installed ones; only
 * the code source differs (module import vs Blob-URL import). */
export function registerBuiltIn(
  manifest: PluginManifest,
  make: () => Promise<SxPlugin>,
): void {
  if (plugins.has(manifest.id)) return;
  plugins.set(manifest.id, {
    manifest,
    builtIn: true,
    make,
    enabled: false,
  });
  notify();
}

const KNOWN_PERMISSIONS = new Set([
  "assets:read",
  "usage:read",
  "drafts:write",
  "views:sidebar",
  "views:asset-tab",
  "views:dashboard",
  "commands",
  "events",
]);

/** Parse and validate a vault extension's plugin.json. Throws with a
 * user-readable reason; unknown permissions are rejected outright (the
 * proxy would block them anyway, but a wrong manifest should fail loudly
 * at registration, not confusingly at call time). */
export function parseVaultManifest(raw: string): PluginManifest {
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    throw new Error("plugin.json is not valid JSON");
  }
  const m = parsed as Partial<PluginManifest>;
  if (!m.id || !/^[a-z][a-z0-9-]{1,63}$/.test(m.id)) {
    throw new Error("plugin.json id must be lowercase letters/digits/hyphens");
  }
  if (m.id === "sx" || m.id.startsWith("sx-")) {
    throw new Error('plugin.json id may not claim the "sx" prefix');
  }
  if (!m.name || !m.version) {
    throw new Error("plugin.json needs name and version");
  }
  if (!Array.isArray(m.permissions)) {
    throw new Error("plugin.json needs a permissions array (may be empty)");
  }
  for (const p of m.permissions) {
    if (!KNOWN_PERMISSIONS.has(p)) {
      throw new Error(`plugin.json declares unknown permission "${p}"`);
    }
  }
  return m as PluginManifest;
}

/** Register a vault-installed extension: code arrives as source text and
 * loads through the Blob importer. Defaults OFF; enabling requires
 * consent and passes the org policy gate like everything else. */
export function registerVaultPlugin(manifest: PluginManifest, source: string): void {
  if (plugins.has(manifest.id)) {
    // Built-ins and earlier registrations win; say so instead of
    // silently dropping (two vault assets can declare the same id).
    console.error(
      `extension id "${manifest.id}" is already registered — ignoring the duplicate`,
    );
    return;
  }
  plugins.set(manifest.id, {
    manifest,
    builtIn: false,
    make: () => importFromSource(source),
    enabled: false,
  });
  notify();
}

/** Load an extension's code from source text — the vault-installed path.
 * Blob-URL dynamic import: the only code source is the vault; no eval,
 * no remote scripts. (Verified on WKWebView dev+production 2026-07-07.) */
export async function importFromSource(source: string): Promise<SxPlugin> {
  const url = URL.createObjectURL(
    new Blob([source], { type: "text/javascript" }),
  );
  try {
    const mod = await import(/* @vite-ignore */ url);
    const PluginClass = mod.default;
    if (typeof PluginClass !== "function") {
      throw new Error("main.js must default-export a plugin class");
    }
    return new PluginClass() as SxPlugin;
  } finally {
    URL.revokeObjectURL(url);
  }
}

export async function enablePlugin(id: string): Promise<void> {
  const p = plugins.get(id);
  if (!p) throw new Error(`unknown extension ${id}`);
  if (p.enabled) return;
  // minAppVersion gates the load: an extension built against a newer API
  // fails with a clear message instead of half-working. Dev builds
  // (empty/non-numeric version) are never blocked.
  const min = p.manifest.minAppVersion;
  if (min && appVersion && /^\d/.test(appVersion) && compareVersions(appVersion, min) < 0) {
    p.error = `needs sx ${min}+ (this is ${appVersion})`;
    notify();
    throw new Error(p.error);
  }
  try {
    p.instance = await p.make();
    const api = buildSxAPI(p.manifest);
    await p.instance.onload(api);
    p.enabled = true;
    p.error = undefined;
  } catch (e) {
    // A failed onload must leave no partial registrations behind.
    teardown(id);
    p.instance = undefined;
    p.error = String(e);
    notify();
    throw e;
  }
  notify();
}

export function disablePlugin(id: string): void {
  const p = plugins.get(id);
  if (!p || !p.enabled) return;
  try {
    p.instance?.onunload?.();
  } catch (e) {
    console.error(`extension ${id}: onunload failed`, e);
  }
  teardown(id);
  p.instance = undefined;
  p.enabled = false;
  notify();
}

/** Remove every trace of the extension from the app. */
function teardown(id: string): void {
  unregisterPlugin(id);
  unsubscribePlugin(id);
  disposePluginMounts(id);
}

/** Test/dev helper. */
export function resetHost(): void {
  for (const id of [...plugins.keys()]) disablePlugin(id);
  plugins.clear();
  notify();
}

// ---- Loader preflight ----
// The one platform-sensitive mechanism, checked at startup on every OS
// (macOS verified during the P1 spike; Windows/Linux prove themselves
// here). Result is surfaced in Extensions diagnostics.

export interface LoaderPreflight {
  blobImport: boolean;
  cssInjection: boolean;
  detail?: string;
}

let preflightResult: LoaderPreflight | null = null;

export async function loaderPreflight(): Promise<LoaderPreflight> {
  if (preflightResult) return preflightResult;
  const result: LoaderPreflight = { blobImport: false, cssInjection: false };
  try {
    const plugin = await importFromSource(
      `export default class { onload() { globalThis.__sxPreflight = "ok"; } }`,
    );
    plugin.onload(null as never);
    result.blobImport =
      (globalThis as Record<string, unknown>).__sxPreflight === "ok";
    delete (globalThis as Record<string, unknown>).__sxPreflight;
  } catch (e) {
    result.detail = String(e);
  }
  try {
    const style = document.createElement("style");
    style.textContent = ".sx-preflight { color: rgb(1, 2, 3); }";
    document.head.appendChild(style);
    const el = document.createElement("div");
    el.className = "sx-preflight";
    document.body.appendChild(el);
    result.cssInjection = getComputedStyle(el).color === "rgb(1, 2, 3)";
    el.remove();
    style.remove();
  } catch (e) {
    result.detail = (result.detail ?? "") + " css: " + String(e);
  }
  preflightResult = result;
  return result;
}
