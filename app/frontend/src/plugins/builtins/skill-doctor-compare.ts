// Duplicate detector — the compare panel (evidence before decision).
// Research across dedupe UIs (Apple Photos, Google Contacts, Salesforce,
// HubSpot, Airtable, Ashby) converges on the same shape: show the
// members side by side with their differences highlighted, pre-select a
// recommended survivor, and make the confirm button restate the action
// ("Keep X · retire 2 others") instead of a generic verb. The AI aids
// (adjudicate, merge) live INSIDE the panel as decision support, not as
// peer buttons on the card.

import { uniqueLines, type Cluster, type SkillDoc } from "./skill-doctor-core";

export interface CompareCallbacks {
  onConsolidate(survivor: string): void;
  onAskAI(): void;
  onMergeAI(): void;
  openAsset(name: string): void;
  /** Resolves to a short reach summary ("installed for everyone"). */
  installSummary(name: string): Promise<string>;
}

function el(tag: string, style?: string, text?: string): HTMLElement {
  const node = document.createElement(tag);
  if (style) node.style.cssText = style;
  if (text !== undefined) node.textContent = text;
  return node;
}

const FAINT = "color: var(--color-ink-faint);";

/** One member column: survivor radio + metadata header + body. */
function memberColumn(
  doc: SkillDoc,
  opts: {
    recommended: boolean;
    radioName: string;
    highlight: Set<number> | null;
    onPick(name: string): void;
    openAsset(name: string): void;
    installSummary(name: string): Promise<string>;
  },
): HTMLElement {
  const col = el(
    "div",
    "display: flex; flex-direction: column; gap: 6px; min-width: 0;",
  );
  const head = el(
    "label",
    "display: flex; gap: 6px; align-items: center; cursor: pointer; flex-wrap: wrap;",
  );
  const radio = document.createElement("input");
  radio.type = "radio";
  radio.name = opts.radioName;
  radio.checked = opts.recommended;
  radio.onchange = () => opts.onPick(doc.name);
  const link = el(
    "a",
    "color: var(--color-accent); cursor: pointer; text-decoration: underline;" +
      "font-weight: 600; font-size: 13px; overflow: hidden; text-overflow: ellipsis;",
    doc.name,
  );
  link.onclick = (e) => {
    e.preventDefault();
    opts.openAsset(doc.name);
  };
  head.append(radio, link);
  if (opts.recommended) {
    head.appendChild(
      el(
        "span",
        "font-size: 10px; font-weight: 600; color: white; background: var(--color-accent);" +
          "padding: 1px 6px; border-radius: 999px;",
        "Recommended · newest",
      ),
    );
  }
  col.appendChild(head);

  const meta = el("div", FAINT + "font-size: 11px;");
  meta.textContent = doc.updatedAt ? `updated ${doc.updatedAt.slice(0, 10)}` : "";
  const reach = el("span", "", "");
  void opts
    .installSummary(doc.name)
    .then((s) => {
      reach.textContent = (meta.textContent ? " · " : "") + s;
    })
    .catch(() => {});
  meta.appendChild(reach);
  col.appendChild(meta);

  const body = el(
    "div",
    "border: 1px solid var(--color-line); border-radius: 8px; padding: 8px;" +
      "background: var(--color-canvas); max-height: 260px; overflow: auto;" +
      "font-family: var(--font-mono); font-size: 11px; line-height: 1.5;" +
      "white-space: pre-wrap; word-break: break-word;",
  );
  const lines = doc.raw.split("\n");
  lines.forEach((line, i) => {
    const ln = el("div", "", line || " ");
    if (opts.highlight?.has(i)) {
      // A line the other variant doesn't have — the delta that should
      // drive the keep/merge decision.
      ln.style.background = "color-mix(in srgb, var(--color-accent) 18%, transparent)";
      ln.style.borderRadius = "3px";
    }
    body.appendChild(ln);
  });
  col.appendChild(body);
  return col;
}

/** The expandable comparison: columns per member, survivor radio in
 * each header, and the decision row at the bottom. */
export function buildComparePanel(
  c: Cluster,
  docs: SkillDoc[],
  recommended: string,
  cb: CompareCallbacks,
): HTMLElement {
  const members = c.members
    .map((name) => docs.find((d) => d.name === name))
    .filter((d): d is SkillDoc => Boolean(d));
  let survivor = recommended;

  const panel = el(
    "div",
    "border: 1px solid var(--color-line); border-radius: 8px; padding: 10px;" +
      "background: var(--color-surface); display: flex; flex-direction: column; gap: 8px;",
  );
  panel.appendChild(
    el(
      "div",
      FAINT + "font-size: 12px;",
      "Pick the one to keep — highlighted lines exist only in that variant.",
    ),
  );

  const grid = el(
    "div",
    `display: grid; grid-template-columns: repeat(${Math.min(members.length, 3)}, minmax(0, 1fr)); gap: 10px;`,
  );
  const radioName = `survivor-${c.signature.replace(/\n/g, "-")}`;
  members.forEach((doc, i) => {
    // Pairwise highlight only for two-member clusters — with more, a
    // line-membership diff misleads more than it helps.
    const highlight =
      members.length === 2
        ? uniqueLines(doc.raw, members[1 - i].raw)
        : null;
    grid.appendChild(
      memberColumn(doc, {
        recommended: doc.name === recommended,
        radioName,
        highlight,
        onPick: (name) => {
          survivor = name;
          confirmBtn.textContent = confirmLabel();
        },
        openAsset: cb.openAsset,
        installSummary: cb.installSummary,
      }),
    );
  });
  panel.appendChild(grid);

  const confirmLabel = () =>
    `Keep ${survivor} · retire ${c.members.length - 1} other${c.members.length > 2 ? "s" : ""}`;
  const decision = el(
    "div",
    "display: flex; gap: 10px; align-items: center; flex-wrap: wrap;",
  );
  const confirmBtn = el(
    "button",
    "padding: 5px 12px; font: inherit; font-size: 12px; font-weight: 500;" +
      "border: 1px solid var(--color-accent); border-radius: 8px; cursor: pointer;" +
      "background: var(--color-accent); color: white;",
    confirmLabel(),
  );
  confirmBtn.onclick = () => cb.onConsolidate(survivor);
  const askLink = el(
    "a",
    "color: var(--color-accent); cursor: pointer; text-decoration: underline; font-size: 12px;",
    "Not sure? Ask AI",
  );
  askLink.onclick = (e) => {
    e.preventDefault();
    cb.onAskAI();
  };
  const mergeLink = el(
    "a",
    FAINT + "cursor: pointer; text-decoration: underline; font-size: 12px;",
    "Merge them into a new draft with AI instead…",
  );
  mergeLink.onclick = (e) => {
    e.preventDefault();
    cb.onMergeAI();
  };
  decision.append(confirmBtn, askLink, mergeLink);
  panel.appendChild(decision);
  return panel;
}

/** A resolved cluster collapses to a one-line summary (queue pattern:
 * outcomes stay visible, attention moves to what's left). */
export function buildResolvedCard(summary: string): HTMLElement {
  return el(
    "div",
    "border: 1px solid var(--color-line); border-radius: 12px; padding: 8px 12px;" +
      FAINT +
      "font-size: 12px; background: var(--color-surface);",
    `✓ ${summary}`,
  );
}
