// Library Dashboard — built-in extension. Adoption and health widgets
// over the vault's usage/audit streams. Deliberately plain-DOM: built-ins
// exercise the exact ViewMount contract third-party extensions get (a
// bare element, no React), proving the API is sufficient.

import type {
  PluginManifest,
  SxAPI,
  SxPlugin,
  UsageEvent,
  ViewMount,
} from "../api";

export const libraryDashboardManifest: PluginManifest = {
  id: "library-dashboard",
  name: "Library Dashboard",
  version: "1.0.0",
  description:
    "Adoption and health at a glance: recent activity, most-used skills, and assets going stale.",
  author: "sx",
  permissions: ["views:dashboard", "usage:read", "assets:read", "commands"],
};

function h(
  tag: string,
  className: string,
  text?: string,
): HTMLElement {
  const el = document.createElement(tag);
  if (className) el.className = className;
  if (text) el.textContent = text;
  return el;
}

function emptyNote(text: string): HTMLElement {
  return h("div", "px-3 py-4 text-sm text-ink-faint", text);
}

function errorNote(e: unknown): HTMLElement {
  // Raw backend errors can be pages long (HTML gateway bodies); a widget
  // shows one readable line and leaves detail to the console.
  console.error("dashboard widget:", e);
  let msg = String(e).replace(/\s+/g, " ");
  if (msg.length > 120) msg = msg.slice(0, 120) + "…";
  return h("div", "px-3 py-4 text-sm text-ink-faint", "Couldn't load: " + msg);
}

function rowList(
  rows: { label: string; value: string }[],
  empty: string,
): HTMLElement {
  if (rows.length === 0) return emptyNote(empty);
  const list = h("ul", "divide-y divide-line");
  for (const r of rows) {
    const li = h("li", "flex items-center gap-2 px-3 py-2 text-sm");
    li.appendChild(h("span", "min-w-0 flex-1 truncate", r.label));
    li.appendChild(h("span", "shrink-0 text-xs text-ink-faint", r.value));
    list.appendChild(li);
  }
  return list;
}

function daysAgo(iso: string): number {
  return Math.floor((Date.now() - Date.parse(iso)) / 86_400_000);
}

export default class LibraryDashboard implements SxPlugin {
  onload(sx: SxAPI): void {
    sx.registerDashboardWidget({
      id: "most-used",
      title: "Most used (30 days)",
      mount: (view) => void this.mountMostUsed(sx, view),
    });
    sx.registerDashboardWidget({
      id: "going-stale",
      title: "Going stale",
      mount: (view) => void this.mountGoingStale(sx, view),
    });
    sx.registerDashboardWidget({
      id: "recent-activity",
      title: "Recent library changes",
      mount: (view) => void this.mountRecentActivity(sx, view),
    });
    sx.registerCommand({
      id: "refresh-dashboard",
      title: "Dashboard: refresh widgets",
      run: () => sx.ui.notice("Dashboard refreshes when reopened"),
    });
  }

  onunload(): void {
    // Registrations and mounts are torn down by the host.
  }

  private async mountMostUsed(sx: SxAPI, view: ViewMount): Promise<void> {
    view.el.appendChild(emptyNote("Loading…"));
    try {
      const events = await sx.usage.events(30);
      const counts = new Map<string, number>();
      for (const e of events) {
        counts.set(e.assetName, (counts.get(e.assetName) ?? 0) + 1);
      }
      const rows = [...counts.entries()]
        .sort((a, b) => b[1] - a[1])
        .slice(0, 8)
        .map(([name, n]) => ({
          label: name,
          value: `${n} ${n === 1 ? "use" : "uses"}`,
        }));
      view.el.replaceChildren(
        rowList(rows, "No usage recorded in the last 30 days"),
      );
    } catch (e) {
      view.el.replaceChildren(errorNote(e));
    }
  }

  private async mountGoingStale(sx: SxAPI, view: ViewMount): Promise<void> {
    view.el.appendChild(emptyNote("Loading…"));
    try {
      const [assets, events] = await Promise.all([
        sx.assets.list(),
        sx.usage.events(90),
      ]);
      const lastUse = new Map<string, UsageEvent>();
      for (const e of events) {
        if (!lastUse.has(e.assetName)) lastUse.set(e.assetName, e);
      }
      const rows = assets
        .map((a) => {
          const last = lastUse.get(a.name);
          return {
            name: a.name,
            days: last ? daysAgo(last.timestamp) : Infinity,
          };
        })
        .filter((a) => a.days >= 30)
        .sort((a, b) => b.days - a.days)
        .slice(0, 8)
        .map((a) => ({
          label: a.name,
          value: a.days === Infinity ? "never used" : `${a.days}d ago`,
        }));
      view.el.replaceChildren(
        rowList(rows, "Everything has been used in the last 30 days"),
      );
    } catch (e) {
      view.el.replaceChildren(errorNote(e));
    }
  }

  private async mountRecentActivity(
    sx: SxAPI,
    view: ViewMount,
  ): Promise<void> {
    view.el.appendChild(emptyNote("Loading…"));
    try {
      const events = await sx.usage.auditEvents(14);
      const rows = events.slice(0, 10).map((e) => ({
        label: `${e.event.replace(/[._]/g, " ")} — ${e.target}`,
        value: `${daysAgo(e.timestamp)}d`,
      }));
      view.el.replaceChildren(
        rowList(rows, "No library changes in the last 14 days"),
      );
    } catch (e) {
      view.el.replaceChildren(errorNote(e));
    }
  }
}
