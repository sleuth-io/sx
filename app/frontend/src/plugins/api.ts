// SxAPI v1 — the ONLY surface extensions may touch. Everything here is a
// deliberate, versioned contract (docs/app-plugins-spec.md): if an
// extension needs something this file doesn't offer, the answer is an API
// addition, never an escape hatch to app internals.

export const SX_API_VERSION = "1.0.0";

/** Capabilities an extension may declare. Undeclared calls throw. */
export type Permission =
  | "assets:read"
  | "usage:read"
  | "drafts:write"
  | "views:sidebar"
  | "views:asset-tab"
  | "views:dashboard"
  | "commands"
  | "events";

/** plugin.json — the extension manifest. */
export interface PluginManifest {
  id: string;
  name: string;
  version: string;
  minAppVersion?: string;
  description?: string;
  author?: string;
  permissions: Permission[];
}

/** The default export of an extension's main.js. */
export interface SxPlugin {
  onload(sx: SxAPI): void | Promise<void>;
  onunload?(): void;
}

// ---- Read models (plain data, decoupled from bridge types) ----

export interface AssetSummary {
  name: string;
  type: string;
  description: string;
  updatedAt?: string;
}

export interface AssetFileContent {
  path: string;
  content: string;
}

export interface CollectionSummary {
  name: string;
  description: string;
  assets: string[];
}

export interface UsageEvent {
  timestamp: string;
  actor: string;
  assetName: string;
  assetVersion: string;
  assetType: string;
}

export interface AuditEvent {
  timestamp: string;
  actor: string;
  event: string;
  targetType: string;
  target: string;
}

export interface DraftInput {
  name: string;
  files: AssetFileContent[];
}

// ---- View contracts ----
// Mount points are React-free: the extension receives a bare DOM element
// it owns until dispose. The host removes the element on teardown, so a
// leaked mount can never outlive its extension.

export interface ViewMount {
  el: HTMLElement;
  /** Called before the host removes el; release listeners/timers here. */
  onDispose(cb: () => void): void;
}

export interface SidebarPanelSpec {
  id: string;
  title: string;
  mount(view: ViewMount): void;
}

export interface AssetTabSpec {
  id: string;
  title: string;
  mount(view: ViewMount, ctx: { assetName: string }): void;
}

export interface DashboardWidgetSpec {
  id: string;
  title: string;
  mount(view: ViewMount): void;
}

export interface CommandSpec {
  id: string;
  title: string;
  run(): void | Promise<void>;
}

// ---- Events ----

export interface BeforePublishContext {
  name: string;
  description: string;
  files: AssetFileContent[];
}

/** Warnings surface in the publish sheet; publishing stays allowed. */
export interface PublishWarning {
  message: string;
  detail?: string;
}

export interface EventMap {
  "draft-saved": { draftId: string };
  "asset-published": { name: string };
  "asset-installed": { name: string };
  "vault-synced": Record<string, never>;
}

// ---- The API object handed to onload ----

export interface SxAPI {
  readonly app: { version: string };
  readonly api: { version: string };

  /** Always available. */
  readonly ui: {
    notice(message: string): void;
    confirm(message: string, action: string): Promise<boolean>;
  };

  /** Always available; per plugin, per profile, stored app-side. */
  readonly storage: {
    loadData<T>(): Promise<T | null>;
    saveData<T>(data: T): Promise<void>;
  };

  /** Requires assets:read. */
  readonly assets: {
    list(): Promise<AssetSummary[]>;
    listCollections(): Promise<CollectionSummary[]>;
    readFiles(name: string): Promise<AssetFileContent[]>;
  };

  /** Requires usage:read. */
  readonly usage: {
    events(sinceDays: number): Promise<UsageEvent[]>;
    auditEvents(sinceDays: number): Promise<AuditEvent[]>;
  };

  /** Requires drafts:write. Never publishes — that stays a human action. */
  readonly drafts: {
    create(draft: DraftInput): Promise<{ id: string }>;
  };

  /** Requires views:sidebar. */
  registerSidebarPanel(spec: SidebarPanelSpec): void;
  /** Requires views:asset-tab. */
  registerAssetTab(spec: AssetTabSpec): void;
  /** Requires views:dashboard. */
  registerDashboardWidget(spec: DashboardWidgetSpec): void;
  /** Requires commands. */
  registerCommand(spec: CommandSpec): void;

  /** Requires events. */
  on<K extends keyof EventMap>(
    event: K,
    handler: (payload: EventMap[K]) => void,
  ): void;
  /**
   * Requires events. Subscribers may return warnings that render in the
   * publish sheet (the doctor hook); returning nothing approves silently.
   */
  onBeforePublish(
    handler: (
      ctx: BeforePublishContext,
    ) => PublishWarning[] | Promise<PublishWarning[]> | void,
  ): void;
}
