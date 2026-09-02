# Migrating from app.skills.new to a git vault

app.skills.new is shutting down, but nothing in your library needs to be left
behind. `sx vault copy` moves everything — assets with their full version
history, teams, bots, installation scopes, collections, audit history, and
usage history — into a git repository your org controls. After the switch,
`sx install` resolves the same assets for everyone from the new backend; only
the storage changes.

One person with admin access runs the copy. Everyone else just points their
sx at the new vault (see [Switch every teammate over](#6-switch-every-teammate-over)).

## Before you start

- **Update sx everywhere**: `sx update`. Old releases lose hooks,
  claude-code plugins, repo-scoped installs, and teammates' personal installs
  during the copy — the current release carries all of them.
- **Pick the repository host**: any git remote works (GitHub, GitLab,
  Bitbucket, self-hosted). You need push access, and your teammates need at
  least read access.
- **Use SSH** for the remote URL if you can. For private repos over HTTPS,
  configure a git credential helper first (e.g. `gh auth setup-git`).

## 1. Create an empty repository

Create a new private repository with no README or initial commit — sx lays
down the [vault structure](vault-spec.md) on first write:

```bash
gh repo create your-org/ai-assets --private
```

Verify you can reach it:

```bash
git ls-remote git@github.com:your-org/ai-assets.git
```

## 2. Add a git profile for the new vault

Keep your existing skills.new profile — the copy reads from it. Add a second
[profile](profiles.md) pointing at the new repository:

```bash
sx profile add ai-assets
# choose "Git repository" and enter git@github.com:your-org/ai-assets.git
```

If your git identity (`git config user.email`) differs from the email you use
on skills.new, set the profile identity to the skills.new one so team
membership and your personal installs resolve after the switch:

```bash
sx profile edit ai-assets --identity you@company.com
```

Check that your skills.new profile is still the active one (marked `✓`):

```bash
sx profile list
```

## 3. Preview the copy

The preview is read-only and shows exactly what will move:

```bash
sx vault copy --from <skills-new-profile> --to ai-assets --dry-run
```

Read the "Notes / losses" section carefully — it names anything that won't
transfer (see [What doesn't carry over](#what-doesnt-carry-over)).

## 4. Run the copy

```bash
sx vault copy --from <skills-new-profile> --to ai-assets --yes
```

A few things to expect:

- **It takes a while.** Every asset version becomes one commit and push to the
  git vault, so a library with hundreds of versions runs for many minutes.
- **Warnings are per-item, not fatal.** A single failed download skips that
  item and the copy continues. Read the final report rather than just the exit
  code.
- **Re-running is safe for assets.** Versions that already copied are skipped,
  so if the server hiccups (a 500 or 502 on individual items is not unusual),
  run the assets stage again until the report is clean:

  ```bash
  sx vault copy --from <skills-new-profile> --to ai-assets --only assets --yes
  ```

- **Audit and usage are additive.** If the audit or usage stage fails, re-run
  just that stage (`--only audit`, `--only usage`) — but only re-run a stage
  that failed *completely*, because re-importing partially imported events
  duplicates them on the destination.

## 5. Verify the copy

Compare the two vaults before switching anything over:

```bash
sx vault list --profile <skills-new-profile>
sx vault list --profile ai-assets

sx team list --profile ai-assets
sx bot list --profile ai-assets
sx collection list --profile ai-assets
sx stats --profile ai-assets
```

Spot-check a few assets' install scopes:

```bash
sx vault show <asset> --profile ai-assets
```

## 6. Switch every teammate over

Everyone (including you) runs these four commands. Teammates do **not** re-run
the copy — the vault already has everything.

```bash
sx profile add ai-assets        # git@github.com:your-org/ai-assets.git
sx profile use ai-assets        # make it the only active profile
sx install                      # reinstall from the new vault
sx profile remove <skills-new-profile>
```

`sx install` regenerates the lock file from the new vault; the same assets end
up installed in the same places. If someone's git email differs from their
skills.new email, they should set `sx profile edit ai-assets --identity`
(step 2) before running `sx install`, or team- and user-scoped assets won't
resolve for them.

## 7. Lock down the new vault

A fresh git vault has no org-admins, which means anyone with push access can
set any scope. Restore governance right after the migration:

```bash
sx org admin add you@company.com colleague@company.com
```

See [Permissions / RBAC](rbac.md) for what org-admins control.

Bots need new API keys — keys never transfer (they're shown once at
creation):

```bash
sx bot key create <bot-name>
```

## 8. Update CI jobs that install skills

Any workflow that ran `sx install` against skills.new (a Claude PR review, an
agent job) needs a credential for the new private repository — the workflow's
built-in token only reaches the repository it runs in. Add a read-only deploy
key to the vault, store it as a secret, and switch the job to `vault-url` +
`ssh-key`. The full walkthrough, including running the job as an sx bot, is in
[Using your vault from GitHub Actions](github-actions.md).

## What doesn't carry over

The copy report names every skipped item, but three categories are expected:

- **Bot API keys** — regenerate them on the new vault (above).
- **Assets whose downloads fail on the server.** If skills.new returns a
  server error for a specific version, that version can't be recovered from
  the server. The latest version usually copies fine; for "discovered" assets
  whose only version is broken, the original content still lives in the
  repository they were discovered from.
- **App-only features.** Extensions copy as assets but only run inside the
  skills.new app; the `sx cloud` relay has no git-vault equivalent.

## Troubleshooting

- **`repository URL unreachable` when adding the profile** — check SSH access
  (`ssh -T git@github.com`) or set up a credential helper for HTTPS.
- **A teammate's assets vanished after switching** — their profile identity
  doesn't match the email their scopes use. Set it with
  `sx profile edit ai-assets --identity <email>` and re-run `sx install`.
- **The copy report shows `user-scoped installs may only target the
  authenticated caller`** — you're running an sx release without the
  migration fixes. Run `sx update` and re-run the assets stage.
- **Repo-scoped assets don't install from the new vault** — same cause as
  above: older releases wrote repo scopes in a form that can't match a real
  remote. Update sx and re-run `--only assets` (the scope rows are rewritten
  in place).

## Further reading

- [Copying a vault](copy.md) — everything `sx vault copy` moves and how
- [Using your vault from GitHub Actions](github-actions.md) — CI access to a private vault
- [Profiles](profiles.md) — managing multiple vault connections
- [Vault structure](vault-spec.md) — what the git repository contains
- [Permissions / RBAC](rbac.md) — governance on git vaults
