// Tiny dependency-free SVG chart builders for the built-in dashboard
// widgets. Plain DOM by design: built-ins exercise the exact ViewMount
// contract third-party extensions get, so anything here is achievable
// with the public API alone.

const SVG_NS = "http://www.w3.org/2000/svg";

export const CHART_COLORS = [
  "#eab308", // amber
  "#3b82f6", // blue
  "#22c55e", // green
  "#ef4444", // red
  "#a855f7", // purple
];

function svgEl<K extends keyof SVGElementTagNameMap>(
  tag: K,
  attrs: Record<string, string>,
): SVGElementTagNameMap[K] {
  const el = document.createElementNS(SVG_NS, tag);
  for (const [k, v] of Object.entries(attrs)) el.setAttribute(k, v);
  return el;
}

export function headline(text: string): HTMLElement {
  const el = document.createElement("div");
  el.className = "px-3 pt-2 text-sm font-medium";
  el.textContent = text;
  return el;
}

/** Donut with two segments (e.g. with/without usage) plus a legend. */
export function donut(
  a: { label: string; value: number; color: string; who?: string[] },
  b: { label: string; value: number; color: string; who?: string[] },
): HTMLElement {
  const wrap = document.createElement("div");
  wrap.className = "flex flex-col items-center gap-1 p-3";
  const total = Math.max(a.value + b.value, 1);
  const size = 150;
  const r = 55;
  const c = size / 2;
  const circumference = 2 * Math.PI * r;
  const svg = svgEl("svg", {
    width: String(size),
    height: String(size),
    viewBox: `0 0 ${size} ${size}`,
  });
  let offset = 0;
  for (const seg of [a, b]) {
    const frac = seg.value / total;
    const ring = svgEl("circle", {
      cx: String(c),
      cy: String(c),
      r: String(r),
      fill: "none",
      stroke: seg.color,
      "stroke-width": "26",
      "stroke-dasharray": `${frac * circumference} ${circumference}`,
      "stroke-dashoffset": String(-offset * circumference),
      transform: `rotate(-90 ${c} ${c})`,
    });
    // Hovering a segment names its people (SVG tooltips need a <title>
    // child, not the attribute).
    if (seg.who?.length) {
      const t = svgEl("title", {});
      t.textContent = whoSummary(seg.label, seg.who);
      ring.appendChild(t);
    }
    svg.appendChild(ring);
    // Count label at the segment's midpoint angle.
    if (seg.value > 0) {
      const mid = (offset + frac / 2) * 2 * Math.PI - Math.PI / 2;
      const label = svgEl("text", {
        x: String(c + Math.cos(mid) * r),
        y: String(c + Math.sin(mid) * r + 4),
        "text-anchor": "middle",
        fill: "#fff",
        "font-size": "12",
        "font-weight": "600",
      });
      label.textContent = String(seg.value);
      svg.appendChild(label);
    }
    offset += frac;
  }
  wrap.appendChild(svg);
  const legend = document.createElement("div");
  legend.className = "flex gap-4 text-xs text-ink-soft";
  for (const seg of [a, b]) {
    const item = document.createElement("span");
    item.className = "flex items-center gap-1.5";
    const dot = document.createElement("span");
    dot.className = "inline-block h-2.5 w-2.5 rounded-sm";
    dot.style.background = seg.color;
    item.append(dot, seg.label);
    if (seg.who?.length) item.title = whoSummary(seg.label, seg.who);
    legend.appendChild(item);
  }
  wrap.appendChild(legend);
  return wrap;
}

/** "With usage: a@x, b@y" — capped so a big org doesn't produce a
 * screen-high native tooltip. */
function whoSummary(label: string, who: string[]): string {
  const MAX = 15;
  const names = who.slice(0, MAX).join(", ");
  const more = who.length > MAX ? ` +${who.length - MAX} more` : "";
  return `${label}: ${names}${more}`;
}

export interface Series {
  name: string;
  color: string;
  /** One value per x bucket, aligned with the labels array. */
  values: number[];
}

/** Multi-line time series with a hover tooltip listing each series'
 * value at the hovered bucket. */
export function lineChart(
  labels: string[],
  series: Series[],
): HTMLElement {
  const wrap = document.createElement("div");
  wrap.className = "relative p-3";
  const w = 420;
  const h = 150;
  const padL = 26;
  const padB = 18;
  const maxY = Math.max(1, ...series.flatMap((s) => s.values));
  const x = (i: number) =>
    padL + (i / Math.max(labels.length - 1, 1)) * (w - padL - 6);
  const y = (v: number) => (h - padB) - (v / maxY) * (h - padB - 8);

  const svg = svgEl("svg", {
    width: "100%",
    viewBox: `0 0 ${w} ${h}`,
    preserveAspectRatio: "none",
  });
  // Axis baselines + y max label
  svg.appendChild(
    svgEl("line", {
      x1: String(padL), y1: String(h - padB), x2: String(w - 4),
      y2: String(h - padB), stroke: "currentColor", "stroke-opacity": "0.2",
    }),
  );
  const yMax = svgEl("text", {
    x: "2", y: "12", fill: "currentColor", "font-size": "9", opacity: "0.6",
  });
  yMax.textContent = String(maxY);
  svg.appendChild(yMax);
  for (const [idx, label] of [0, labels.length - 1].entries()) {
    const tx = svgEl("text", {
      x: String(idx === 0 ? padL : w - 6),
      y: String(h - 4),
      fill: "currentColor",
      "font-size": "9",
      opacity: "0.6",
      "text-anchor": idx === 0 ? "start" : "end",
    });
    tx.textContent = labels[label];
    svg.appendChild(tx);
  }
  for (const s of series) {
    const points = s.values.map((v, i) => `${x(i)},${y(v)}`).join(" ");
    svg.appendChild(
      svgEl("polyline", {
        points,
        fill: "none",
        stroke: s.color,
        "stroke-width": "2",
        "stroke-linejoin": "round",
      }),
    );
  }
  wrap.appendChild(svg);

  // Hover tooltip: nearest bucket, one row per series (mock parity).
  const tip = document.createElement("div");
  tip.className =
    "pointer-events-none absolute z-10 hidden max-w-[260px] rounded-lg border border-line bg-surface px-3 py-2 text-xs shadow-xl";
  wrap.appendChild(tip);
  svg.addEventListener("mousemove", (e) => {
    const rect = svg.getBoundingClientRect();
    const frac = (e.clientX - rect.left) / rect.width;
    const i = Math.round(frac * (labels.length - 1));
    if (i < 0 || i >= labels.length) return;
    tip.replaceChildren();
    const title = document.createElement("div");
    title.className = "mb-1 font-medium";
    title.textContent = labels[i];
    tip.appendChild(title);
    for (const s of [...series].sort(
      (p, q) => q.values[i] - p.values[i],
    )) {
      if (s.values[i] === 0) continue;
      const row = document.createElement("div");
      row.className = "flex items-center gap-2";
      const dot = document.createElement("span");
      dot.className = "inline-block h-2 w-2 rounded-sm";
      dot.style.background = s.color;
      const name = document.createElement("span");
      name.className = "min-w-0 flex-1 truncate";
      name.textContent = s.name;
      const val = document.createElement("span");
      val.textContent = String(s.values[i]);
      row.append(dot, name, val);
      tip.appendChild(row);
    }
    // Clamp by the tooltip's MEASURED width (long asset names blow past
    // any fixed guess and get clipped by the card edge): make it
    // visible first so offsetWidth is real, then position.
    tip.classList.remove("hidden");
    const tipW = tip.offsetWidth;
    const cursorX = e.clientX - rect.left;
    const left =
      cursorX + 12 + tipW > rect.width
        ? Math.max(4, cursorX - tipW - 12)
        : cursorX + 12;
    tip.style.left = `${left}px`;
    tip.style.top = "8px";
  });
  svg.addEventListener("mouseleave", () => tip.classList.add("hidden"));
  return wrap;
}

/** Horizontal bar list (leaderboard). */
export function barList(
  rows: { label: string; value: number; color: string }[],
  unit: string,
): HTMLElement {
  const wrap = document.createElement("div");
  wrap.className = "space-y-2 p-3";
  const max = Math.max(1, ...rows.map((r) => r.value));
  for (const r of rows) {
    const row = document.createElement("div");
    row.className = "flex items-center gap-2 text-xs";
    const name = document.createElement("span");
    name.className = "w-36 shrink-0 truncate text-right text-ink-soft";
    name.textContent = r.label;
    const track = document.createElement("div");
    track.className = "h-4 min-w-0 flex-1";
    const bar = document.createElement("div");
    bar.className = "h-full rounded-sm";
    bar.style.width = `${(r.value / max) * 100}%`;
    bar.style.background = r.color;
    track.appendChild(bar);
    const val = document.createElement("span");
    val.className = "w-16 shrink-0 text-ink-faint";
    val.textContent = `${r.value} ${unit}`;
    row.append(name, track, val);
    wrap.appendChild(row);
  }
  return wrap;
}
