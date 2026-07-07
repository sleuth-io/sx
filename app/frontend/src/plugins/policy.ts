// Extension policy + consent (P2). The vault's [app-plugins] policy is
// read at boot and enforced at enable time; consent is per profile and
// re-prompted whenever an extension's declared permissions change.

import {
  GetPluginPolicy,
  PluginConsents,
  SetPluginConsent,
} from "../../wailsjs/go/main/App";
import type { Permission, PluginManifest } from "./api";

export interface PluginPolicy {
  mode: "open" | "allowlist" | "disabled";
  allowed: string[];
}

let policy: PluginPolicy = { mode: "open", allowed: [] };
let consents: Record<string, string[]> = {};

export async function loadPolicyAndConsents(): Promise<void> {
  try {
    const p = await GetPluginPolicy();
    policy = {
      mode: (p.mode as PluginPolicy["mode"]) || "open",
      allowed: p.allowed ?? [],
    };
  } catch {
    policy = { mode: "open", allowed: [] };
  }
  try {
    consents = (await PluginConsents()) ?? {};
  } catch {
    consents = {};
  }
}

export function currentPolicy(): PluginPolicy {
  return policy;
}

/** Why an extension can't be enabled, or null when it can.
 *
 * The allowlist governs THIRD-PARTY code only: built-ins ship with the
 * app and are already trusted at install, and an org restricting vault
 * extensions must not silently lose Publish Doctor's safety net.
 * `disabled` is the total switch — it turns off everything
 * extension-shaped, built-ins included, deliberately. */
export function policyBlocks(id: string, builtIn: boolean): string | null {
  if (policy.mode === "disabled") {
    return "Extensions are disabled by your organization";
  }
  if (
    policy.mode === "allowlist" &&
    !builtIn &&
    !policy.allowed.includes(id)
  ) {
    return "Blocked by your organization's extension allowlist";
  }
  return null;
}

/** True when the user has consented to exactly this permission set. */
export function hasConsent(manifest: PluginManifest): boolean {
  const granted = consents[manifest.id];
  if (!granted) return false;
  const want = [...manifest.permissions].sort().join(",");
  return [...granted].sort().join(",") === want;
}

export async function recordConsent(manifest: PluginManifest): Promise<void> {
  await SetPluginConsent(manifest.id, [...manifest.permissions]);
  consents[manifest.id] = [...manifest.permissions];
}

// Every FIXED permission must have a description or the build fails —
// only the parameterized net:<host> family is described dynamically.
type FixedPermission = Exclude<Permission, `net:${string}`>;

const PERMISSION_DESCRIPTIONS: Record<FixedPermission, string> = {
  "assets:read": "Read your library's skills, collections, and files",
  "usage:read": "Read your library's usage and change history",
  "drafts:write": "Create and edit drafts (never publishes on its own)",
  "views:sidebar": "Add panels to the sidebar",
  "views:asset-tab": "Add tabs to the skill detail view",
  "views:dashboard": "Add widgets to the dashboard",
  commands: "Add commands to the command palette",
  events: "React to library activity (saves, publishes, installs, syncs)",
  editor: "Read and edit the draft you have open in the editor",
  "views:main": "Add full-page views to the sidebar",
  "assets:write-metadata":
    "Update asset descriptions, keywords, owner, and status (as new revisions)",
  secrets: "Store its own API keys and tokens in your OS keychain",
  "storage:shared":
    "Keep shared state in this library, visible to everyone who uses it",
};

/** Plain-language permission description for the consent sheet. The
 * net:<host> family is described per host — the consent line IS the
 * egress disclosure, so it must name where data can go. */
export function describePermission(p: Permission): string {
  if (p.startsWith("net:")) {
    return `Connect to ${p.slice(4)} over the internet`;
  }
  return PERMISSION_DESCRIPTIONS[p as FixedPermission] ?? p;
}

/** Test/dev helper. */
export function resetPolicy(): void {
  policy = { mode: "open", allowed: [] };
  consents = {};
}
