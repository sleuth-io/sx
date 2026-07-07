// Top Assets by Usage — built-in widget extension. Multi-line daily
// usage for the top assets over the last 30 days, with an average/day,
// total, and trend headline and a per-day hover tooltip.

import type {
  PluginManifest,
  SxAPI,
  SxPlugin,
  UsageEvent,
  ViewMount,
} from "../api";
import { CHART_COLORS, headline, lineChart, type Series } from "./charts";

export const usageTrendsWidgetManifest: PluginManifest = {
  id: "usage-trends-widget",
  name: "Top Assets by Usage widget",
  version: "1.0.0",
  description:
    "Dashboard chart: daily usage of your top assets over the last 30 days, with trend.",
  author: "sx",
  permissions: ["views:dashboard", "usage:read"],
};

const WINDOW_DAYS = 30;
const TOP_ASSETS = 5;

function dayKey(iso: string): string {
  return iso.slice(0, 10);
}

function shortDay(key: string): string {
  const d = new Date(key + "T00:00:00Z");
  return d.toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

export function buildSeries(events: UsageEvent[]): {
  labels: string[];
  series: Series[];
  total: number;
  perDay: number;
  trendPct: number | null;
} {
  const days: string[] = [];
  const now = new Date();
  for (let i = WINDOW_DAYS - 1; i >= 0; i--) {
    const d = new Date(now.getTime() - i * 86_400_000);
    days.push(d.toISOString().slice(0, 10));
  }
  const dayIndex = new Map(days.map((d, i) => [d, i]));

  const totals = new Map<string, number>();
  const buckets = new Map<string, number[]>();
  let total = 0;
  for (const e of events) {
    const idx = dayIndex.get(dayKey(e.timestamp));
    if (idx === undefined) continue;
    total++;
    totals.set(e.assetName, (totals.get(e.assetName) ?? 0) + 1);
    let arr = buckets.get(e.assetName);
    if (!arr) {
      arr = new Array<number>(days.length).fill(0);
      buckets.set(e.assetName, arr);
    }
    arr[idx]++;
  }
  const top = [...totals.entries()]
    .sort((a, b) => b[1] - a[1])
    .slice(0, TOP_ASSETS);
  const series: Series[] = top.map(([name], i) => ({
    name,
    color: CHART_COLORS[i % CHART_COLORS.length],
    values: buckets.get(name) ?? [],
  }));

  // Trend: second half of the window vs the first half.
  const half = Math.floor(days.length / 2);
  let firstHalf = 0;
  let secondHalf = 0;
  for (const arr of buckets.values()) {
    for (let i = 0; i < arr.length; i++) {
      if (i < half) firstHalf += arr[i];
      else secondHalf += arr[i];
    }
  }
  const trendPct =
    firstHalf > 0
      ? Math.round(((secondHalf - firstHalf) / firstHalf) * 100)
      : null;

  return {
    labels: days.map(shortDay),
    series,
    total,
    perDay: Math.round((total / WINDOW_DAYS) * 100) / 100,
    trendPct,
  };
}

export default class UsageTrendsWidget implements SxPlugin {
  onload(sx: SxAPI): void {
    sx.registerDashboardWidget({
      id: "top-assets-usage",
      title: "Top assets by usage",
      mount: (view) => void this.mount(sx, view),
    });
  }

  onunload(): void {}

  private async mount(sx: SxAPI, view: ViewMount): Promise<void> {
    try {
      const events = await sx.usage.events(WINDOW_DAYS);
      const { labels, series, total, perDay, trendPct } = buildSeries(events);
      if (total === 0) {
        const empty = document.createElement("div");
        empty.className = "px-3 py-4 text-sm text-ink-faint";
        empty.textContent = "No usage recorded in the last 30 days";
        view.el.replaceChildren(empty);
        return;
      }
      const trend =
        trendPct === null ? "" : ` — ${trendPct >= 0 ? "↑" : "↓"} ${Math.abs(trendPct)}%`;
      view.el.replaceChildren(
        headline(`${perDay} /day — ${total} total${trend}`),
        lineChart(labels, series),
      );
    } catch (e) {
      console.error("usage trends widget:", e);
      const err = document.createElement("div");
      err.className = "px-3 py-4 text-sm text-ink-faint";
      err.textContent = "Couldn't load usage data";
      view.el.replaceChildren(err);
    }
  }
}
