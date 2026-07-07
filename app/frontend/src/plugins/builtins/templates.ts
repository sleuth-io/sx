// Templates — built-in extension. Scaffolds for new skills via palette
// commands, plus quick-capture from the clipboard. Everything lands as a
// draft; publishing stays a human action in the core UI.

import type { PluginManifest, SxAPI, SxPlugin } from "../api";

export const templatesManifest: PluginManifest = {
  id: "templates",
  name: "Templates",
  version: "1.0.0",
  description:
    "Start new skills from proven scaffolds, or capture your clipboard straight into a draft.",
  author: "sx",
  permissions: ["commands", "drafts:write"],
};

interface Template {
  id: string;
  title: string;
  name: string;
  body: string;
}

const TEMPLATES: Template[] = [
  {
    id: "template-task-skill",
    title: "Template: task skill (when-to-use + steps)",
    name: "new-task-skill",
    body: `---
name: new-task-skill
description: Use when <the task this covers>. Rename me and say when an AI tool should reach for this.
---

# What this does

One paragraph of context the AI needs before acting.

## Steps

1. First step
2. Second step

## Pitfalls

- The mistake this skill exists to prevent.
`,
  },
  {
    id: "template-conventions",
    title: "Template: team conventions",
    name: "new-conventions",
    body: `---
name: new-conventions
description: Use when writing or reviewing <area> so output follows the team's conventions.
---

# Conventions

## Always

- ...

## Never

- ...

## Examples

Good:

Bad:
`,
  },
  {
    id: "template-runbook",
    title: "Template: runbook",
    name: "new-runbook",
    body: `---
name: new-runbook
description: Use when operating <system> — deploys, checks, and rollback.
---

# Runbook

## Preconditions

## Procedure

1. ...

## Rollback

## Verification
`,
  },
];

export default class Templates implements SxPlugin {
  onload(sx: SxAPI): void {
    for (const t of TEMPLATES) {
      sx.registerCommand({
        id: t.id,
        title: t.title,
        run: async () => {
          const draft = await sx.drafts.create({
            name: t.name,
            files: [{ path: "SKILL.md", content: t.body }],
          });
          sx.ui.notice(`Draft ${draft.id} created — it's under Drafts`);
        },
      });
    }
    sx.registerCommand({
      id: "quick-capture",
      title: "Quick capture: clipboard → draft",
      run: async () => {
        let text = "";
        try {
          text = await navigator.clipboard.readText();
        } catch {
          sx.ui.notice("Couldn't read the clipboard in this environment");
          return;
        }
        if (!text.trim()) {
          sx.ui.notice("Clipboard is empty");
          return;
        }
        const body = `---\nname: captured-prompt\ndescription: \n---\n\n${text}\n`;
        const draft = await sx.drafts.create({
          name: "captured-prompt",
          files: [{ path: "SKILL.md", content: body }],
        });
        sx.ui.notice(`Captured to draft ${draft.id} — add a description before publishing`);
      },
    });
  }

  onunload(): void {
    // Commands are torn down by the host.
  }
}
