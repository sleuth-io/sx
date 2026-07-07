// Boots the extension system: registers built-ins with the host, runs the
// loader preflight, and enables whatever the per-profile state says
// (built-ins default on until the user configures otherwise).

import { EnabledPlugins } from "../../wailsjs/go/main/App";
import { registerBuiltIn, enablePlugin, loaderPreflight } from "./host";
import PublishDoctor, {
  publishDoctorManifest,
} from "./builtins/publish-doctor";
import LibraryDashboard, {
  libraryDashboardManifest,
} from "./builtins/library-dashboard";

const DEFAULT_ENABLED = [
  libraryDashboardManifest.id,
  publishDoctorManifest.id,
];

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

  let enabled = DEFAULT_ENABLED;
  try {
    const state = await EnabledPlugins();
    if (state.configured) enabled = state.enabled ?? [];
  } catch {
    // No profile yet (onboarding) — defaults apply next boot.
  }
  for (const id of enabled) {
    try {
      await enablePlugin(id);
    } catch (e) {
      // A broken extension must never take the app down with it.
      console.error(`extension ${id} failed to enable`, e);
    }
  }
}

/** Test/dev helper. */
export function resetBoot(): void {
  booted = false;
}
