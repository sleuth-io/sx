// Extension updates. An update is nothing special: republish the
// marketplace's latest bundle into the vault (the exact install path),
// then reload the plugin. Availability is a version comparison between
// installed manifests and the marketplace catalog, matched by plugin id
// (installed app-plugin assets are always named by id).

import {
  InstallMarketplaceExtension,
  SetPluginDecision,
} from "../../wailsjs/go/main/App";
import type { main } from "../../wailsjs/go/models";
import { refreshVaultPlugins } from "./boot";
import {
  compareVersions,
  disablePlugin,
  enablePlugin,
  listPlugins,
} from "./host";
import { hasConsent } from "./policy";
import type { PluginManifest } from "./api";

export interface UpdateInfo {
  entry: main.MarketplaceExtension;
  installedVersion: string;
}

/** Marketplace entries that are newer than what's installed. Built-ins
 * never update from a marketplace; broken version strings compare as
 * equal and drop out. */
export function findUpdates(
  catalog: main.MarketplaceExtension[],
): UpdateInfo[] {
  const out: UpdateInfo[] = [];
  for (const entry of catalog) {
    const installed = listPlugins().find(
      (p) => !p.builtIn && p.manifest.id === entry.id,
    );
    if (!installed) continue;
    if (compareVersions(installed.manifest.version, entry.version) < 0) {
      out.push({ entry, installedVersion: installed.manifest.version });
    }
  }
  return out;
}

export type UpdateOutcome =
  | { state: "enabled" }
  | { state: "installed" }
  /** The update changed the permission set of a running extension —
   * it's staged disabled and the caller re-prompts consent. */
  | { state: "needs-consent"; manifest: PluginManifest };

/** Fetch + republish the marketplace's latest, then reload the plugin.
 * A running extension comes back up on the new code immediately unless
 * its permissions changed, in which case consent decides. */
export async function applyUpdate(
  entry: main.MarketplaceExtension,
): Promise<UpdateOutcome> {
  const wasEnabled = !!listPlugins().find(
    (p) => p.manifest.id === entry.id && p.enabled,
  );
  await InstallMarketplaceExtension(entry.assetName);
  await refreshVaultPlugins();
  const updated = listPlugins().find((p) => p.manifest.id === entry.id);
  if (!updated || !wasEnabled) return { state: "installed" };
  // Reload: tear the old code down, bring the new code up.
  disablePlugin(entry.id);
  if (!hasConsent(updated.manifest)) {
    return { state: "needs-consent", manifest: updated.manifest };
  }
  await enablePlugin(entry.id);
  await SetPluginDecision(entry.id, true);
  return { state: "enabled" };
}
