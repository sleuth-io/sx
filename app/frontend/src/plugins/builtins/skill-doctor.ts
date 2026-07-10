// Skill Doctor — built-in extension, phases 1–2 of
// docs/skill-dedupe-spec.md: detect duplicate skills (local hash +
// TF-IDF, plus an LLM catalog sweep for semantic duplicates), then act:
// KEEP ONE (move every duplicate's installations onto a survivor and
// retire the rest — recoverable, but destructive enough to confirm
// loudly) or MERGE WITH AI (compose one definitive SKILL.md as a DRAFT;
// publishing stays a human action). Dismissals are team-shared. Every
// scan runs fresh per mount — results are per-library and an instance
// survives library switches.

import type { SxAPI, SxPlugin, ViewMount, PluginManifest } from "../api";
import {
  adjudicateMessages,
  cluster,
  membersBySimilarity,
  mergeMessages,
  mergeSweepGroups,
  normalizeSkillFiles,
  sha256,
  similarityMatrix,
  sweepMessages,
  tokenize,
  MAX_LLM_MEMBERS,
  NEAR_IDENTICAL,
  SWEEP_SCHEMA,
  VERDICT_SCHEMA,
  type Cluster,
  type SkillDoc,
} from "./skill-doctor-core";

export const skillDoctorManifest: PluginManifest = {
  id: "skill-doctor",
  name: "Skill Doctor",
  version: "1.1.0",
  description:
    "Find duplicate and overlapping skills, then fix them: keep one (installations move onto it) or merge them into a new draft with AI.",
  author: "sx",
  permissions: [
    "assets:read",
    "assets:consolidate",
    "drafts:write",
    "views:main",
    "commands",
    "llm:use",
    "storage:shared",
  ],
};

const READ_CONCURRENCY = 8;

interface SharedDoc {
  dismissed?: Record<string, { by: string; at: string }>;
}

interface Verdict {
  verdict: "duplicate" | "overlapping" | "distinct";
  reasoning: string;
  recommendation: string;
}

function el(tag: string, style?: string, text?: string): HTMLElement {
  const node = document.createElement(tag);
  if (style) node.style.cssText = style;
  if (text !== undefined) node.textContent = text;
  return node;
}

const FAINT = "color: var(--color-ink-faint);";
const CARD =
  "border: 1px solid var(--color-line); border-radius: 12px; padding: 12px;" +
  "background: var(--color-surface); display: flex; flex-direction: column; gap: 8px;";
const BUTTON =
  "padding: 5px 10px; font: inherit; font-size: 12px; font-weight: 500;" +
  "border: 1px solid var(--color-line); border-radius: 8px; cursor: pointer;" +
  "background: var(--color-surface); color: var(--color-ink);";
const PRIMARY =
  BUTTON +
  "background: var(--color-accent); border-color: var(--color-accent); color: white;";
const NOTE =
  "border: 1px solid var(--color-line); border-radius: 8px; padding: 8px 10px;" +
  "background: var(--color-canvas); font-size: 12px; line-height: 1.5;";

export default class SkillDoctor implements SxPlugin {
  private sx!: SxAPI;
  private docs: SkillDoc[] = [];
  private clusters: Cluster[] = [];
  private dismissed: Record<string, { by: string; at: string }> = {};
  private verdicts = new Map<string, Verdict | string>();
  private busy = new Map<string, string>(); // signature -> action label
  private pickerOpen = new Set<string>(); // signatures with Keep-one open
  private scanning = false;
  private status = "";
  private aiNote = "";
  private rerender: (() => void) | null = null;

  onload(sx: SxAPI): void {
    this.sx = sx;
    sx.registerMainView({
      id: "skill-doctor",
      title: "Skill Doctor",
      mount: (view) => void this.mount(view),
    });
    sx.registerCommand({
      id: "scan-duplicates",
      title: "Skill Doctor: scan for duplicate skills",
      run: () => sx.ui.openView("skill-doctor"),
    });
  }

  onunload(): void {}

  private async mount(view: ViewMount): Promise<void> {
    let disposed = false;
    view.onDispose(() => {
      disposed = true;
      this.rerender = null;
    });
    const root = view.el;
    root.style.cssText = "display: flex; flex-direction: column; gap: 12px;";
    root.replaceChildren();
    const body = el("div", "display: flex; flex-direction: column; gap: 10px;");
    root.append(this.header(), body);

    this.rerender = () => {
      if (disposed) return;
      body.replaceChildren(...this.renderBody());
    };
    this.rerender();
    // Always rescan on mount: this instance outlives library switches,
    // so cached results may belong to a DIFFERENT vault.
    await this.scan();
  }

  private header(): HTMLElement {
    const row = el(
      "div",
      "display: flex; gap: 10px; align-items: center; flex-wrap: wrap;",
    );
    const title = el("div", "", "");
    title.append(
      el("div", "font-weight: 600; font-size: 14px;", "Duplicate skills"),
      el(
        "div",
        FAINT + "font-size: 12px;",
        "Found by content similarity plus an AI sweep of the whole catalog. " +
          "Keep one (installations move onto it) or merge them into a new draft.",
      ),
    );
    const spacer = el("div", "flex: 1;");
    const rescan = el("button", BUTTON, "Rescan");
    rescan.onclick = () => void this.scan();
    row.append(title, spacer, rescan);
    return row;
  }

  private renderBody(): HTMLElement[] {
    if (this.scanning) {
      return [el("div", FAINT + "font-size: 13px; padding: 8px;", this.status)];
    }
    const out: HTMLElement[] = [];
    if (this.aiNote) {
      out.push(el("div", FAINT + "font-size: 12px;", this.aiNote));
    }
    const visible = this.clusters.filter((c) => !this.dismissed[c.signature]);
    const hidden = this.clusters.length - visible.length;
    if (visible.length === 0) {
      out.push(
        el(
          "div",
          FAINT + "font-size: 13px; padding: 8px;",
          `No duplicates found across ${this.docs.length} skills.` +
            (hidden > 0 ? ` ${hidden} dismissed cluster(s) hidden.` : ""),
        ),
      );
      return out;
    }
    for (const c of visible) out.push(this.clusterCard(c));
    if (hidden > 0) {
      out.push(
        el(
          "div",
          FAINT + "font-size: 12px; text-align: center;",
          `${hidden} dismissed cluster(s) hidden — they stay hidden for the whole team.`,
        ),
      );
    }
    return out;
  }

  private clusterCard(c: Cluster): HTMLElement {
    const card = el("div", CARD);
    const head = el(
      "div",
      "display: flex; gap: 8px; align-items: center; flex-wrap: wrap;",
    );
    const tier = c.exact
      ? { label: "Exact duplicates", bg: "var(--color-accent)" }
      : c.score >= NEAR_IDENTICAL
        ? { label: "Near-identical", bg: "var(--color-accent)" }
        : c.aiReason
          ? { label: "AI-flagged", bg: "var(--color-ink-faint)" }
          : { label: "Similar", bg: "var(--color-ink-faint)" };
    head.append(
      el(
        "span",
        `font-size: 11px; font-weight: 600; color: white; background: ${tier.bg};` +
          "padding: 2px 8px; border-radius: 999px;",
        tier.label,
      ),
      el(
        "span",
        FAINT + "font-size: 12px;",
        c.exact ? "identical content" : `${Math.round(c.score * 100)}% similar`,
      ),
    );
    card.appendChild(head);
    if (c.aiReason) {
      card.appendChild(el("div", FAINT + "font-size: 12px;", c.aiReason));
    }

    const list = el("div", "display: flex; flex-direction: column; gap: 4px;");
    for (const name of c.members) {
      const doc = this.docs.find((d) => d.name === name);
      const rowEl = el(
        "div",
        "display: flex; gap: 8px; align-items: baseline; font-size: 13px;",
      );
      const link = el(
        "a",
        "color: var(--color-accent); cursor: pointer; text-decoration: underline; white-space: nowrap;",
        name,
      );
      link.onclick = (e) => {
        e.preventDefault();
        this.sx.ui.openAsset(name);
      };
      rowEl.append(
        link,
        el(
          "span",
          FAINT +
            "font-size: 12px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;",
          doc?.description || "",
        ),
      );
      list.appendChild(rowEl);
    }
    card.appendChild(list);

    const verdict = this.verdicts.get(c.signature);
    if (verdict) card.appendChild(this.verdictBlock(verdict));
    if (this.pickerOpen.has(c.signature)) {
      card.appendChild(this.keepOnePicker(c));
    }

    const busyLabel = this.busy.get(c.signature);
    const actions = el("div", "display: flex; gap: 8px; flex-wrap: wrap;");
    const keep = el("button", PRIMARY, "Keep one…");
    keep.onclick = () => {
      if (this.pickerOpen.has(c.signature)) this.pickerOpen.delete(c.signature);
      else this.pickerOpen.add(c.signature);
      this.rerender?.();
    };
    const merge = el("button", BUTTON, "Merge with AI…");
    merge.onclick = () => void this.mergeCluster(c);
    const ask = el(
      "button",
      BUTTON,
      verdict ? "Ask AI again" : "Ask AI: same skill?",
    );
    ask.onclick = () => void this.adjudicate(c);
    const dismiss = el("button", BUTTON, "Dismiss for the team");
    dismiss.onclick = () => void this.dismiss(c);
    for (const b of [keep, merge, ask, dismiss]) {
      if (busyLabel) b.setAttribute("disabled", "true");
      actions.appendChild(b);
    }
    if (busyLabel) {
      actions.appendChild(el("span", FAINT + "font-size: 12px;", busyLabel));
    }
    card.appendChild(actions);
    return card;
  }

  private verdictBlock(v: Verdict | string): HTMLElement {
    const box = el("div", NOTE);
    if (typeof v === "string") {
      box.textContent = v;
      return box;
    }
    box.append(
      el("div", "font-weight: 600;", `AI verdict: ${v.verdict}`),
      el("div", "", v.reasoning),
      el("div", FAINT, `Recommendation: ${v.recommendation}`),
    );
    return box;
  }

  /** The Keep-one panel: pick the survivor, see each member's reach,
   * and confirm the (destructive, recoverable) consolidation. */
  private keepOnePicker(c: Cluster): HTMLElement {
    const box = el(
      "div",
      NOTE + "display: flex; flex-direction: column; gap: 6px;",
    );
    box.appendChild(
      el(
        "div",
        "font-weight: 600;",
        "Keep which one? The others' installations move onto it, then they are retired.",
      ),
    );
    let survivor = c.members[0];
    for (const name of c.members) {
      const row = el(
        "label",
        "display: flex; gap: 8px; align-items: center; cursor: pointer;",
      );
      const radio = document.createElement("input");
      radio.type = "radio";
      radio.name = `keep-${c.signature.replace(/\n/g, "-")}`;
      radio.checked = name === survivor;
      radio.onchange = () => {
        survivor = name;
      };
      const reach = el("span", FAINT + "font-size: 11px;", "");
      void this.sx.assets
        .installations(name)
        .then((v) => {
          reach.textContent = v.everyone
            ? "installed for everyone"
            : `${v.installations.length} install row(s)`;
        })
        .catch(() => {});
      row.append(radio, el("span", "", name), reach);
      box.appendChild(row);
    }
    const go = el(
      "button",
      PRIMARY + "align-self: flex-start;",
      "Consolidate…",
    );
    go.onclick = () => void this.consolidate(c, survivor);
    box.appendChild(go);
    return box;
  }

  // ---- Actions ----

  private async scan(): Promise<void> {
    if (this.scanning) return;
    this.scanning = true;
    this.verdicts.clear();
    this.pickerOpen.clear();
    this.aiNote = "";
    this.status = "Reading skills…";
    this.rerender?.();
    try {
      const assets = (await this.sx.assets.list()).filter(
        (a) => a.type === "skill",
      );
      const docs: SkillDoc[] = [];
      let done = 0;
      let next = 0;
      const worker = async () => {
        while (next < assets.length) {
          const a = assets[next++];
          try {
            const files = await this.sx.assets.readFiles(a.name);
            const mdFiles = files.filter((f) =>
              f.path.toLowerCase().endsWith(".md"),
            );
            const raw = mdFiles.map((f) => f.content).join("\n\n");
            const text = normalizeSkillFiles(mdFiles.map((f) => f.content));
            if (text) {
              docs.push({
                name: a.name,
                description: a.description,
                raw,
                text,
                hash: await sha256(text),
                tokens: tokenize(text),
              });
            }
          } catch {
            // Unreadable asset — skip it; a rescan retries.
          }
          done++;
          this.status = `Reading skills… ${done}/${assets.length}`;
          this.rerender?.();
        }
      };
      await Promise.all(
        Array.from(
          { length: Math.min(READ_CONCURRENCY, assets.length) },
          worker,
        ),
      );
      docs.sort((a, b) => a.name.localeCompare(b.name));
      this.docs = docs;
      const sim = similarityMatrix(docs.map((d) => d.tokens));
      this.clusters = cluster(docs, sim);

      // The AI sweep: semantic duplicates the local pass can't see.
      // Best-effort — no provider or a failed call degrades to local
      // results with a visible note, never a broken scan.
      const provider = await this.sx.llm.provider().catch(() => "");
      if (provider && docs.length >= 2) {
        this.status = `Asking ${provider} to sweep the catalog for semantic duplicates…`;
        this.rerender?.();
        try {
          const result = await this.sx.llm.complete({
            messages: sweepMessages(docs),
            schema: SWEEP_SCHEMA,
            maxTokens: 2048,
          });
          const groups = (
            result.json as { groups: { members: string[]; reason: string }[] }
          ).groups;
          this.clusters = mergeSweepGroups(docs, sim, this.clusters, groups);
        } catch (e) {
          this.aiNote = `AI sweep unavailable (${String((e as Error)?.message || e)}) — showing local matches only.`;
        }
      } else if (!provider) {
        this.aiNote =
          "No AI provider configured — showing local matches only. Configure one in Settings for the semantic sweep.";
      }

      const shared = await this.sx.sharedStorage
        .load<SharedDoc>()
        .catch(() => null);
      this.dismissed = shared?.dismissed ?? {};
    } finally {
      this.scanning = false;
      this.rerender?.();
    }
  }

  /** Consolidate: THE destructive action. Reach is unioned onto the
   * survivor and the rest are retired — recoverable from version
   * history, but gone from the library, so the confirm says exactly
   * that and names every asset. */
  private async consolidate(c: Cluster, survivor: string): Promise<void> {
    const losers = c.members.filter((m) => m !== survivor);
    const ok = await this.sx.ui.confirm(
      `Keep “${survivor}” and retire ${losers.map((l) => `“${l}”`).join(", ")}? ` +
        "Their installations move onto the kept skill so nobody loses it. " +
        "Retired skills leave the library (recoverable from version history).",
      "Consolidate",
    );
    if (!ok) return;
    this.busy.set(c.signature, "Consolidating…");
    this.rerender?.();
    try {
      const result = await this.sx.assets.consolidate({
        into: survivor,
        from: losers,
      });
      let msg = `Kept “${survivor}”, retired ${result.retired.length} skill(s)`;
      if (result.movedInstallations > 0) {
        msg += `, moved ${result.movedInstallations} installation(s)`;
      }
      if (result.skipped.length > 0) {
        msg += ` — ${result.skipped.length} install move(s) refused: ${result.skipped.join("; ")}`;
      }
      this.sx.ui.notice(msg);
      this.busy.delete(c.signature);
      await this.scan();
    } catch (e) {
      this.busy.delete(c.signature);
      this.verdicts.set(c.signature, String((e as Error)?.message || e));
      this.rerender?.();
    }
  }

  /** Merge: compose ONE definitive skill from the variants as a DRAFT.
   * Nothing is retired here — the user reviews and publishes the draft,
   * then Keep one moves reach onto it. Publish stays a human action. */
  private async mergeCluster(c: Cluster): Promise<void> {
    const members = membersBySimilarity(c)
      .map((name) => this.docs.find((d) => d.name === name))
      .filter((d): d is SkillDoc => Boolean(d))
      .slice(0, MAX_LLM_MEMBERS);
    if (members.length < 2) return;
    this.busy.set(c.signature, "Merging with AI…");
    this.rerender?.();
    try {
      const result = await this.sx.llm.complete({
        messages: mergeMessages(members),
        maxTokens: 8192,
      });
      const content = result.text
        .replace(/^```[a-z]*\n?/, "")
        .replace(/\n?```\s*$/, "");
      const name =
        (content.match(/^name:\s*([a-z0-9-]+)/m) || [])[1] ||
        `${members[0].name}-merged`;
      await this.sx.drafts.create({
        name,
        files: [{ path: "SKILL.md", content }],
      });
      this.sx.ui.notice(
        `Draft “${name}” created from ${members.length} variants — review and publish it, then use Keep one to retire the originals onto it.`,
      );
    } catch (e) {
      this.verdicts.set(c.signature, String((e as Error)?.message || e));
    }
    this.busy.delete(c.signature);
    this.rerender?.();
  }

  /** Team-shared dismissal: keyed by the cluster's member set, so the
   * same grouping stays hidden for everyone until its membership
   * changes (a new near-duplicate re-surfaces it). */
  private async dismiss(c: Cluster): Promise<void> {
    const who = (await this.sx.app.currentUser().catch(() => "")) || "someone";
    const shared =
      (await this.sx.sharedStorage.load<SharedDoc>().catch(() => null)) ?? {};
    shared.dismissed = shared.dismissed ?? {};
    shared.dismissed[c.signature] = { by: who, at: new Date().toISOString() };
    await this.sx.sharedStorage.save(shared);
    this.dismissed = shared.dismissed;
    this.sx.ui.notice("Cluster dismissed for the whole library");
    this.rerender?.();
  }

  /** One structured completion through sx.llm — whatever provider the
   * user configured answers, and the schema keeps the reply renderable. */
  private async adjudicate(c: Cluster): Promise<void> {
    this.busy.set(c.signature, "Asking your AI provider…");
    this.rerender?.();
    const members = membersBySimilarity(c)
      .map((name) => this.docs.find((d) => d.name === name))
      .filter((d): d is SkillDoc => Boolean(d));
    const sent = members.slice(0, MAX_LLM_MEMBERS);
    try {
      const result = await this.sx.llm.complete({
        messages: adjudicateMessages(sent, members.length - sent.length),
        schema: VERDICT_SCHEMA,
        maxTokens: 1024,
      });
      this.verdicts.set(c.signature, result.json as Verdict);
    } catch (e) {
      this.verdicts.set(c.signature, String((e as Error)?.message || e));
    }
    this.busy.delete(c.signature);
    this.rerender?.();
  }
}
