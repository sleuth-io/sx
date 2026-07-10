// User Leaderboard — built-in widget extension. Horizontal bars of the
// top users by distinct assets used in the last 30 days.

import type { PluginManifest, SxAPI, SxPlugin, ViewMount } from "../api";
import { barList, CHART_COLORS, headline, loadingSkeleton } from "./charts";

export const leaderboardWidgetManifest: PluginManifest = {
  id: "leaderboard-widget",
  name: "User Leaderboard widget",
  version: "1.0.0",
  description:
    "Dashboard bars: your most active users by distinct assets used in the last 30 days.",
  author: "sx",
  permissions: ["views:dashboard", "usage:read"],
};

const TOP_USERS = 5;

export default class LeaderboardWidget implements SxPlugin {
  onload(sx: SxAPI): void {
    sx.registerDashboardWidget({
      id: "user-leaderboard",
      title: "User leaderboard · last 30 days",
      mount: (view) => void this.mount(sx, view),
    });
  }

  onunload(): void {}

  private async mount(sx: SxAPI, view: ViewMount): Promise<void> {
    view.el.replaceChildren(loadingSkeleton());
    try {
      const stats = await sx.usage.userStats(30);
      const top = stats.active.slice(0, TOP_USERS);
      if (top.length === 0) {
        const empty = document.createElement("div");
        empty.className = "px-3 py-4 text-sm text-ink-faint";
        empty.textContent = "No usage recorded in the last 30 days";
        view.el.replaceChildren(empty);
        return;
      }
      view.el.replaceChildren(
        headline(`Top ${top.length} users by assets used`),
        barList(
          top.map((u, i) => ({
            label: u.actor,
            value: u.distinctAssets,
            color: CHART_COLORS[(i + 1) % CHART_COLORS.length],
          })),
          "assets",
        ),
      );
    } catch (e) {
      console.error("leaderboard widget:", e);
      const err = document.createElement("div");
      err.className = "px-3 py-4 text-sm text-ink-faint";
      err.textContent = "Couldn't load usage data: " + [...String(e)].slice(0, 160).join("");
      view.el.replaceChildren(err);
    }
  }
}
