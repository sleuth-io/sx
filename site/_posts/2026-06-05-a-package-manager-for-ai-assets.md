---
layout: post
title: "A package manager for AI assets (and why the lock file is per-user)"
date: 2026-06-05
author: Dylan Etkin
description: >-
  How sx borrows the npm/Cargo manifest-and-lock shape for AI assets — and the
  two places that mental model breaks: a per-user lock file resolved against
  your git identity, and normalizing one asset into a dozen clients' formats.
image: /sx/assets/sx-hero.png
---

![sx]({{ '/assets/sx-hero.png' | relative_url }})

Sometime in the last two years your repos quietly filled up with a new category of file. Not code, not config exactly: prompts. A `.claude/skills/` directory here. A `.cursor/rules/` folder there. A `CLAUDE.md` at the root, an `AGENTS.md` next to it, a `.mcp.json` listing the servers your agent is allowed to call. These are the things that make a coding agent useful on *your* codebase, and they're sprawling.

The moment one of them is good enough that a teammate wants it, you copy-paste. Now there are two copies. Someone fixes a bug in one, the copies drift, and three months later nobody can tell which version is canonical or which repos even have it. Git is versioning each copy, but only inside its own repo. Nothing connects the copy in one repo to the copy in another, so there's no shared source of truth, no way to say "everyone on the platform team gets this rule but nobody else," and no way to know if anyone is actually using the thing you wrote.

A team I talked to a few weeks ago lives exactly this. They run five repos: one application and four microservices, and every fix to a shared skill has to be copied into all five. They'd been doing that for months, tweaking each copy a little as it landed, until the copies had drifted into near-duplicates that say roughly the same thing in slightly different words. One of their engineers called them versions "from different eras." Someone eventually built a Confluence page just to inventory the duplication. Four of them meet every couple of days to ask who managed to migrate which repo, and the answer is always the same: no time, a deal just closed, something has to ship today.

This isn't for lack of tooling. Claude Code has plugins and a plugin marketplace: bundle up some skills, commands, and hooks, push them to a git repo, and anyone can install the bundle. It's genuinely useful. But it's one client, and a marketplace install is all-or-nothing. It writes Claude Code's formats and no one else's, and it has no concept of "this rule for the platform team, that one for everybody." The cross-team, cross-client version of the problem is still wide open.

So it's a distribution problem, and distribution problems have a known shape: you build a package manager. We did; it's called `sx`. Most of it is the boring, well-understood machinery you'd expect: a manifest, a lock file, a resolver. That part is solved, and `sx` just borrows the shape npm and Cargo already settled on.

The standard playbook gets you most of the way and then gives a few wrong answers, because AI assets break the package-manager mental model in places code packages never do. The headline one is heretical if you grew up on `package-lock.json`: the lock file is not committed, and it is different for every developer on the team. The unglamorous one is the integration tax nobody warns you about. A dozen AI clients took one good idea and each shipped an incompatible on-disk format for it. Those two problems get most of the space here. The back half is shorter: how the assets stay installed without anyone running a command, how a vault running on your laptop can serve the web clients like claude.ai, and an honest accounting of where the design leaks.

## The boring part, briefly

`sx` borrows the manifest-and-lock split that npm, Cargo, and uv all landed on independently, because it's correct.

There's a manifest, `sx.toml`, which is the human-authored source of truth. It lists every managed asset, where its bytes live (an HTTP URL, a git ref, a local path), and *who should get it*. It's committed to the vault. Here's a trimmed asset entry:

```toml
[[assets]]
name = "python-docstrings"
version = "1.2.0"
type = "rule"


[assets.source-http]
url = "https://vault.example.com/assets/python-docstrings/1.2.0/bundle.zip"
hashes = { sha256 = "e3b0c442…" }


[[assets.scopes]]
kind = "team"
team = "platform"
```

And there's a lock file, the fully-resolved artifact you actually install from, pinned by content hash. So far, nothing unusual. The interesting word in that manifest is `scopes`, and it's what makes the lock file stop behaving like npm's.

## The lock file is resolved against *you*

In a normal package manager the dependency graph is a property of the project. Everyone who checks out the repo resolves the same graph and gets the same `package-lock.json`. The lock file is shared precisely because the question it answers, "what are this project's dependencies?", has exactly one answer.

The question `sx` answers is different: "what should *this caller, standing in this directory* have installed?" The manifest scopes assets to an org, a repo, a path within a repo, a team, a single user, or a bot identity. Resolving that requires knowing who's asking. So resolution takes a second input that npm never needs, the caller's identity, and the lock file becomes a per-user projection of the manifest rather than a shared fact.

Identity comes from git. `sx` shells out to `git config user.email`, with a documented fallback chain: an `SX_BOT` environment variable wins first (for CI runners and headless agents), then explicit overrides, then the git config, then a synthetic `local:$USER@hostname` as a last resort. That synthetic identity is deliberately a second-class citizen. It's fine for reads, but any mutation calls `RequireRealIdentity()` and bounces it, so nobody accidentally writes audit entries as `local:nobody@laptop`. Resolving identity means a `git` subprocess, so the result is cached per `(repoPath, SX_BOT)` pair.

The resolver, `manifest.Resolve(manifest, actor)`, walks every scope on every asset and decides, for *this* actor, whether it applies and what it collapses to. The team case is the one worth looking at:

- A `kind = "org"` scope is global. Everyone gets it; it resolves to a lock entry with no scope restriction.
- A `kind = "user"` scope resolves to global *if the email matches the caller*, and is dropped entirely otherwise.
- A `kind = "team"` scope is the interesting one. Teams own repositories. So a team scope doesn't resolve to "team" (there's no such thing in a lock file). It expands into one repo-scoped entry *per repository the team owns*, but only if the caller is a member of that team.

That last point is the design decision. Team rosters change constantly; repositories a team owns change less; the set of assets a team should have changes least of all. By storing `kind = team` in the manifest and expanding it to concrete repos only at resolution time, a roster change never requires rewriting a single asset entry. The membership lives in one place (the team definition) and fans out at the last possible moment.

There's a merge pass after expansion, because scopes overlap. If a caller ends up with both a whole-repo scope and a `kind = "path"` scope for the same repo (the path scope carrying something like `paths = ["docs/"]`), the repo-wide entry wins and the narrower one is dropped. A repo-wide install already covers every path under it, so keeping both would leave a redundant entry for someone to puzzle over later. Paths are sorted lexicographically so the output is deterministic.

Then the lock file is written to the user's cache directory, keyed by a hash of the vault URL, and here's the last touch I liked: **rotation is keyed on content, not time.** When `sx` writes a new lock file it hashes the bytes, and if they match what's already there it just touches the mtime. If they differ, the old file is renamed with an ISO-8601 timestamp (colons swapped for hyphens, thanks Windows) before the new one lands. So `lockfiles/<vault>.lock` is always current, and `lockfiles/<vault>-2026-01-14T09-31-07Z.lock` is the exact thing you had installed last Tuesday, byte for byte. A stale CI box that hasn't re-resolved in a month still has a reproducible record of what it's running. Rewriting the manifest with no semantic change produces no rotation noise, because the content hash is unchanged.

You do give up something npm takes for granted. You can't `git diff` two developers' installs to explain why they differ, because the lock was never in git to begin with. It isn't a shared artifact, so there's nothing to compare across people. What you get back is reproducibility along the other axis: the rotated history is the diff, scoped to one person over time. For this domain that's the right trade. "Why does Alice have a rule I don't?" is answered by her team membership in the manifest, not by diffing two lock files that were never meant to match.

## Every client invented its own file format

Here's the problem that requires the most code to solve and, as is true with most things in life, xkcd called it.

A "rule" is a simple idea: some instructions plus a set of file globs they apply to. Every AI client agreed on the idea and then implemented it differently.

- **Claude Code** wants a Markdown file with YAML frontmatter using a `paths:` key.
- **Cursor** wants a `.mdc` file with frontmatter using a `globs:` key, plus an `alwaysApply: true` flag when there are no globs. The glob value is a bare string when there's one glob but a YAML list when there are several.
- **Gemini** doesn't do per-rule files at all. Everything goes into one `GEMINI.md` per scope.

So the canonical asset is stored once, format-neutral, and each client owns a *handler* that renders it into that client's dialect on install. The architecture is deliberately dumb in the right way: there's no central router that knows how to translate asset type A for client B. Each client is a sealed box implementing one interface:

```go
type Client interface {
    ID() string
    SupportsAssetType(t asset.Type) bool
    InstallAssets(ctx context.Context, req InstallRequest) (InstallResponse, error)
    // …
}
```

Each one registers itself at import time with a plain `func init()`. The orchestrator fans out across every installed client concurrently, hands each one the subset of assets it declared support for, and lets it do whatever it wants to the filesystem. Adding a client is adding a package, not editing a switch statement.

The capability declaration is where the README's support matrix actually lives. Claude Code constructs itself with `asset.AllTypes()`; Gemini lists exactly the five types it can represent and silently never receives an agent or a plugin. The matrix isn't documentation that can drift from the code. It *is* the code.

Two details from this layer are worth calling out.

**The Gemini marker trick.** Since Gemini packs every rule into one shared `GEMINI.md`, `sx` needs to update or remove a single rule later without clobbering a file the user might also be hand-editing. It wraps each managed section in HTML comment sentinels:

```markdown
<!-- sx:python-docstrings -->
## Python docstrings
Enforce docstrings on all public functions.
<!-- /sx:python-docstrings -->
```

Update is a find-and-replace between the markers; uninstall is a delete between them; anything the user wrote outside the markers is never touched. It's the same technique your dotfile manager uses for "managed block" sections of `.bashrc`, applied to agent instructions.

**Unsupported is not the same as failed.** Hook events don't map cleanly across clients. Claude Code distinguishes `post-tool-use` from `post-tool-use-failure`; Gemini collapses both into one `AfterTool` event and has no concept at all of `pre-compact`. When a handler hits an event the client can't express, it returns a sentinel `ErrUnsupportedEvent`, and the install pipeline translates that specific error into a `StatusSkipped` rather than a `StatusFailed`:

```go
func TranslateInstallError(err error, successMessage string) (ResultStatus, string, error) {
    if err == nil {
        return StatusSuccess, successMessage, nil
    }
    if errors.Is(err, hook.ErrUnsupportedEvent) {
        return StatusSkipped, err.Error(), err
    }
    return StatusFailed, fmt.Sprintf("Installation failed: %v", err), err
}
```

So installing a six-event hook bundle onto Gemini reports "installed, two events skipped" instead of failing. That difference matters. A tool that reports a hard failure on every partial install gets ignored, and a tool that silently drops things gets distrusted. `Skipped` is a real status sitting next to success and failure, and a surprising amount of the per-client code exists only to tell the three apart correctly.

## Keeping it installed without anyone running a command

A package manager nobody invokes is a directory of stale files. The expectation for these assets is that they're just *there* when you open your AI harness, and that they update themselves.

`sx` does this with the same mechanism it's distributing: a hook. On first install into Claude Code it writes a `SessionStart` hook into `~/.claude/settings.json` that runs `sx install --hook-mode --client=claude-code`. Every new session re-resolves the manifest and reconciles the filesystem, so a rule published to your team this morning is present the next time anyone starts Claude, with no broadcast and no "please run `sx pull`" Slack message.

The trap with "run on every session start" is doing real work every time. The hook guards itself with a session cache (an append-only file of `session-id timestamp` lines) and a fast-path check that bails in about a millisecond if this session has already been reconciled. The dedup strategy is per-client on purpose, because the clients fire hooks on different cadences: Claude Code's `SessionStart` is genuinely once per session, but Cursor fires per prompt and Copilot per prompt-correlation, so each carries the tracking its own firing pattern requires.

The usage telemetry uses the same approach and solves a git-shaped problem along the way. A `PostToolUse` hook fires after a skill or command runs, but you can't have every tool invocation produce a commit to a git-backed vault. You'd generate thousands of commits a day and turn the history into noise. So usage is two-phase: the hook enqueues an event to a local spool directory immediately and non-blockingly, and the next vault mutation that's *already* committing for some other reason sweeps the spool into the monthly `.sx/usage/YYYY-MM.jsonl` file in the same commit. The local filesystem is a write-ahead log; the vault commit is the fsync. `sx stats` reads those append-only JSONL streams back to compute adoption and per-asset usage. Audit events (`.sx/audit/YYYY-MM.jsonl`) work the same way: dumb append-only log, all the intelligence in the reader, monthly files that sort lexicographically because the filenames are ISO dates.

## Getting a local vault into a browser tab

The last piece is the one that sounds difficult at first. You can install assets onto local clients because `sx` is a process on your machine writing to your disk. But claude.ai and chatgpt.com run in a browser. There's no local process and no filesystem to write to. They speak MCP over HTTPS to a public endpoint. How do you serve a vault that lives on a developer's laptop, behind NAT, to a web app, without uploading the vault somewhere?

You run a reverse tunnel. `sx cloud serve` opens a *persistent outbound WebSocket* to a relay hosted at skills.new, authenticated with a machine token held in the OS keyring (Keychain, Credential Manager, or freedesktop Secret Service). The browser talks MCP to the relay's public URL; the relay wraps each JSON-RPC request in an envelope and pushes it down the open socket; the local `sx` process unwraps it, runs it against an in-process MCP server backed by the local vault, and pushes the response back up the same socket. The asset bytes never leave the machine; only the MCP request and its result cross the relay. It's the same shape as ngrok, scoped to one protocol.

A couple of things make it hold up:

- The local side runs the MCP server *in memory* over the SDK's in-process transport, and multiplexes concurrent requests from the relay onto that single connection by JSON-RPC id, using a `sync.Map` of pending response channels. So a slow `load_my_asset` doesn't head-of-line-block a `list_my_assets` arriving right behind it.
- Idle WebSockets through nginx/pushpin-style proxies get reaped at around 60 seconds, so the dispatch loop sends a ping if 45 seconds pass with no traffic.
- The whole MCP server is rebuilt fresh on every reconnect, so a dropped network never leaves half-alive session state behind.

For the local path/git vaults the relay exposes a handful of asset tools (`list_my_skills`, `load_my_asset`, and friends) with a one-slot zip cache so the common "list, then load, then read three files out of the same bundle" sequence doesn't re-download the zip four times. The whole thing is maybe 500 lines.

## Where this leaks

I don't want to leave you with the impression this is airtight. It isn't, and the gaps are worth naming because some of them are fundamental.

The big one is identity. For git and path vaults, "who are you" is whatever `git config user.email` says, and git will happily tell it anything I type. So the scoping is access *organization*, not access *control*: it decides what the honest caller installs, and it keeps an audit trail, but it does not stop a determined person from setting their email to a teammate's and resolving that teammate's scopes. Real enforcement needs a backend that authenticates the caller. The hosted Sleuth vault does this with tokens; the file-backed vaults lean on whoever already has commit rights to the repo. And commit rights are a coarse grain: write access to the vault repo is write access to every skill in it, not just your team's. The same team I mentioned earlier had two engineers revert each other's pull requests overnight, back and forth, over a single shared skill, because nothing about a git repo can express "this skill belongs to that team." If you're tempted to copy this design, decide up front which of those two worlds you're in.

The audit and usage streams are append-only JSONL committed to the vault, which means a git-backed vault accumulates commits and, under real concurrency, merge conflicts on the monthly files. The spool-and-batch trick keeps it to one write per mutation instead of one per tool call, but your telemetry still lives in your git history, and that carries real costs. It's fine at team scale and would not be fine at thousands of seats, which is exactly why the hosted backend exists.

And the capability matrix cuts both ways. A hook bundle that installs cleanly on Claude Code arrives on Gemini with two events silently skipped. We report it, but the asset *author* doesn't necessarily see that report. They wrote one thing, and different people received slightly different things. Honest degradation beats a hard failure, but it is not the same as the asset working everywhere, and the README's tidy support matrix hides a lot of per-client asterisks.

None of that changes the shape of the thing. The package-manager bones (the manifest, the lock, content-hashed sources) were the easy, solved part. The real work came from the domain refusing to fit the mold: an install target that depends on who's asking and where they stand, a dozen clients that each speak a different on-disk dialect, and the requirement that the whole apparatus stay invisible or it rots. The npm-shaped instinct is right up until each of those, and then it quietly gives you the wrong answer.

The code is Go and it's open source. If you only read two directories, read the resolver and the client handlers. That's where the two hard problems live.
