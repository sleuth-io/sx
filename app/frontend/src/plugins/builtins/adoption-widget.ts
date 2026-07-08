// User Adoption — built-in widget extension. One donut: how many of the
// people this library knows about actually used something in the last 30
// days. Each dashboard widget is its own extension so teams can disable
// or replace them individually.

import type { PluginManifest, SxAPI, SxPlugin, ViewMount } from "../api";
import { donut, headline } from "./charts";

export const adoptionWidgetManifest: PluginManifest = {
  id: "adoption-widget",
  name: "User Adoption widget",
  version: "1.0.0",
  description:
    "Dashboard donut: how many of your known users actually used the library in the last 30 days.",
  author: "sx",
  permissions: ["views:dashboard", "usage:read"],
};

export default class AdoptionWidget implements SxPlugin {
  onload(sx: SxAPI): void {
    sx.registerDashboardWidget({
      id: "user-adoption",
      title: "User adoption · last 30 days",
      mount: (view) => void this.mount(sx, view),
    });
  }

  onunload(): void {}

  private async mount(sx: SxAPI, view: ViewMount): Promise<void> {
    try {
      const stats = await sx.usage.userStats(30);
      const total = stats.knownUsers.length;
      const withUsage = stats.active.length;
      const without = Math.max(total - withUsage, 0);
      const pct = total > 0 ? Math.round((withUsage / total) * 100) : 0;
      // Hover answers the obvious follow-up: WHO is in each group.
      const activeSet = new Set(stats.active.map((u) => u.actor));
      const activeWho = stats.active.map((u) => u.actor).sort();
      const withoutWho = stats.knownUsers.filter((u) => !activeSet.has(u)).sort();
      view.el.replaceChildren(
        headline(
          `${pct}% adoption — ${withUsage} of ${total} users with usage`,
        ),
        donut(
          { label: "Without usage", value: without, color: "#ef4444", who: withoutWho },
          { label: "With usage", value: withUsage, color: "#22c55e", who: activeWho },
        ),
      );
    } catch (e) {
      console.error("adoption widget:", e);
      const err = document.createElement("div");
      err.className = "px-3 py-4 text-sm text-ink-faint";
      err.textContent = "Couldn't load adoption data: " + [...String(e)].slice(0, 160).join("");
      view.el.replaceChildren(err);
    }
  }
}
