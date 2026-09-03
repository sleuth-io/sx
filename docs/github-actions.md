# Using your vault from GitHub Actions

CI jobs that run an AI reviewer or agent — a Claude PR review, a scheduled
refactoring bot — should load the same skills, rules, and standards your
developers get locally. `sx install` does that in a workflow exactly as it
does on a laptop; the one difference is **how the job authenticates to the
vault**, because a workflow starts with no credentials for any repository but
its own.

This page covers a vault stored in a **private git repository**. For the
composite action that wraps the steps below, see
[sleuth-io/skills-actions](https://github.com/sleuth-io/skills-actions).

## Why the default token isn't enough

Every workflow gets a `GITHUB_TOKEN` scoped to the repository the workflow
runs in. It can't read any other repository — not even one in the same
organization. If your vault is `your-org/ai-assets` and the workflow runs in
`your-org/backend`, the job needs a credential that reaches `ai-assets`.

You have three options; the first is the one to reach for:

| Credential                     | Scope                 | Lifetime          | Setup                 |
|--------------------------------|-----------------------|-------------------|-----------------------|
| **Read-only deploy key**       | one repository        | until you delete it | one key, one secret |
| GitHub App installation token  | repositories you pick | minutes (minted per run) | an App + two secrets |
| Fine-grained personal token    | repositories you pick | expires, tied to a person | one secret        |

## How sx authenticates to a git vault

sx talks to a git vault with the ordinary `git` binary. For SSH remotes it
accepts a private key three ways — `--ssh-key /path`, `SX_SSH_KEY=/path`, or
`SX_SSH_KEY` set to the **key content itself**. The last form is what CI
uses: the secret's value goes straight into the environment, sx writes it to
a `0600` temp file and runs git with

```
GIT_SSH_COMMAND="ssh -i <tmpfile> -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new"
```

Nothing secret is written to the sx config file, `IdentitiesOnly` stops ssh
from trying the runner's other keys, and `accept-new` accepts the host key on
first contact (GitHub's runners already ship github.com's host keys, so this
is a no-op there). sx writes the key once per process and deletes the temp
file when it exits, so the key doesn't outlive the `sx install` step. GitHub
masks the secret's value in job logs.

## Recommended: a read-only deploy key

A **deploy key** is an SSH public key attached to one repository. It grants
access to that repository only, can be read-only, belongs to no user account,
and never expires — which makes it the right shape for "this CI job may read
the vault". One key can be attached to only one repository, so generate a
dedicated key for the vault rather than reusing one.

### 1. Generate the key pair

```bash
ssh-keygen -t ed25519 -N "" -C "sx vault deploy key (github actions)" -f sx_vault_key
```

This creates `sx_vault_key` (private) and `sx_vault_key.pub` (public).

### 2. Add the public key to the vault repository

```bash
gh repo deploy-key add sx_vault_key.pub -R your-org/ai-assets -t "GitHub Actions (read-only)"
```

Or in the browser: vault repository → **Settings → Deploy keys → Add deploy
key**, paste `sx_vault_key.pub`, and leave **Allow write access** unchecked.

### 3. Store the private key as a secret

For a single repository:

```bash
gh secret set SX_VAULT_SSH_KEY -R your-org/backend < sx_vault_key
```

For many repositories, create it once as an **organization secret** and share
it with the repositories that need it (this needs org-admin rights):

```bash
gh secret set SX_VAULT_SSH_KEY --org your-org --visibility selected \
  --repos your-org/backend,your-org/frontend < sx_vault_key
```

Then delete the local private key — the secret is its only copy you need.

### 4. Use it in the workflow

With the composite action:

```yaml
- uses: sleuth-io/skills-actions/install-skills@v1
  with:
    vault-url: git@github.com:your-org/ai-assets.git
    ssh-key: ${{ secrets.SX_VAULT_SSH_KEY }}
    clients: claude-code
```

Or by hand, if you build or pin sx yourself:

```yaml
- name: Configure sx
  run: |
    mkdir -p ~/.config/sx
    cat > ~/.config/sx/config.json <<'JSON'
    {
      "defaultProfile": "default",
      "profiles": {
        "default": { "type": "git", "repositoryUrl": "git@github.com:your-org/ai-assets.git" }
      },
      "forceEnabledClients": ["claude-code"]
    }
    JSON

- name: Install skills
  env:
    SX_SSH_KEY: ${{ secrets.SX_VAULT_SSH_KEY }}
  run: sx install
```

`sx install` resolves the assets scoped to the checked-out repository (it
matches the checkout's `origin` remote against the vault's repo scopes) plus
everything installed org-wide, and writes them where the enabled clients look
for them — `.claude/skills/` in the checkout for repo-scoped skills, the
runner's home directory for global ones.

## Installing as a bot

A CI job has no human identity, so out of the box it only receives
**repo-scoped** and **org-wide** assets. To give it team-scoped assets (or
assets installed to it specifically), run it as an sx [bot](bots.md):

```bash
# once, from a machine with vault write access
sx bot create ci-reviewer --description "PR review job" --team platform
```

```yaml
- uses: sleuth-io/skills-actions/install-skills@v1
  with:
    vault-url: git@github.com:your-org/ai-assets.git
    ssh-key: ${{ secrets.SX_VAULT_SSH_KEY }}
    bot: ci-reviewer
    clients: claude-code
```

(By hand: set `SX_BOT: ci-reviewer` in the install step's `env`.)

On a git vault a bot is identity-only — anyone who can read the vault can
claim it — which matches the vault's own trust model: git read access *is*
asset access. The deploy key is what gates that access.

## Security notes

- **Read-only is enough.** `sx install` only reads the vault; publishing from
  CI is a separate decision that deserves a separate, write-capable credential.
- **Fork pull requests don't get secrets.** GitHub withholds repository
  secrets from workflows triggered by forks, so guard the job
  (`if: github.event.pull_request.head.repo.fork == false`) or let it degrade
  to a review without skills, as the [sx repo's own workflow](../.github/workflows/claude-pr-review.yml)
  does.
- **Rotate by replacing, not editing.** Generate a new pair, add the new
  deploy key, update the secret, delete the old deploy key. Deleting a deploy
  key revokes it immediately.
- **Never print the key.** Steps that `echo` environment variables will have
  the value masked by GitHub, but `set -x` in a script that constructs an ssh
  command can still leak fragments. sx itself logs only the key's type.
- **Keep the key's lifetime to the install step.** sx removes its temp copy
  of the key on exit. If a later step in the same job runs an agent with
  shell access, don't also export `SX_SSH_KEY` at the job level — scope the
  `env:` to the install step, as the examples here do.
- **Deploy keys show who is using them.** The vault repository's Deploy keys
  page records when each key was last used.

## Alternatives

### GitHub App installation token

If your organization forbids deploy keys or wants short-lived credentials,
create a GitHub App with **Contents: Read** permission, install it on the
vault repository, and mint a token per run. Point git's HTTPS remotes at it
with an `insteadOf` rewrite so sx (and anything else) picks it up:

```yaml
- uses: actions/create-github-app-token@v2
  id: vault-token
  with:
    app-id: ${{ vars.SX_VAULT_APP_ID }}
    private-key: ${{ secrets.SX_VAULT_APP_PRIVATE_KEY }}
    repositories: ai-assets

- name: Route vault access through the app token
  run: |
    git config --global url."https://x-access-token:${{ steps.vault-token.outputs.token }}@github.com/".insteadOf "https://github.com/"

- uses: sleuth-io/skills-actions/install-skills@v1
  with:
    vault-url: https://github.com/your-org/ai-assets.git   # HTTPS, no ssh-key
    clients: claude-code
```

The token expires after an hour and is scoped to exactly the repositories you
name. The cost is the App itself: creating it, installing it on the vault, and
storing its private key as a secret.

### Fine-grained personal access token

A fine-grained PAT with **Contents: Read** on the vault repository works the
same way as the App token (`insteadOf` rewrite, HTTPS `vault-url`), but it is
tied to whoever created it and expires on a schedule. Treat it as a stopgap.

## Troubleshooting

- **`Permission denied (publickey)`** — the deploy key isn't on the vault
  repository, or the secret holds the public key / a truncated key. The value
  must be the whole private key file, `-----BEGIN` to `-----END`.
- **`fatal: could not read Username for 'https://github.com'`** — the
  `vault-url` is HTTPS and no credential is configured. Either supply the
  deploy key (sx rewrites an HTTPS GitHub URL to SSH when `SX_SSH_KEY` is set)
  or route HTTPS through an App token as described below.
- **The job reports `skills=none`** — the vault has nothing scoped to this
  repository or org-wide, or the checkout's remote doesn't match the vault's
  repo scope rows. Run `sx vault show <asset>` locally to see an asset's
  scopes; team- and bot-scoped assets need `bot:` (above).
- **Host key verification failed** — a self-hosted runner without GitHub's
  host keys and a locked-down ssh config. Add `github.com` to the runner's
  `known_hosts` (`ssh-keyscan github.com >> ~/.ssh/known_hosts`).

## Further reading

- [Bots](bots.md) — bot identities and `SX_BOT`
- [Environment variables](environment-variables.md) — `SX_SSH_KEY`, `SX_BOT`, isolation recipes
- [Migrating from skills.new to your own git vault](migrate-from-skills-new.md)
- [sleuth-io/skills-actions](https://github.com/sleuth-io/skills-actions) — the composite action
