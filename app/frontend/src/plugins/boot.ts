// Boots the extension system: registers built-ins with the host, runs the
// loader preflight, loads the org policy + user decisions, and enables
// what the user INTENDS (unknown ids fall back to their default:
// built-ins on). A load failure never rewrites intent — the extension
// shows its error in the Extensions screen and retries next boot.

import {
  AppVersion,
  CachedVaultPlugins,
  ListVaultPlugins,
  PluginDecisions,
} from "../../wailsjs/go/main/App";
import type { main } from "../../wailsjs/go/models";
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
import {
  hasConsent,
  loadCachedPolicyAndConsents,
  loadPolicyAndConsents,
  policyBlocks,
} from "./policy";
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
import SkillDoctor, { skillDoctorManifest } from "./builtins/skill-doctor";

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
  registerBuiltIn(skillDoctorManifest, async () => new SkillDoctor());

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
  // stay registered and just re-evaluate against the new policy. (On
  // plain boot this is a no-op; on a library switch the new profile's
  // cache repopulates immediately below.)
  for (const p of listPlugins()) {
    if (!p.builtIn) unregisterVaultPlugin(p.manifest.id);
  }

  // FAST PATH: the cached policy plus cached copies of the vault's
  // extensions register and enable before any vault I/O — built-ins
  // included, which are otherwise gated on the policy round trip. On a
  // remote vault the fresh listing below is the chattiest call in boot;
  // waiting for it lands extension UI last and reflows everything.
  // Bundles are immutable per revision, so cached copies are exact for
  // unchanged extensions; staleness lasts one revalidation (an extension
  // revoked since last boot runs for the seconds until the fresh listing
  // prunes it — the same window policy-cache.json already accepts).
  try {
    await loadCachedPolicyAndConsents();
    registerListing((await CachedVaultPlugins()) ?? []);
    await applyEnablement();
  } catch {
    // No usable cache — the fresh pass below is the first paint.
  }

  // REVALIDATE: fresh policy, fresh listing (which rewrites the cache
  // app-side). Same-revision extensions re-register as no-ops, so an
  // unchanged vault causes zero UI churn; the enablement pass then
  // applies the fresh policy (disabling anything newly blocked).
  await loadPolicyAndConsents();
  try {
    registerListing((await ListVaultPlugins()) ?? []);
  } catch {
    // Vault unreachable — cached registrations (or built-ins) stand.
  }
  await applyEnablement();
}

/** Register one listing's extensions and prune vault extensions absent
 * from it. Callers must pass a SUCCESSFUL listing: ListVaultPlugins
 * throws on failure rather than returning empty, so a transient backend
 * error never reads as "no extensions" and tears down what's running. */
function registerListing(listed: main.VaultPlugin[]): void {
  const seen = new Set<string>();
  for (const vp of listed) {
    try {
      const manifest = parseVaultManifest(vp.manifest);
      registerVaultPlugin(manifest, vp.source, vp.scope, vp.version);
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
}

/** Enable/disable every registered extension per the current decisions,
 * policy, and consents. */
async function applyEnablement(): Promise<void> {
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
 * stale row behind. The prune runs ONLY on a successful listing (see
 * registerListing). */
export async function refreshVaultPlugins(): Promise<void> {
  try {
    registerListing((await ListVaultPlugins()) ?? []);
  } catch {
    // Listing failed — leave the registry as-is; the next boot or
    // refresh reconciles.
  }
}

/** Test/dev helper. */
export function resetBoot(): void {
  booted = false;
}
