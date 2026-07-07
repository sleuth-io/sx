// Importer — built-in extension. Batch-imports existing prompts into
// drafts: a .claude/skills directory, an Obsidian folder, or any folder
// of loose markdown. One palette command, native folder picker, drafts
// out — the human reviews and publishes.

import type { PluginManifest, SxAPI, SxPlugin } from "../api";

export const importerManifest: PluginManifest = {
  id: "importer",
  name: "Importer",
  version: "1.0.0",
  description:
    "Bring existing prompts into your library: import a .claude folder, an Obsidian folder, or loose markdown as drafts.",
  author: "sx",
  permissions: ["commands", "drafts:write"],
};

export default class Importer implements SxPlugin {
  onload(sx: SxAPI): void {
    sx.registerCommand({
      id: "import-folder",
      title: "Import skills from a folder…",
      run: async () => {
        try {
          const res = await sx.drafts.importFromFolder();
          if (res.created.length === 0 && res.skipped === 0) {
            return; // picker cancelled
          }
          const skipped =
            res.skipped > 0 ? ` (${res.skipped} entries skipped)` : "";
          sx.ui.notice(
            res.created.length === 0
              ? `Nothing importable found${skipped}`
              : `Created ${res.created.length} draft${res.created.length === 1 ? "" : "s"}${skipped} — review them under Drafts`,
          );
        } catch (e) {
          sx.ui.notice(String(e));
        }
      },
    });
  }

  onunload(): void {
    // Commands are torn down by the host.
  }
}
