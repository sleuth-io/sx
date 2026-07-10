// Duplicate detector — built-in extension, phases 1–2 of
// docs/skill-dedupe-spec.md. Detects duplicate skills (local hash +
// TF-IDF, plus an LLM catalog sweep for semantic duplicates) and fixes
// them. The review flow follows the patterns good dedupe UIs share
// (Apple Photos, Google Contacts, Salesforce, Ashby): evidence before
// decision (a compare panel with the survivor picker inside), friction
// tiered by confidence (exact duplicates get a one-click keep-newest),
// a recommended survivor pre-selected, one primary action per card, and
// "Not duplicates" as the cheap, instant dismissal (team-shared).
// Results are cached for 12 hours per profile (a profile IS a library,
// so caches can never leak across vaults); Rescan forces a fresh pass.

import type { SxAPI, SxPlugin, ViewMount, PluginManifest } from "../api";
import {
  adjudicateMessages,
  cluster,
  membersBySimilarity,
  mergeMessages,
  mergeSweepGroups,
  normalizeSkillFiles,
  recommendSurvivor,
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
import {
  buildComparePanel,
  buildResolvedCard,
} from "./skill-doctor-compare";

export const skillDoctorManifest: PluginManifest = {
  id: "skill-doctor",
  name: "Duplicate detector",
  version: "1.2.1",
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
const CACHE_TTL_MS = 12 * 60 * 60 * 1000;

interface SharedDoc {
  dismissed?: Record<string, { by: string; at: string }>;
}

/** The persisted scan (sx.storage — per plugin, per profile).
 * hadProvider records whether the scan ran with an AI provider
 * configured — NOT whether one is configured now. The no-provider
 * prompt is always derived from a live check, never from the cache:
 * a cached flag goes stale the moment the user changes Settings. */
interface CacheDoc {
  v: 3;
  at: number;
  docs: SkillDoc[];
  clusters: Cluster[];
  aiNote: string;
  hadProvider: boolean;
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
  private scannedAt = 0;
  private dismissed: Record<string, { by: string; at: string }> = {};
  private verdicts = new Map<string, Verdict | string>();
  private resolved = new Map<string, string>(); // signature -> outcome line
  private busy = new Map<string, string>();
  private compareOpen = new Set<string>();
  private scanning = false;
  private status = "";
  private aiNote = "";
  private needsProvider = false;
  private providerWatch: number | null = null;
  private rerender: (() => void) | null = null;

  onload(sx: SxAPI): void {
    this.sx = sx;
    sx.registerMainView({
      id: "skill-doctor",
      title: "Duplicate detector",
      section: "tools",
      mount: (view) => void this.mount(view),
    });
    sx.registerCommand({
      id: "scan-duplicates",
      title: "Duplicate detector: scan for duplicate skills",
      run: () => sx.ui.openView("skill-doctor"),
    });
  }

  onunload(): void {
    this.stopProviderWatch();
  }

  private async mount(view: ViewMount): Promise<void> {
    let disposed = false;
    view.onDispose(() => {
      disposed = true;
      this.rerender = null;
      this.stopProviderWatch();
    });
    const root = view.el;
    root.style.cssText = "display: flex; flex-direction: column; gap: 12px;";

    // The rerender owns the WHOLE view, provider prompt and header
    // included: the prompt sits above everything (it gates the tool,
    // not one scan's results), and the header's "Last scan" note stays
    // current after a rescan.
    this.rerender = () => {
      if (disposed) return;
      const body = el(
        "div",
        "display: flex; flex-direction: column; gap: 10px;",
      );
      body.append(...this.renderBody());
      root.replaceChildren(
        ...(this.needsProvider ? [this.providerPrompt()] : []),
        this.header(),
        body,
      );
    };
    this.rerender();

    // The prompt reflects Settings as they are NOW — checked live, in
    // parallel with the cache read, never trusted from the cache.
    const [cached, provider] = await Promise.all([
      this.sx.storage.loadData<CacheDoc>().catch(() => null),
      this.sx.llm.provider().catch(() => ""),
    ]);
    this.needsProvider = !provider;
    this.watchProvider();

    // The persisted cache is the ONLY fast path: sx.storage is per
    // profile, so it can never serve another vault's clusters. Plain
    // instance state (this.docs) deliberately is NOT trusted here —
    // built-in instances survive library switches, and an in-memory
    // shortcut would replay library A's skills against library B.
    // A local-only cache is stale the moment a provider exists: rescan
    // so the semantic sweep the user just enabled actually runs.
    if (
      cached?.v === 3 &&
      Date.now() - cached.at < CACHE_TTL_MS &&
      !(provider && !cached.hadProvider)
    ) {
      if (cached.at !== this.scannedAt) {
        // Different scan than the one this instance last showed (fresh
        // boot, or a library switch behind our back): transient per-scan
        // state must not carry over — signatures can collide across
        // libraries when forked skills share names.
        this.verdicts.clear();
        this.resolved.clear();
        this.compareOpen.clear();
      }
      this.docs = cached.docs;
      this.clusters = cached.clusters;
      this.aiNote = cached.aiNote;
      this.scannedAt = cached.at;
      await this.loadDismissals();
      this.rerender?.();
      return;
    }
    await this.scan();
  }

  /** Poll the provider setting the whole time the view is mounted —
   * BOTH directions. Settings opens as a modal OVER this still-mounted
   * view, so no remount ever re-checks: without the poll the prompt
   * sits stale after the user configures a provider, and stays hidden
   * after they remove one. Configuring triggers a fresh scan (the
   * whole point was the semantic sweep); removing just surfaces the
   * prompt over the existing results. */
  private watchProvider(): void {
    if (this.providerWatch !== null) return;
    this.providerWatch = window.setInterval(() => {
      if (this.scanning) return;
      void this.sx.llm
        .provider()
        .then((provider) => {
          const missing = !provider;
          if (missing === this.needsProvider) return;
          this.needsProvider = missing;
          if (missing) this.rerender?.();
          else void this.scan();
        })
        .catch(() => {});
    }, 2000);
  }

  private stopProviderWatch(): void {
    if (this.providerWatch === null) return;
    window.clearInterval(this.providerWatch);
    this.providerWatch = null;
  }

  private async loadDismissals(): Promise<void> {
    const shared = await this.sx.sharedStorage
      .load<SharedDoc>()
      .catch(() => null);
    this.dismissed = shared?.dismissed ?? {};
  }

  private header(): HTMLElement {
    const row = el(
      "div",
      "display: flex; gap: 10px; align-items: center; flex-wrap: wrap;",
    );
    const title = el("div", "", "");
    const scannedNote = this.scannedAt
      ? ` Last scan ${new Date(this.scannedAt).toLocaleString()}.`
      : "";
    title.append(
      el("div", "font-weight: 600; font-size: 14px;", "Duplicate skills"),
      el(
        "div",
        FAINT + "font-size: 12px;",
        "Found by content similarity plus an AI sweep of the whole catalog." +
          scannedNote,
      ),
    );
    const spacer = el("div", "flex: 1;");
    const rescan = el("button", BUTTON, "Rescan");
    rescan.onclick = () => void this.scan();
    row.append(title, spacer, rescan);
    return row;
  }

  /** The no-provider state, in AI assist's words and shape: what's
   * missing, what qualifies, and a deep link into Settings → AI
   * provider — not a shrug about zero duplicates. */
  private providerPrompt(): HTMLElement {
    const row = el(
      "div",
      "display: flex; gap: 8px; align-items: center; flex-wrap: wrap;" +
        "padding: 8px 10px; border: 1px solid var(--color-line); border-radius: 10px;" +
        "background: var(--color-surface); font-size: 12px;",
    );
    const link = el(
      "a",
      "color: var(--color-accent); cursor: pointer; text-decoration: underline;",
      "Open AI settings",
    );
    link.onclick = (e) => {
      e.preventDefault();
      this.sx.ui.openSettings("ai");
    };
    row.append(
      el(
        "span",
        "color: var(--color-ink);",
        "No AI provider configured yet — pick one (an installed CLI, a local " +
          "Ollama model, or your own API key) to sweep the catalog for " +
          "semantic duplicates. Scans without one show local matches only.",
      ),
      link,
    );
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
    const open = this.clusters.filter(
      (c) => !this.dismissed[c.signature] && !this.resolved.has(c.signature),
    );
    const done = this.clusters.filter((c) => this.resolved.has(c.signature));
    const hidden =
      this.clusters.length - open.length - done.length;
    if (open.length === 0 && done.length === 0) {
      // With no provider the interesting fact isn't "nothing found" —
      // it's that the semantic sweep never ran. The prompt above IS the
      // empty state, so the count line only appears once AI has swept.
      if (!this.needsProvider) {
        out.push(
          el(
            "div",
            FAINT + "font-size: 13px; padding: 8px;",
            `No duplicates found across ${this.docs.length} skills.` +
              (hidden > 0 ? ` ${hidden} dismissed cluster(s) hidden.` : ""),
          ),
        );
      }
      return out;
    }
    if (done.length > 0) {
      out.push(
        el(
          "div",
          FAINT + "font-size: 12px;",
          `${done.length} of ${done.length + open.length} resolved`,
        ),
      );
      for (const c of done) {
        out.push(buildResolvedCard(this.resolved.get(c.signature) ?? "Resolved"));
      }
    }
    for (const c of open) out.push(this.clusterCard(c));
    if (hidden > 0) {
      out.push(
        el(
          "div",
          FAINT + "font-size: 12px; text-align: center;",
          `${hidden} cluster(s) marked not-duplicates — hidden for the whole team.`,
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
    if (this.compareOpen.has(c.signature)) {
      card.appendChild(
        buildComparePanel(c, this.docs, recommendSurvivor(c, this.docs), {
          onConsolidate: (survivor) => void this.consolidate(c, survivor),
          onAskAI: () => void this.adjudicate(c),
          onMergeAI: () => void this.mergeCluster(c),
          openAsset: (name) => this.sx.ui.openAsset(name),
          installSummary: async (name) => {
            const v = await this.sx.assets.installations(name);
            return v.everyone
              ? "installed for everyone"
              : `${v.installations.length} install row(s)`;
          },
        }),
      );
    }

    // One primary action, tiered by confidence: exact duplicates are a
    // one-click keep-newest (their content is identical — comparing is
    // busywork); everything else opens the compare panel first.
    const busyLabel = this.busy.get(c.signature);
    const actions = el("div", "display: flex; gap: 8px; flex-wrap: wrap; align-items: center;");
    let primary: HTMLElement;
    if (c.exact) {
      const survivor = recommendSurvivor(c, this.docs);
      primary = el("button", PRIMARY, `Merge — keep newest (${survivor})…`);
      primary.onclick = () => void this.consolidate(c, survivor);
    } else {
      const open = this.compareOpen.has(c.signature);
      primary = el("button", PRIMARY, open ? "Hide comparison" : "Compare…");
      primary.onclick = () => {
        if (open) this.compareOpen.delete(c.signature);
        else this.compareOpen.add(c.signature);
        this.rerender?.();
      };
    }
    const notDup = el("button", BUTTON, "Not duplicates");
    notDup.onclick = () => void this.dismiss(c);
    for (const b of [primary, notDup]) {
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

  // ---- Actions ----

  private async scan(): Promise<void> {
    if (this.scanning) return;
    this.scanning = true;
    this.verdicts.clear();
    this.resolved.clear();
    this.compareOpen.clear();
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
                updatedAt: a.updatedAt,
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
      this.needsProvider = !provider;
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
      }

      this.scannedAt = Date.now();
      // Persist best-effort: a cache too big for the storage cap just
      // means the next mount rescans.
      void this.sx.storage
        .saveData<CacheDoc>({
          v: 3,
          at: this.scannedAt,
          docs: this.docs,
          clusters: this.clusters,
          aiNote: this.aiNote,
          hadProvider: provider !== "",
        })
        .catch(() => {});
      await this.loadDismissals();
    } finally {
      this.scanning = false;
      this.rerender?.();
    }
  }

  /** Consolidate: THE destructive action. Reach is unioned onto the
   * survivor and the rest are retired — recoverable from version
   * history, but gone from the library, so the confirm names every
   * asset and states the consequence. */
  private async consolidate(c: Cluster, survivor: string): Promise<void> {
    const losers = c.members.filter((m) => m !== survivor);
    const ok = await this.sx.ui.confirm(
      `Merge ${c.members.length} skills into “${survivor}”? ` +
        `${losers.map((l) => `“${l}”`).join(" and ")} will be removed from the ` +
        "library for everyone — their installations move onto the kept skill, " +
        "and they stay recoverable from version history.",
      `Merge into ${survivor}`,
    );
    if (!ok) return;
    this.busy.set(c.signature, "Consolidating…");
    this.rerender?.();
    try {
      const result = await this.sx.assets.consolidate({
        into: survivor,
        from: losers,
      });
      let outcome = `Merged into “${survivor}”`;
      if (result.retired.length > 0) {
        outcome += ` — retired ${result.retired.join(", ")}`;
      }
      if (result.movedInstallations > 0) {
        outcome += `, moved ${result.movedInstallations} installation(s)`;
      }
      if (result.kept.length > 0) {
        // Loud, not a parenthetical: reach that couldn't move means the
        // source is still in the library on purpose.
        outcome += `. KEPT ${result.kept.join(", ")} — their reach couldn't be moved: ${result.skipped.join("; ")}`;
      } else if (result.skipped.length > 0) {
        outcome += ` (${result.skipped.length} install move(s) refused: ${result.skipped.join("; ")})`;
      }
      this.resolved.set(c.signature, outcome);
      this.compareOpen.delete(c.signature);
      this.busy.delete(c.signature);
      this.sx.ui.notice(outcome);
      // The library changed: invalidate the PERSISTED cache too, or
      // the next mount would reload the pre-consolidation snapshot.
      this.scannedAt = 0;
      void this.sx.storage
        .saveData<CacheDoc>({
          v: 3,
          at: 0,
          docs: [],
          clusters: [],
          aiNote: "",
          hadProvider: false,
        })
        .catch(() => {});
      this.rerender?.();
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
        `Draft “${name}” created from ${members.length} variants — review and publish it, then merge the originals onto it.`,
      );
    } catch (e) {
      this.verdicts.set(c.signature, String((e as Error)?.message || e));
    }
    this.busy.delete(c.signature);
    this.rerender?.();
  }

  /** "Not duplicates": instant, positive, team-shared — the pair never
   * resurfaces for anyone unless the cluster's membership changes. */
  private async dismiss(c: Cluster): Promise<void> {
    const who = (await this.sx.app.currentUser().catch(() => "")) || "someone";
    const shared =
      (await this.sx.sharedStorage.load<SharedDoc>().catch(() => null)) ?? {};
    shared.dismissed = shared.dismissed ?? {};
    shared.dismissed[c.signature] = { by: who, at: new Date().toISOString() };
    await this.sx.sharedStorage.save(shared);
    this.dismissed = shared.dismissed;
    this.resolved.set(c.signature, `Marked not-duplicates (${c.members.join(", ")})`);
    this.sx.ui.notice("Marked as not duplicates for the whole library");
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
