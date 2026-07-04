# Exposing sx libraries as AI-tool plugins

Status: Layers 1 and 2 approved for implementation. Layer 3 is spec-only,
awaiting a go decision.

## Why

By mid-2026 the major coding agents converged on two distribution
primitives sx already has:

- **Agent Skills** (SKILL.md folders) — adopted by Claude Code, OpenAI
  Codex, Gemini CLI, GitHub Copilot, and Cursor. sx skill assets are
  already in this format.
- **Git-repo plugin marketplaces** — Claude Code (`.claude-plugin/
  marketplace.json`) and Codex (`.agents/plugins/marketplace.json`) both
  treat a git repo as an installable catalog of plugins, with private-repo
  access through normal git auth. An sx git vault is architecturally the
  same thing: a git repo of assets plus a manifest.

The v2 storage format (materialized latest at `assets/{name}/`) means the
vault's asset directories are directly consumable by these systems — no
repackaging needed, only generated manifests.

## Asset-type routing

What each tool's plugin/extension system can carry:

| sx type | Claude Code plugin | Codex plugin | Gemini extension |
|---------|--------------------|--------------|------------------|
| skill   | yes                | yes          | yes              |
| mcp     | yes (`.mcp.json`)  | yes (`.mcp.json`) | yes (manifest `mcpServers`) |
| hook    | yes (`hooks/hooks.json`) | yes    | yes              |
| agent   | yes (`agents/*.md`) | no          | yes (`agents/`)  |
| command | yes (`commands/*.md`) | as a skill | TOML conversion or as a skill |
| rule    | **no** (plugins never load a CLAUDE.md) | **no** | yes (`contextFileName`) |

Rules therefore stay on the copy-install path for Claude and Codex
permanently (a deliberate platform trust boundary, not a gap we can
engineer around). The generator keeps a per-type-per-tool routing table so
a platform adding context support later is a one-entry change.

## Layer 1 — vendor-neutral skills directory (implemented)

Codex's current documented skill discovery is `.agents/skills` (repo) and
`~/.agents/skills` (user); `~/.codex/skills` is the legacy location.
sx already installs repo/path-scoped Codex skills to `.agents/`, but
global-scoped skills still go to `~/.codex/skills`, which current Codex
releases no longer list in discovery.

Change: the Codex client routes global-scope skills to
`~/.agents/skills`. On install it also removes a leftover copy of the
same skill at the legacy `~/.codex/skills/<name>` path so tools that do
read both never see duplicates. List/uninstall go through the same path
computation, so tracker behavior follows automatically.

`~/.agents/skills` is additionally read by Gemini CLI and GitHub Copilot,
so this one change widens coverage beyond Codex.

Deliberately out of scope for now:

- Gemini native skills (today sx converts skills to `.gemini/commands/*.toml`;
  switching to native `skills/` needs behavior verification against both
  Gemini CLI and the VS Code extension, which share config).
- Copilot's `.github/skills` handling (already correct).

## Layer 2 — git/path vault as a plugin marketplace (implemented)

Every time the manifest is saved (`manifest.Save`, the single write choke
point for git and path vaults), sx regenerates three derived files at the
vault root. They are deterministic functions of `sx.toml` and always
overwritten, never hand-edited; old sx versions ignore them entirely, so
this is backward compatible the same way the v2 storage migration was —
additive files, no schema change.

Generated files:

1. `.claude-plugin/marketplace.json` — the vault as a Claude Code
   marketplace:
   - one whole-library plugin (`skills: ["./assets"]` — the scanner picks
     up every asset dir containing a SKILL.md and ignores the rest), and
   - one plugin per collection that contains at least one skill asset
     (`strict: false` entries with `skills` listing each skill asset dir
     directly).
   Both entry shapes validated against `claude plugin validate` and a real
   sandboxed `claude plugin install` (v2.1.172).
2. `.codex-plugin/plugin.json` — whole-library Codex plugin
   (`skills: "./assets"`). Codex marketplace entries don't support
   component overrides, so collections-as-plugins is Claude-only for now.
3. `.agents/plugins/marketplace.json` — Codex marketplace listing the
   single library plugin with a `local` source.

Naming: the marketplace/plugin slug comes from the git `origin` remote URL
basename when the vault root is a clone, else the directory basename,
slugified. Versioning is intentionally omitted from Claude entries — every
commit is then a new version, which matches vault semantics exactly.

User story: `/plugin marketplace add your-org/your-vault` in Claude Code
(or `codex plugin marketplace add …`) with zero sx involvement. Private
vaults work through the consumer's git credentials.

Known caveats (accepted):

- No team scoping — repo access = whole library. True of raw git vaults
  generally; scoped delivery is Layer 3's job.
- Claude copies the plugin source to its cache per installed plugin, which
  includes `.sx/versions/` history. Large vaults pay a disk cost;
  mitigation (thin generated plugin roots) can come later if it bites.
- A vault written only by pre-Layer-2 sx clients after a newer client
  generated the manifests will have stale manifests until the next
  new-client write (same mixed-version caveat as the storage migration).
- Sleuth vaults are server-backed; a marketplace endpoint there is a
  server feature, out of scope here.

## Layer 3 — materialized local plugin (spec only, not approved yet)

Goal: `sx install`/`sx sync` stops sprinkling per-tool file copies for
plugin-capable clients and instead compiles the user's **scope-resolved**
asset set into one plugin directory per library, then registers it with
each tool once:

```
~/.sx/plugins/<library>/
├── .claude-plugin/plugin.json     ← Claude (loaded in-place as a
│                                     skills-directory plugin under
│                                     ~/.claude/skills/, no cache dance)
├── .codex-plugin/plugin.json      ← Codex (local marketplace add)
├── gemini-extension.json          ← Gemini (gemini extensions link)
├── skills/…  agents/…  hooks/hooks.json  .mcp.json
```

Key properties:

- Resolution, scoping, tracker, and stats are unchanged — only the write
  target and registration change. sx remains the thing that talks to the
  vault (including Sleuth vaults with real ACLs); the AI tools only ever
  see the local materialization.
- Per-client feature detection; non-plugin clients (Cursor, Windsurf,
  Cline, Kiro, …) keep today's copy path byte-for-byte. Rules keep the
  copy path on Claude/Codex (see routing table).
- Migration mirrors the storage-format playbook: on first sync after
  upgrade, per client — remove tracker-known copied files, materialize,
  register, update tracker. One atomic swap, silent except for one
  message: slash invocation becomes plugin-namespaced
  (`/brand-voice` → `/<library>:brand-voice`). Model-invoked skills are
  unaffected. Downgrade is a documented one-way door (old sx re-copies
  files and doesn't know about the plugin; duplicates until re-upgrade).
- Overlap with Layer 2: sx knows the vault's repo URL, so install detects
  an existing direct marketplace registration of the same vault and
  offers to take over management (or skips its own registration).

Open questions to settle before implementation:

1. Registration mechanics per tool (Claude skills-dir plugin vs local
   marketplace + settings write; Codex `plugin marketplace add` vs
   config.toml edit; Gemini `extensions link`) and how they behave in
   CI/headless.
2. Whether the desktop app's "Use in my AI tools" installs the whole
   library plugin (likely yes) and what per-asset uninstall means then
   (plugin carries the library; per-asset disable?).
3. Version stamping cadence for the materialized plugin manifest.

## Verification

- Layer 1: unit tests on the Codex path computation + legacy cleanup;
  docker-interactive-testing run of `sx install` with a fake ~/.codex.
- Layer 2: unit tests for the generator (fixtures → JSON), an e2e test
  asserting a vault write produces valid manifests, and a live
  `claude plugin validate` + sandboxed-HOME `claude plugin marketplace
  add` + `install` against a generated vault.
- Layer 3: not yet.
