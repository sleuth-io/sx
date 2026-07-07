// Slot registry: the app's pluggable surfaces. Extensions contribute
// entries through the SxAPI; core UI reads them through useSlot(). Every
// registration is owned by a plugin id so the host can tear down a
// disabled extension completely — nothing can outlive its owner.

import { useSyncExternalStore } from "react";
import type {
  AssetTabSpec,
  CommandSpec,
  DashboardWidgetSpec,
  SidebarPanelSpec,
} from "./api";

export type SlotKind =
  | "sidebar-panel"
  | "asset-tab"
  | "dashboard-widget"
  | "command";

type SpecFor = {
  "sidebar-panel": SidebarPanelSpec;
  "asset-tab": AssetTabSpec;
  "dashboard-widget": DashboardWidgetSpec;
  command: CommandSpec;
};

export interface SlotEntry<K extends SlotKind = SlotKind> {
  pluginId: string;
  spec: SpecFor[K];
}

const entries = new Map<SlotKind, SlotEntry[]>();
const listeners = new Set<() => void>();
// Snapshots are cached per kind: useSyncExternalStore requires a stable
// getSnapshot result between notifications or React loops re-rendering.
const snapshots = new Map<SlotKind, SlotEntry[]>();

function notify() {
  snapshots.clear();
  for (const l of listeners) l();
}

export function registerSlotEntry<K extends SlotKind>(
  kind: K,
  pluginId: string,
  spec: SpecFor[K],
): void {
  const list = entries.get(kind) ?? [];
  const id = (spec as { id: string }).id;
  // Uniqueness is per extension: ids are namespaced by owner everywhere
  // they're consumed, so one extension picking a popular id must not be
  // able to block another from loading.
  if (
    list.some(
      (e) => e.pluginId === pluginId && (e.spec as { id: string }).id === id,
    )
  ) {
    throw new Error(`${kind} "${id}" is already registered by ${pluginId}`);
  }
  entries.set(kind, [...list, { pluginId, spec }]);
  notify();
}

/** Remove every registration owned by pluginId, across all slots. */
export function unregisterPlugin(pluginId: string): void {
  let changed = false;
  for (const [kind, list] of entries) {
    const kept = list.filter((e) => e.pluginId !== pluginId);
    if (kept.length !== list.length) {
      entries.set(kind, kept);
      changed = true;
    }
  }
  if (changed) notify();
}

export function slotEntries<K extends SlotKind>(kind: K): SlotEntry<K>[] {
  let snap = snapshots.get(kind);
  if (!snap) {
    snap = [...(entries.get(kind) ?? [])];
    snapshots.set(kind, snap);
  }
  return snap as SlotEntry<K>[];
}

function subscribe(cb: () => void): () => void {
  listeners.add(cb);
  return () => listeners.delete(cb);
}

/** React view of a slot; re-renders when registrations change. */
export function useSlot<K extends SlotKind>(kind: K): SlotEntry<K>[] {
  return useSyncExternalStore(subscribe, () => slotEntries(kind));
}

/** Test/dev helper: a clean slate. */
export function resetSlots(): void {
  entries.clear();
  notify();
}
