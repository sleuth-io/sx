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
  enablePlugin,
  listPlugins,
  loaderPreflight,
  setAppVersion,
} from "./host";
import { hasConsent, loadPolicyAndConsents, policyBlocks } from "./policy";
import PublishDoctor, {
  publishDoctorManifest,
} from "./builtins/publish-doctor";
import LibraryDashboard, {
  libraryDashboardManifest,
} from "./builtins/library-dashboard";
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
  registerBuiltIn(
    libraryDashboardManifest,
    async () => new LibraryDashboard(),
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
  await loadPolicyAndConsents();

  // Vault-installed extensions: published like any asset, scoped to this
  // user, surfaced ONLY in the Extensions screen. Default off; enabling
  // is consent-gated below like everything else.
  try {
    for (const vp of (await ListVaultPlugins()) ?? []) {
      try {
        registerVaultPlugin(parseVaultManifest(vp.manifest), vp.source);
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
    if (!intended || policyBlocks(id)) continue;
    // The consent guarantee holds on EVERY load path, not just the
    // Settings toggle: a vault-installed extension whose permissions
    // changed since consent stays off until the user re-consents (its
    // row in the Extensions screen shows why). Built-ins ship with the
    // app and are implicitly consented at their bundled permission set.
    if (!p.builtIn && !hasConsent(p.manifest)) continue;
    try {
      await enablePlugin(id);
    } catch (e) {
      // A broken extension must never take the app down — and must not
      // lose its intent: it stays "wanted" and retries next boot.
      console.error(`extension ${id} failed to enable`, e);
    }
  }
}

/** Re-scan the vault for extensions (after "Add extension…"). Already-
 * registered ids are left untouched. */
export async function refreshVaultPlugins(): Promise<void> {
  try {
    for (const vp of (await ListVaultPlugins()) ?? []) {
      try {
        registerVaultPlugin(parseVaultManifest(vp.manifest), vp.source);
      } catch (e) {
        console.error(`extension asset ${vp.assetName} rejected:`, e);
      }
    }
  } catch {
    // vault unreachable; the next boot retries
  }
}

/** Test/dev helper. */
export function resetBoot(): void {
  booted = false;
}
