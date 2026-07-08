// Publish Doctor — built-in extension. Pre-publish checks that surface
// as warnings in the publish sheet; never blocks publishing. Runs through
// the host/API/permission path like any vault-installed extension would
// (docs/app-plugins-spec.md: built-ins are the API's conformance suite).

import type {
  BeforePublishContext,
  PluginManifest,
  PublishWarning,
  SxAPI,
  SxPlugin,
} from "../api";

export const publishDoctorManifest: PluginManifest = {
  id: "publish-doctor",
  name: "Publish Doctor",
  version: "1.0.0",
  description:
    "Checks drafts before publishing: frontmatter, description quality, and broken file references.",
  author: "sx",
  permissions: ["events", "assets:read"],
};

const MIN_DESCRIPTION = 40;
const MAX_DESCRIPTION = 1024;

export function doctorChecks(ctx: BeforePublishContext): PublishWarning[] {
  const warnings: PublishWarning[] = [];
  const prompt = ctx.files.find((f) => /(^|\/)(SKILL|RULE|AGENT|COMMAND)\.md$/i.test(f.path) || f.path.endsWith(".md"));

  // Frontmatter validity: the prompt file should open with a --- block
  // containing name and description.
  if (prompt) {
    const fm = /^---\n([\s\S]*?)\n---/.exec(prompt.content);
    if (!fm) {
      warnings.push({
        message: "No frontmatter block in " + prompt.path,
        detail: "AI tools use the --- name/description block to decide when to load this.",
      });
    } else {
      if (!/^name:\s*\S/m.test(fm[1])) {
        warnings.push({ message: "Frontmatter is missing a name" });
      }
      if (!/^description:\s*\S/m.test(fm[1])) {
        warnings.push({ message: "Frontmatter is missing a description" });
      }
    }
  }

  // Description quality: too short and tools won't know when to load it.
  const desc = ctx.description.trim();
  if (desc.length === 0) {
    warnings.push({
      message: "The description is empty",
      detail: "One sentence saying when to use this — it's what AI tools (and teammates) match against.",
    });
  } else if (desc.length < MIN_DESCRIPTION) {
    warnings.push({
      message: "The description is very short",
      detail: `Aim for a sentence describing when to use this (${desc.length}/${MIN_DESCRIPTION}+ characters).`,
    });
  } else if (desc.length > MAX_DESCRIPTION) {
    warnings.push({
      message: "The description is very long",
      detail: "Long descriptions dilute matching; move detail into the body.",
    });
  }

  // Trigger phrasing: "use when …" style descriptions measurably improve
  // model-invoked loading.
  if (desc && !/\b(use|when|for|helps?|writes?|checks?|reviews?)\b/i.test(desc)) {
    warnings.push({
      message: "The description doesn't say when to use this",
      detail: 'Consider phrasing like "Use when …" so tools can match tasks to it.',
    });
  }

  // Broken relative file references inside markdown files.
  const paths = new Set(ctx.files.map((f) => f.path));
  for (const f of ctx.files) {
    if (!f.path.endsWith(".md")) continue;
    for (const m of f.content.matchAll(/\]\((\.\/?[^)#\s]+)\)/g)) {
      const target = m[1].replace(/^\.\//, "");
      if (!paths.has(target)) {
        warnings.push({
          message: `${f.path} links to a missing file: ${target}`,
        });
      }
    }
  }

  return warnings;
}

export default class PublishDoctor implements SxPlugin {
  onload(sx: SxAPI): void {
    sx.onBeforePublish((ctx) => doctorChecks(ctx));
  }
  onunload(): void {
    // Subscriptions are torn down by the host.
  }
}
