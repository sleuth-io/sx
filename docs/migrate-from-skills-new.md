# Migrating from skills.new to your own git vault

This guide moves a team's library from skills.new into a private git
repository your organization controls. Everything comes along — skills, rules,
commands, MCP configs, hooks, plugins, their full version history, teams, bots,
installation scopes, collections, audit history, and usage history — and `sx`
keeps working exactly as before against the new vault. It covers the whole
migration, start to finish: the vault itself, every teammate's machine, and
your GitHub Actions. Follow it in order and you're done.

**Who does what**

| Part | Who | Time |
|------|-----|------|
| [A. Move the vault](#part-a-move-the-vault) | one admin, once | 15–60 min (mostly waiting on the copy) |
| [B. Switch your machine](#part-b-switch-your-machine) | every teammate | 2 min each |
| [C. Update GitHub Actions](#part-c-update-github-actions) | one admin, once per org | 10 min |
| [D. Clean up](#part-d-clean-up) | one admin | 5 min |

Parts B and C only need the new vault, so do Part A first.

## Before you start

- **Update sx everywhere** — on every machine and in CI — to **v2.3.9 or
  newer**. Older releases silently lose hooks, claude-code plugins, repo-scoped
  installs, and teammates' personal installs during the copy, and fail in CI on
  plugins.

  ```bash
  sx update
  sx --version   # 2.3.9+
  ```

- **The admin running Part A needs**: access to your skills.new library, the
  ability to create a private repository in your GitHub organization, and SSH
  access to GitHub from their machine (`ssh -T git@github.com`).
- **Teammates need** read access to the new repository to install assets,
  and push access to publish with `sx add` or to have their usage counted in
  `sx stats` (see [Access levels](#access-levels)). Most teams simply give
  everyone push access and let sx's org-admin and team-admin rules govern
  who may change what.

---

## Part A: Move the vault

*One admin does this once.*

### A1. Create an empty private repository

Create the repository with **no README and no initial commit** — sx lays down
the vault structure on its first write. `ai-assets` is a good name; use
whatever fits.

```bash
gh repo create your-org/ai-assets --private
git ls-remote git@github.com:your-org/ai-assets.git   # should print nothing and exit 0
```

(In the GitHub UI: New repository → Private → leave "Add a README" unchecked.
Any git host works — GitLab and Bitbucket have deploy keys too — but the
commands in this guide are written for GitHub.)

### A2. Add a git profile alongside your skills.new profile

sx connects to vaults through [profiles](https://github.com/sleuth-io/sx/blob/main/docs/profiles.md).
Keep your existing skills.new profile — the copy reads from it — and add a
second one for the new repository:

```bash
sx profile add ai-assets
```

Answer the prompts:

1. **How will you use sx?** → *Share with my team*
2. **Vault type** → *Git repository*
3. **Repository URL** → `git@github.com:your-org/ai-assets.git`
4. **Clients** → keep your current selection
5. **Identity email** → **the email you use on skills.new.** Team membership
   and personal ("just for me") installs are keyed on it; if it differs from
   your `git config user.email`, this is what makes them resolve after the
   switch.

You can change the identity later with
`sx profile edit ai-assets --identity you@company.com`.

Confirm the skills.new profile is still the active one (the `✓`) — the new
one just exists for now:

```bash
sx profile list
```

Find your skills.new profile's name in that list; the commands below call it
`<skills-new-profile>`.

### A3. Preview the copy

```bash
sx vault copy --from <skills-new-profile> --to ai-assets --dry-run
```

This is read-only. It prints the counts of everything that will move and a
"Notes / losses" section listing anything that won't (see
[What doesn't carry over](#what-doesnt-carry-over)).

### A4. Run the copy

```bash
sx vault copy --from <skills-new-profile> --to ai-assets --yes
```

What to expect:

- **It takes a while.** Every asset version becomes one commit and push, so
  a library with a few hundred versions runs for many minutes. Leave it.
- **Warnings are per item, never fatal.** One failed download skips that item
  and the copy continues. Read the report at the end — don't judge by the exit
  code alone.
- **Re-running the assets stage is safe and expected.** Already-copied
  versions are skipped. If the report shows server errors (a 500 or 502 on
  individual items is common), run the assets stage again until the report is
  clean:

  ```bash
  sx vault copy --from <skills-new-profile> --to ai-assets --only assets --yes
  ```

- **Audit and usage history are additive.** If the audit or usage stage fails,
  re-run only that stage (`--only audit` / `--only usage`) — and only if it
  failed *completely* (0 events copied). Re-importing partially imported
  events duplicates them.

### A5. Verify the copy

Compare the two vaults before anyone switches:

```bash
sx vault list --profile <skills-new-profile>
sx vault list --profile ai-assets

sx team list --profile ai-assets
sx bot list --profile ai-assets
sx collection list --profile ai-assets
sx stats --profile ai-assets
```

Spot-check the install scopes of a few assets you care about — repo-scoped,
team-scoped, and a personal one:

```bash
sx vault show <asset> --profile ai-assets
```

### A6. Tidy team admins

Teams keep the admins they had on skills.new. A team that had *no* admins
gets you as its sole admin and member — a git vault never leaves a team
without an admin. Check the result, and where you'd rather not own a team,
hand it to someone first (the last admin can't leave), then step out;
removing your membership also drops your admin role:

```bash
sx team list --profile ai-assets
sx team member add --profile ai-assets <team> colleague@company.com --admin
sx team member remove --profile ai-assets <team> you@company.com
```

Only a team's admins can change it (org-admins don't override this), so leave
teams that already had admins to them.

`--profile ai-assets` matters for every write in Part A: your skills.new
profile is still the active one, so a command without it changes skills.new.

### A7. Lock down scope changes

A fresh git vault has no org-admins, which means anyone with push access can
set any org-, repo-, path-, or bot-scoped install (team scopes are always
gated on that team's admins). Restore governance now:

```bash
sx org admin add --profile ai-assets you@company.com colleague@company.com
```

Org-admins control org-, repo-, path-, and bot-scoped installs; team admins
keep control of their own team's scope. Details:
[Permissions / RBAC](https://github.com/sleuth-io/sx/blob/main/docs/rbac.md).

### A8. Bots: no API keys any more

Bots copy over with their team memberships, but **git vaults have no bot API
keys** — `sx bot key create` is a skills.new feature. On a git vault a bot is
an identity that a job claims by setting `SX_BOT=<bot-name>`; the repository's
own access control (who can read the repo) is what gates it. Part C shows how
CI uses this.

---

## Part B: Switch your machine

*Every teammate does this, including the admin. Nobody re-runs the copy —
the vault already has everything.*

### B1. Point sx at the new vault

(If you ran Part A, the profile already exists — start at `sx profile use`.)

```bash
sx profile add ai-assets          # Share with my team → Git repository → git@github.com:your-org/ai-assets.git
sx profile use ai-assets          # make it the only active (and default) profile
sx install                        # reinstall everything from the new vault
sx profile remove <skills-new-profile>
```

When `sx profile add` asks for an identity email, enter **your skills.new
email**. If you skipped it or entered something else, set it before
`sx install`:

```bash
sx profile edit ai-assets --identity you@company.com
```

`sx install` regenerates your lock file from the new vault. The same assets
end up installed in the same places; the only visible difference is the
"Source" line now naming the git repository.

### B2. Check what's installed

```bash
sx config      # profile + vault, detected clients, install status
```

If something you expect is missing, see [Troubleshooting](#troubleshooting).

### B3. The sx desktop app

If you use the sx desktop app, open its settings and select the `ai-assets`
profile (or add the repository there). It reads the same config file as the
CLI, so once the CLI switch above is done the app follows.

### B4. Remove skills.new-only pieces

- **Cloud relay.** If you exposed your vault to claude.ai or chatgpt.com
  through the skills.new relay, drop this machine's relay credential — the
  relay serves the skills.new vault, not your git repository:

  ```bash
  sx cloud status
  sx cloud revoke     # removes the local token only
  ```

  The relay itself stays active on skills.new until it's invalidated there —
  that's an admin step in Part D.

- **MCP servers that call app.skills.new.** Any MCP asset whose configuration
  points at `app.skills.new` targets the vault you're leaving. Uninstall it
  locally (`sx uninstall <name>`); the admin removes it from the vault in
  Part D.

### Access levels

| You want to… | Repository access needed |
|--------------|--------------------------|
| Install assets (`sx install`) | read |
| Publish or rescope assets (`sx add`) | push (a teammate blocked by a team's edit gate is offered a pull request instead of a hard failure — that still pushes a branch, so it needs push access too) |
| Have your usage counted in `sx stats` | push (usage events are committed and pushed by your sx, throttled; without push access they stay queued locally — nothing else breaks) |
| Manage teams, org-admins, bots | push (and the relevant team-admin / org-admin role) |


---

## Part C: Update GitHub Actions

*One admin does this once per organization.*

If you run a Claude PR review (or any job) that installs skills with
`sleuth-io/skills-actions/install-skills`, it currently authenticates to
skills.new with an API key. It now needs to reach your private vault
repository instead — and a workflow's built-in `GITHUB_TOKEN` can only read
the repository the workflow runs in, not the vault. The fix is a **read-only
deploy key** on the vault repository, stored as a secret.

### C1. Create a deploy key for the vault

```bash
ssh-keygen -t ed25519 -N "" -C "sx vault deploy key (github actions)" -f sx_vault_key
gh repo deploy-key add sx_vault_key.pub -R your-org/ai-assets -t "GitHub Actions (read-only)"
```

(UI: vault repository → Settings → Deploy keys → Add deploy key, paste
`sx_vault_key.pub`, leave **Allow write access** unchecked.)

A deploy key is tied to that one repository, is read-only, belongs to no user
account, and never expires. Read-only is all `sx install` needs.

### C2. Store the private key as a secret

For one repository:

```bash
gh secret set SX_VAULT_SSH_KEY -R your-org/backend < sx_vault_key
```

For every repository that runs the review, make it an organization secret
instead (needs org-admin rights):

```bash
gh secret set SX_VAULT_SSH_KEY --org your-org --visibility selected \
  --repos your-org/backend,your-org/frontend < sx_vault_key
```

Then delete `sx_vault_key` locally — the secret is the copy that matters.

### C3. Create a bot for CI (recommended)

A CI job has no human identity, so by itself it only receives repo-scoped
and org-wide assets. Give it a bot identity so team-scoped assets resolve
too — the same skills your developers get in that repo:

```bash
sx bot create ci-reviewer --description "PR review job" --team platform
```

### C4. Update the workflow

Change the `install-skills` step from the API key to the vault:

```diff
       - uses: sleuth-io/skills-actions/install-skills@v1
         with:
-          api-key: ${{ secrets.SKILLS_API_KEY }}
+          vault-url: git@github.com:your-org/ai-assets.git
+          ssh-key: ${{ secrets.SX_VAULT_SSH_KEY }}
+          bot: ci-reviewer
           clients: claude-code
```

`@v1` already includes these inputs (or pin `@v1.1`). The key is passed to sx
as `SX_SSH_KEY`; sx writes it to a temp file for git and deletes it when it
exits, so it never lands in a config file or outlives the install step.

If the review job runs on pull requests, skip forks — GitHub withholds secrets
from fork-triggered workflows, so the step would fail there:

```yaml
jobs:
  review:
    if: github.event.pull_request.head.repo.fork == false
```

If you wrote your own install steps instead of using the action, the
equivalent is a git-type sx config plus `SX_SSH_KEY` and `SX_BOT` in the
install step's `env:` — a full example is in
[Using your vault from GitHub Actions](https://github.com/sleuth-io/sx/blob/main/docs/github-actions.md).

### C5. Run it once and check

Open a test pull request. In the job log you should see
`SSH key loaded from environment variable`, then `Installed N assets`, then
`Found skills: …`. A `⊘` line for a claude-code *plugin* is normal — plugins
need the `claude` CLI, which the runner doesn't have at that point — and
doesn't fail the job.

---

## Part D: Clean up

Once Parts A–C are done:

1. **Remove the skills.new API key secrets** from GitHub (`SKILLS_API_KEY` or
   whatever you named them) — they're no longer used:

   ```bash
   gh secret delete SKILLS_API_KEY -R your-org/backend
   ```

2. **Confirm nobody is still on the old profile**: `sx profile list` on each
   machine should no longer show a `https://app.skills.new` entry.
3. **Remove skills.new-only assets from the vault** — any MCP server whose
   configuration points at `app.skills.new` (teammates uninstalled it locally
   in B4; this removes it from the vault once):

   ```bash
   sx vault remove <name>
   ```

4. **Invalidate the cloud relay server-side**, if you used one: `sx cloud
   revoke` only deletes local tokens, so visit
   [app.skills.new/relay](https://app.skills.new/relay) and revoke it there.
5. **Keep the repository private** and treat read access to it as access to
   your team's skills.

---

## What doesn't carry over

The copy report lists every skipped item. These are the expected ones:

- **Bot API keys.** Not a loss on a git vault — there are none. Jobs set
  `SX_BOT=<name>` instead (Part C).
- **Individual versions skills.new can't serve.** If the server returns an
  error for a specific historical version, that version can't be recovered.
  The latest version almost always copies fine. "Discovered" assets whose only
  version fails still have their content in the repository they were
  discovered from.
- **skills.new-only features**: the web UI, extensions (they copy as assets
  but only run inside the app), the cloud relay for claude.ai/chatgpt.com,
  repository discovery of skills, quality scores, and the `sx query` MCP tool
  (Sleuth AI Query). The git vault has the CLI, the desktop app, `sx stats`,
  and the audit log.

## Troubleshooting

- **`sx profile add` says the repository URL is unreachable** — SSH access.
  Run `ssh -T git@github.com`; if that fails, add your SSH key to GitHub. (For
  HTTPS URLs, configure a credential helper first: `gh auth setup-git`.)
- **After switching, team-scoped or personal assets are missing** — the
  profile identity doesn't match the email your scopes use. Fix with
  `sx profile edit ai-assets --identity <your-skills.new-email>` and run
  `sx install` again.
- **After switching, repo-scoped assets are missing in a repo** — check the
  scope with `sx vault show <asset>`; it should read `github.com/org/repo`.
  If it reads a bare `org/repo`, the copy was made with an sx older than
  2.3.8: update sx and re-run `sx vault copy … --only assets --yes` (scopes are
  rewritten in place).
- **The copy report says `user-scoped installs may only target the
  authenticated caller`** — same cause: update sx and re-run the assets stage.
- **CI: `Permission denied (publickey)`** — the deploy key isn't on the vault
  repository, or the secret holds the public key or a truncated one. The value
  must be the whole private key file, `-----BEGIN` to `-----END`.
- **CI: `fatal: could not read Username for 'https://github.com'`** — the
  `vault-url` is HTTPS with no credential; use the `git@github.com:…` form
  with `ssh-key`.
- **CI: `Found skills: none`** — the vault has nothing scoped to that
  repository or org-wide, or `bot:` names a bot that isn't in the right team.
  `sx vault show <asset>` shows an asset's scopes.
- **`sx stats` is missing some people's usage** — they don't have push access
  to the vault repository (see [Access levels](#access-levels)).

## Reference

- [Copying a vault](https://github.com/sleuth-io/sx/blob/main/docs/copy.md) — everything `sx vault copy` moves and how
- [Using your vault from GitHub Actions](https://github.com/sleuth-io/sx/blob/main/docs/github-actions.md) — the CI setup in depth, including the GitHub App alternative to deploy keys
- [Profiles](https://github.com/sleuth-io/sx/blob/main/docs/profiles.md)
- [Bots](https://github.com/sleuth-io/sx/blob/main/docs/bots.md) and [Teams](https://github.com/sleuth-io/sx/blob/main/docs/teams.md)
- [Permissions / RBAC](https://github.com/sleuth-io/sx/blob/main/docs/rbac.md)
- [Vault structure](https://github.com/sleuth-io/sx/blob/main/docs/vault-spec.md) — what ends up in the repository
- [sleuth-io/skills-actions](https://github.com/sleuth-io/skills-actions) — the GitHub Action
