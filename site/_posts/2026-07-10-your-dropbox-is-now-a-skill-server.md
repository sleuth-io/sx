---
layout: post
title: "Your Dropbox is now a skill server"
date: 2026-07-10
author: Dylan Etkin
description: >-
  sx 2.0 is out: a native app for Mac, Windows, and Linux that lets anyone
  share AI skills with their team, no git and no terminal required.
image: /sx/assets/skill-server-hero.png
---

![sx 2.0]({{ '/assets/skill-server-hero.png' | relative_url }})

*sx 2.0 is out: a native app for Mac, Windows, and Linux that lets anyone share AI skills with their team, no git and no terminal required.*

A few months ago I wrote that there's an [npm-shaped hole in the AI tooling stack]({% post_url 2026-06-05-a-package-manager-for-ai-assets %}). Your best AI users build up skills, MCP configs, and commands that multiply their output, and that knowledge stays trapped on their machines because there's no clean way to distribute it. We built sx, an open source package manager for AI assets, to fill that hole. It worked. Developers use it to version skills in git vaults and install them across Claude Code, Cursor, Copilot, Codex, Gemini, Cline, and Kiro with a lock file and deterministic installs.

Then I made a mistake I should have seen coming, because I've watched Atlassian make it twice. I built the sharing layer for developers and assumed everyone else would eventually meet us at the command line. They won't. In the sixty or so discovery interviews we've run this year, the people getting the most out of skills are increasingly in marketing, legal, sales, and ops. They write great skills. They have no git, no terminal, and no interest in acquiring either. Asking a marketing team to `sx init --type git` is asking them to not share skills.

sx 2.0 is the fix. It's a real desktop app, and the distribution model it leans on is one every team already has: a shared folder.

## A shared Dropbox folder is the whole backend

Here's the workflow for a non-technical team. You open the app, point your library at a folder in Dropbox, Google Drive, OneDrive, or iCloud, and drag your skills in. Markdown goes in, skills come out. Your teammates point the app at the same folder and everything you publish shows up for them. There's no server and no accounts. The file sync product your company already pays for does the replication.

This works because of the other big change in 2.0: vault format v2. The latest version of every asset now lives directly on disk at `assets/<name>/` as plain, readable markdown. Version history lives in `.sx/versions/` next to it. You can grep the vault. You can open it in Obsidian. You can point `.claude/skills` straight at it and it just works, because there's nothing to unpack.

The obvious comparison is exactly that Obsidian setup, a markdown vault in a synced folder, and plenty of teams do run their skills that way today. The difference is what happens after the files sync. sx knows about the AI clients natively. When you hit Sync in the app, it runs an `sx install` in the background: it resolves what should be installed for you, translates each asset into every client's format (Claude Code skills, Cursor rules, Copilot instructions, and so on), and writes it to the right place on your machine. Your teammate drops a skill in the shared folder, you click one button, and it's live in your AI client. That translation layer is the part a folder full of markdown can't do for you.

Developers lose nothing here. The CLI is still the same Go binary, the git and skills.new vault types still work, and the app and CLI read the same vaults. 2.0 adds collections, which group related skills and install as a unit resolved at read time, so a skill added to a shared collection next month reaches the whole team automatically without anyone re-running anything.

## Extensions, because your team's problems aren't my roadmap

The second half of this release line is an extension system for the app. I've spent enough years around plugin ecosystems (I was at Atlassian when my co-founder Don built theirs) to believe that the interesting problems in a team tool are always specific to the team, and that cramming every team's answer into core is how products get bloated and slow.

So the app is now pluggable. An extension is a folder with a manifest and one ES module, no build step until you want one. Extensions can add dashboard widgets, publish-time checks, editor commands, and whole new views. There's a marketplace that ships with fifteen of them, and the ones people reach for first are the team-health ones:

* **Collection Doctor** scores a collection 0 to 100 and names the problems: thin descriptions, stale assets that should be retired, near-duplicate skills, oversized skills eating context. Each finding is one click from the asset it names.
* The **adoption widgets** show who on the team is actually using which skills, so you can see whether that skill someone spent a week on is earning its keep.
* **Review Rota** gives every asset a review due date that adapts to how heavily it's used, and rotates reviews fairly across the team.
* **Collection Export** compiles a collection into a Claude Code, Codex, or Gemini plugin, or a plain zip, for sharing outside your vault.

Two design decisions I'll defend if you push on them in the comments. First, extensions are permission-gated: no filesystem, no Node, no network beyond hosts an extension explicitly declares, and enabling one shows you a plain-language list of exactly what it can touch, re-prompted whenever an update changes that list. Second, extensions are just sx assets. They publish, version, scope to teams, pin, and audit through the same pipeline as a skill, which means an extension update can never sneak past review the way it can in ecosystems where plugins auto-update out-of-band. Org admins can allowlist or disable third-party extensions vault-wide. The marketplace itself is just another sx vault, so pointing the app at your own repo gives you a private one.

## Where this is going

The premise behind sx hasn't changed since the first release: AI gains are trapped in individuals, and the missing layer is distribution. What changed is my picture of who needs it. The 2.0 line is a bet that the next wave of skill authors won't be developers, and that meeting them means a native app, a folder they already share, and one sync button that hides a package manager underneath.

Everything is Apache-2.0 and on GitHub: [github.com/sleuth-io/sx](https://github.com/sleuth-io/sx). The app is in the [release assets](https://github.com/sleuth-io/sx/releases) for all three platforms, and `brew install sx` still gets you the CLI. If you try the shared-folder setup with your team, I want to hear where it breaks. That's the part of this release I'm least sure survives contact with real Dropbox latency.
