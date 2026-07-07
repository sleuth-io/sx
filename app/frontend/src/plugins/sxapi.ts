// buildSxAPI constructs the per-extension, permission-filtered API object.
// Undeclared capabilities throw at the call site — the manifest's
// permission list is enforcement, not documentation. Mounted views are
// tracked per plugin so the host can dispose every DOM trace on disable.

import {
  GetAsset,
  ListAssets,
  ListCollections,
  CreateDraftFromFiles,
  ImportDraftsFromFolder,
  PluginLoadData,
  PluginSaveData,
  PluginUsageEvents,
  PluginAuditEvents,
} from "../../wailsjs/go/main/App";
import type {
  AssetTabSpec,
  CommandSpec,
  DashboardWidgetSpec,
  Permission,
  PluginManifest,
  SidebarPanelSpec,
  SxAPI,
  ViewMount,
} from "./api";
import { SX_API_VERSION } from "./api";
import { getAppVersion } from "./host";
import { registerSlotEntry } from "./registry";
import { subscribeBeforePublish, subscribeEvent } from "./events";

// ---- UI services, provided by the app shell ----
// The Library screen registers real implementations on mount; extensions
// loaded before that (or in tests) degrade to console/no-op gracefully.

interface UIHandlers {
  notice(message: string): void;
  confirm(message: string, action: string): Promise<boolean>;
}

let ui: UIHandlers = {
  notice: (m) => console.log("extension notice:", m),
  confirm: async () => false,
};

export function setPluginUIHandlers(handlers: UIHandlers): void {
  ui = handlers;
}

// ---- Mount tracking ----
// The UI slot components call mountEntry() when rendering an extension's
// view; the host calls disposePluginMounts() on disable. Elements and
// dispose callbacks are tracked per plugin so no DOM or listener can
// outlive its owner.

interface TrackedMount {
  el: HTMLElement;
  disposers: (() => void)[];
}

const mounts = new Map<string, TrackedMount[]>();

/** Mount one slot entry into el, tracking it for teardown. Returns a
 * dispose function the React wrapper calls on unmount as well. */
export function mountEntry(
  pluginId: string,
  el: HTMLElement,
  mount: (view: ViewMount) => void,
): () => void {
  const tracked: TrackedMount = { el, disposers: [] };
  const list = mounts.get(pluginId) ?? [];
  mounts.set(pluginId, [...list, tracked]);
  try {
    mount({ el, onDispose: (cb) => tracked.disposers.push(cb) });
  } catch (e) {
    console.error(`extension ${pluginId}: mount failed`, e);
  }
  return () => disposeOne(pluginId, tracked);
}

function disposeOne(pluginId: string, tracked: TrackedMount): void {
  const list = mounts.get(pluginId) ?? [];
  const idx = list.indexOf(tracked);
  if (idx < 0) return; // already disposed (host teardown won the race)
  list.splice(idx, 1);
  for (const d of tracked.disposers) {
    try {
      d();
    } catch (e) {
      console.error(`extension ${pluginId}: dispose failed`, e);
    }
  }
  tracked.el.replaceChildren();
}

export function disposePluginMounts(pluginId: string): void {
  for (const m of [...(mounts.get(pluginId) ?? [])]) {
    disposeOne(pluginId, m);
  }
  mounts.delete(pluginId);
}

// ---- API construction ----

export function buildSxAPI(manifest: PluginManifest): SxAPI {
  const granted = new Set<Permission>(manifest.permissions);
  const id = manifest.id;

  function need(p: Permission): void {
    if (!granted.has(p)) {
      throw new Error(
        `extension ${id} used "${p}" without declaring it in plugin.json`,
      );
    }
  }

  const api: SxAPI = {
    app: { version: getAppVersion() },
    api: { version: SX_API_VERSION },

    ui: {
      notice: (message) => ui.notice(message),
      confirm: (message, action) => ui.confirm(message, action),
    },

    storage: {
      async loadData<T>(): Promise<T | null> {
        const raw = await PluginLoadData(id);
        if (!raw) return null;
        try {
          return JSON.parse(raw) as T;
        } catch {
          return null;
        }
      },
      async saveData<T>(data: T): Promise<void> {
        await PluginSaveData(id, JSON.stringify(data));
      },
    },

    assets: {
      async list() {
        need("assets:read");
        const items = await ListAssets();
        return (items ?? []).map((a) => ({
          name: a.name,
          type: a.type,
          description: a.description,
          updatedAt: a.updatedAt,
        }));
      },
      async listCollections() {
        need("assets:read");
        const cols = await ListCollections();
        return (cols ?? []).map((c) => ({
          name: c.name,
          description: c.description,
          assets: c.assets ?? [],
        }));
      },
      async readFiles(name: string) {
        need("assets:read");
        const detail = await GetAsset(name, "");
        return (detail.files ?? []).map((f) => ({
          path: f.path,
          content: f.content,
        }));
      },
    },

    usage: {
      async events(sinceDays: number) {
        need("usage:read");
        return (await PluginUsageEvents(sinceDays)) ?? [];
      },
      async auditEvents(sinceDays: number) {
        need("usage:read");
        return (await PluginAuditEvents(sinceDays)) ?? [];
      },
    },

    drafts: {
      async create(draft) {
        need("drafts:write");
        const created = await CreateDraftFromFiles(
          draft.name,
          draft.files.map((f) => ({ path: f.path, content: f.content })),
        );
        return { id: created.id };
      },
      async importFromFolder() {
        need("drafts:write");
        const res = await ImportDraftsFromFolder();
        return { created: res.created ?? [], skipped: res.skipped ?? 0 };
      },
    },

    registerSidebarPanel(spec: SidebarPanelSpec) {
      need("views:sidebar");
      registerSlotEntry("sidebar-panel", id, spec);
    },
    registerAssetTab(spec: AssetTabSpec) {
      need("views:asset-tab");
      registerSlotEntry("asset-tab", id, spec);
    },
    registerDashboardWidget(spec: DashboardWidgetSpec) {
      need("views:dashboard");
      registerSlotEntry("dashboard-widget", id, spec);
    },
    registerCommand(spec: CommandSpec) {
      need("commands");
      registerSlotEntry("command", id, spec);
    },

    on(event, handler) {
      need("events");
      subscribeEvent(id, event, handler as (payload: unknown) => void);
    },
    onBeforePublish(handler) {
      need("events");
      subscribeBeforePublish(id, handler);
    },
  };

  return Object.freeze(api);
}
