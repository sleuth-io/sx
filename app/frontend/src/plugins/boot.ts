// Boots the extension system: registers built-ins with the host, runs the
// loader preflight, loads the org policy + user decisions, and enables
// what the user INTENDS (unknown ids fall back to their default:
// built-ins on). A load failure never rewrites intent — the extension
// shows its error in the Extensions screen and retries next boot.

import {
  AppVersion,
  ListVaultPlugins,
  PluginDecisions,
} from "../../wailsjs/go/main/App";
import {
  registerBuiltIn,
  registerVaultPlugin,
  parseVaultManifest,
  disablePlugin,
  enablePlugin,
  listPlugins,
  loaderPreflight,
  setAppVersion,
  unregisterVaultPlugin,
} from "./host";
import { hasConsent, loadPolicyAndConsents, policyBlocks } from "./policy";
import PublishDoctor, {
  publishDoctorManifest,
} from "./builtins/publish-doctor";
import AdoptionWidget, {
  adoptionWidgetManifest,
} from "./builtins/adoption-widget";
import UsageTrendsWidget, {
  usageTrendsWidgetManifest,
} from "./builtins/usage-trends-widget";
import LeaderboardWidget, {
  leaderboardWidgetManifest,
} from "./builtins/leaderboard-widget";
import Templates, { templatesManifest } from "./builtins/templates";
import Importer, { importerManifest } from "./builtins/importer";

let booted = false;

export async function bootExtensions(): Promise<void> {
  if (booted) return;
  booted = true;

  // Built-ins run through the same host/API/permission path as
  // vault-installed extensions; only the code source differs (bundled
  // module vs Blob import — the preflight keeps the Blob path honest).
  registerBuiltIn(publishDoctorManifest, async () => new PublishDoctor());
  // Each dashboard widget is its OWN extension: teams disable or replace
  // them individually, and third-party widgets sit beside them as equals.
  registerBuiltIn(adoptionWidgetManifest, async () => new AdoptionWidget());
  registerBuiltIn(
    usageTrendsWidgetManifest,
    async () => new UsageTrendsWidget(),
  );
  registerBuiltIn(
    leaderboardWidgetManifest,
    async () => new LeaderboardWidget(),
  );
  registerBuiltIn(templatesManifest, async () => new Templates());
  registerBuiltIn(importerManifest, async () => new Importer());

  void loaderPreflight().then((r) => {
    if (!r.blobImport || !r.cssInjection) {
      console.error("extension loader preflight failed", r);
    }
  });

  try {
    setAppVersion(await AppVersion());
  } catch {
    // Dev/browser context without the bridge; version stays "".
  }
  await syncVaultExtensions();
}

/**
 * Load the CURRENT library's extension state: policy, decisions, its
 * app-plugin assets, and the resulting enablement. Extensions are
 * per-library, so this runs at boot AND on every library switch —
 * without the re-sync, switching libraries strands the previous
 * library's extensions (and its policy) in the host until restart.
 *
 * Serialized: rapid library switches must not interleave two syncs
 * (concurrent enables of one plugin double-mount and the duplicate
 * slot id tears the second down). Each call chains after any sync
 * already in flight; the last switch wins because it runs last.
 */
let syncing: Promise<void> | null = null;

export function syncVaultExtensions(): Promise<void> {
  const run = (syncing ?? Promise.resolve()).then(doSyncVaultExtensions);
  const guarded = run.finally(() => {
    if (syncing === guarded) syncing = null;
  });
  syncing = guarded;
  return guarded;
}

async function doSyncVaultExtensions(): Promise<void> {
  // Drop the previous library's vault extensions entirely; built-ins
  // stay registered and just re-evaluate against the new policy.
  for (const p of listPlugins()) {
    if (!p.builtIn) unregisterVaultPlugin(p.manifest.id);
  }
  await loadPolicyAndConsents();

  // Vault-installed extensions: published like any asset, scoped to this
  // user, surfaced ONLY in the Extensions screen. Default off; enabling
  // is consent-gated below like everything else.
  try {
    for (const vp of (await ListVaultPlugins()) ?? []) {
      try {
        registerVaultPlugin(parseVaultManifest(vp.manifest), vp.source, vp.scope);
      } catch (e) {
        console.error(`extension asset ${vp.assetName} rejected:`, e);
      }
    }
  } catch {
    // Vault unreachable — built-ins still work.
  }

  let decisions: Record<string, boolean> = {};
  try {
    decisions = (await PluginDecisions()) ?? {};
  } catch {
    // No profile yet (onboarding) — defaults apply next boot.
  }
  for (const p of listPlugins()) {
    const id = p.manifest.id;
    const intended = decisions[id] ?? p.builtIn; // built-ins default on
    const allowed = intended && !policyBlocks(id, p.builtIn);
    if (!allowed) {
      // A built-in enabled under the previous library's policy may be
      // blocked under this one — teardown, don't linger.
      if (p.enabled) disablePlugin(id);
      continue;
    }
    // The consent guarantee holds on EVERY load path, not just the
    // Settings toggle: a vault-installed extension whose permissions
    // changed since consent stays off until the user re-consents (its
    // row in the Extensions screen shows why). Built-ins ship with the
    // app and are implicitly consented at their bundled permission set.
    if (!p.builtIn && !hasConsent(p.manifest)) continue;
    if (p.enabled) continue;
    try {
      await enablePlugin(id);
    } catch (e) {
      // A broken extension must never take the app down — and must not
      // lose its intent: it stays "wanted" and retries next boot.
      console.error(`extension ${id} failed to enable`, e);
    }
  }
}

/** Re-scan the vault for extensions (after install, remove, or a sharing
 * change). Upserts what the vault lists and drops vault extensions that
 * no longer reach this user — a remove or scope change must not leave a
 * stale row behind. The prune runs ONLY on a successful listing:
 * ListVaultPlugins throws on a listing failure rather than returning
 * empty, so a transient backend error lands in the catch below instead
 * of reading as "no extensions" and tearing down everything running. */
export async function refreshVaultPlugins(): Promise<void> {
  try {
    const listed = (await ListVaultPlugins()) ?? [];
    const seen = new Set<string>();
    for (const vp of listed) {
      try {
        const manifest = parseVaultManifest(vp.manifest);
        registerVaultPlugin(manifest, vp.source, vp.scope);
        seen.add(manifest.id);
      } catch (e) {
        console.error(`extension asset ${vp.assetName} rejected:`, e);
      }
    }
    for (const p of listPlugins()) {
      if (!p.builtIn && !seen.has(p.manifest.id)) {
        unregisterVaultPlugin(p.manifest.id);
      }
    }
  } catch {
    // Listing failed — leave the registry as-is; the next boot or
    // refresh reconciles.
  }
}

/** Test/dev helper. */
export function resetBoot(): void {
  booted = false;
}
