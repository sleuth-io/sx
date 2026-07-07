// Boots the extension system: registers built-ins with the host, runs the
// loader preflight, loads the org policy + user decisions, and enables
// what the user INTENDS (unknown ids fall back to their default:
// built-ins on). A load failure never rewrites intent — the extension
// shows its error in the Extensions screen and retries next boot.

import { AppVersion, PluginDecisions } from "../../wailsjs/go/main/App";
import {
  registerBuiltIn,
  enablePlugin,
  loaderPreflight,
  setAppVersion,
} from "./host";
import { loadPolicyAndConsents, policyBlocks } from "./policy";
import PublishDoctor, {
  publishDoctorManifest,
} from "./builtins/publish-doctor";
import LibraryDashboard, {
  libraryDashboardManifest,
} from "./builtins/library-dashboard";

const BUILT_INS = [libraryDashboardManifest.id, publishDoctorManifest.id];

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

  let decisions: Record<string, boolean> = {};
  try {
    decisions = (await PluginDecisions()) ?? {};
  } catch {
    // No profile yet (onboarding) — defaults apply next boot.
  }
  for (const id of BUILT_INS) {
    const intended = decisions[id] ?? true; // built-ins default on
    if (!intended || policyBlocks(id)) continue;
    try {
      await enablePlugin(id);
    } catch (e) {
      // A broken extension must never take the app down — and must not
      // lose its intent: it stays "wanted" and retries next boot.
      console.error(`extension ${id} failed to enable`, e);
    }
  }
}

/** Test/dev helper. */
export function resetBoot(): void {
  booted = false;
}
