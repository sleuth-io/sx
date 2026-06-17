---
layout: post
title: "All your skills should live in one place"
date: 2026-06-17
author: Mato Žgajner
description: >-
  AI skills, rules and MCP configs can end up scattered, duplicated and outdated.
  sx pulls them into a single source of truth and delivers them, in the right format,
  to whatever tool each person runs.
---

If your organization uses coding agents heavily, you've probably bumped into various annoyances around managing them at scale. We built [`sx`](https://github.com/sleuth-io/sx) to try and address that.

## Three things we wanted to fix

1. **Skills live in multiple locations and often aren't synced.**
   Repo-specific skills are part of the repo, which is cool since it means they're implicitly up to date. Global skills are just loose files sitting in your home folder though. They don't get shared, updated or backed up.

2. **Multiple repos cause skill duplication.**
   Say your team has a neat set of test writing conventions and you try to enforce them with a skill. Unless you work on a single product, that skill will end up in more than one repo. This means you now have multiple instances you need to manually keep track of.

3. **People use different coding agents.**
   Many organizations let their devs pick AI tools based on individual preference. This means you're forced to[^x] add `.claude`, `.cursor` and `.codex` to support everyone, multiplying the mess I already mentioned.

## What sx does about it

You put every skill, tool, rule and any other AI asset in a single repository. There's only one copy of each, along with configuration that defines where it should be applied (globally, per-repo, per-team or per-user).

The `sx` client on each person's machine takes care of installing the right skills in the right places, and in the right format for whatever AI tool they happen to run. Whenever a skill changes, it auto-updates in place.

## Setting it up

First, install `sx` via Homebrew …

```bash
brew tap sleuth-io/tap
brew install sx
```

… or via a shell script:

```bash
curl -fsSL https://raw.githubusercontent.com/sleuth-io/sx/main/install.sh | bash
```

Then, scaffold that central repo I mentioned (we call it a git vault):

```bash
sx init --type git --repo-url git@github.com:yourorg/skills.git
```

If you'd rather just see what one looks like, [`sleuth-io/skills-repository`](https://github.com/sleuth-io/skills-repository) is a real, working example.

Next, move your skills to that repo with the client:

```bash
# add a skill from a local folder and scope it to specific repos
sx add ./postgres-optimizations --repo git@github.com:yourorg/repo-one.git \
                                --repo git@github.com:yourorg/repo-two.git
```

Scopes define where a skill should be used: `--org` makes a skill available to everyone, `--team` activates it for a team of people, `--repo` and `--path` enable it in specific repos (or subpaths, if you've got monorepos), `--user` targets one person. That config lives in the git repo along with the skills, so you can easily modify everything right on the filesystem if you prefer that to the CLI tool.

Then everyone on the team installs the client and points it at the vault. From their side it's basically:

```bash
sx init                # connect to the vault
sx install             # pull down whatever's scoped to them
```

The client wires itself into each AI tool's session startup with hooks, so from then on it quietly re-syncs on its own - pulling new skills, updating changed ones, and writing them into the correct `.claude` / `.cursor` / `.codex` / whatever folder for each person's tool.

### One gotcha: gitignore those folders

Because the client is writing into `.claude/`, `.cursor/`, `.codex/` and the rest inside your repos, you'll need to delete all those folders and add them to `.gitignore` (basically treat them as you would a build output folder).

```gitignore
.claude/
.cursor/
.codex/
.gemini/
AGENTS.md
.mcp.json
```

## That's the pitch

To be completely transparent, we initially created this tool to support a product we're developing, [Skills.new](https://skills.new). It adds a few fancy features on top of everything listed in this blog post, like usage stats and fine-grained governance/permissions.

Despite that fact, this isn't a bait-and-switch. The `sx` client is entirely [open source](https://github.com/sleuth-io/sx) (Apache 2.0), you don't need to create any accounts with us and since you hook it up to a repo you manage, the data always stays under your control.

If you end up trying it out, please send feedback [our way](mailto:support@sleuth.io).

[^x]: <small> Thankfully smart people are trying to come up with [standards](https://github.com/agentsfolder/spec) to address this. Some coding harnesses can also read configurations meant for others, but at the moment, it's still not a solved problem.</small>
