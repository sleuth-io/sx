# SX Profiles

## Overview

Profiles let you connect sx to multiple vaults. Common reasons to have
more than one:

- Personal projects vs. work projects
- Different teams or organizations
- Development vs. production vaults
- Local testing vs. shared team vaults

Profiles can be active one-at-a-time (the classic single-profile model)
or several-at-once. When more than one profile is active, `sx install`
fetches every active profile's lock file in parallel and merges the
applicable assets into one installation pass.

Each profile carries its own vault URL, auth, and an optional
**identity email** used to resolve team/user scopes — so your work
profile can resolve `--team backend` against your work email and your
personal profile against your personal email on the same machine.

## Active set vs. default

Two profile-level concepts coexist:

- **Active profiles** — every profile in this set contributes assets at
  install time. `sx install` reads lock files from all of them.
- **Default profile** — the single write target for mutating commands
  (`sx add`, `sx team …`, `sx role …`) when `--profile` isn't passed.
  Also the conflict tiebreaker: when two active profiles publish an
  asset with the same name, the default profile's copy wins. If the
  default isn't in the active set, the first-listed active profile
  wins and sx prints a warning.

The default profile is always part of the active set (sx auto-activates
it when you change defaults). The active set always contains at least
one profile — you can't deactivate the last one.

## Commands

### List profiles

```bash
sx profile list
```

Output:

```
✓★ work     git@github.com:company/skills.git (alice@company.com)
✓  personal git@github.com:alice/skills.git (alice@personal.com)
   archive  file:///Users/alice/.sx/old-vault

✓ = active   ★ = default
```

### Add a profile

```bash
sx profile add <profile-name>
```

Launches the interactive vault-configuration flow, then prompts for the
profile's identity email (defaulting to `git config user.email`).

### Activate / deactivate

```bash
sx profile activate <name>     # add to the active set
sx profile deactivate <name>   # remove from the active set
```

`activate` is additive: other profiles stay active. `deactivate` runs
cleanup on the next `sx install` (its installed assets are removed).
Cannot deactivate the last active profile.

### Default

```bash
sx profile default <name>      # set the default profile
```

Auto-activates the profile if it isn't already active. This is the
profile that mutating commands target when `--profile` isn't passed,
and the conflict tiebreaker for the install merge.

### Use (exclusive switch)

```bash
sx profile use <name>          # exclusive: deactivates all others
```

`use` is the original single-profile workflow: it sets `<name>` as the
only active profile and the default. Equivalent to deactivating
everything else and then `sx profile default <name>`.

### Edit identity

```bash
sx profile edit <name> --identity alice@company.com
sx profile edit <name>          # interactive prompt
```

The identity email is used to resolve team and user scopes for this
profile. When empty, sx falls back to `git config user.email`.

### Remove a profile

```bash
sx profile remove <profile-name>
```

You cannot remove the last remaining profile. If the removed profile
was the default, sx picks a new default automatically.

### Current

```bash
sx profile current
```

Prints every active profile, one per line, with `(default)` after the
default profile.

## Profile selection priority

For a single command:

1. **`--profile` flag** — explicit override (accepts comma-separated
   names for multi-active, e.g. `--profile work,personal`).
2. **`SX_PROFILE` env var** — session-level override, same syntax.
3. **`ActiveProfiles` from config** — the persisted active set.

When an override is provided, its order is preserved. Otherwise sx
bubbles the default profile to the front of the active set so it wins
conflicts.

## Conflict resolution between active profiles

When two active profiles publish an asset with the same name:

- If the default profile is one of them → the default wins silently
  (info-level log entry).
- If the default isn't in the active set → the first-listed active
  profile wins, and sx prints a warning showing what was shadowed.

Shadowed assets aren't downloaded or installed; only the winner is.

### Mutating commands target the default profile

`sx add`, `sx install <asset> --org/--repo/--team/...`, `sx team`,
`sx role`, and similar mutating commands write to the **default**
profile's vault when no `--profile` flag is given. `sx install`'s
set-target form verifies that the asset exists in that vault before
writing and errors with a hint to use `--profile <name>` if it
doesn't — so retargeting an asset that lives in a non-default active
profile is a one-flag change.

### Cross-profile dependencies are not resolved

Dependency resolution runs **per profile**, against that profile's own
lock file. If an asset in profile `work` declares a dependency on a
skill that lives only in profile `personal`, `sx install` fails the
work profile's resolve step rather than reaching across vaults. Bundle
shared dependencies into the same vault if you need them to compose.

## Configuration storage

Profiles are stored in the sx configuration file at
`~/.config/sx/config.json`:

```json
{
  "defaultProfile": "work",
  "activeProfiles": ["work", "personal"],
  "profiles": {
    "work": {
      "type": "git",
      "repositoryUrl": "git@github.com:company/skills.git",
      "identity": "alice@company.com"
    },
    "personal": {
      "type": "git",
      "repositoryUrl": "git@github.com:alice/skills.git",
      "identity": "alice@personal.com"
    }
  }
}
```

### Profile fields

| Field | Description |
|-------|-------------|
| `type` | Vault type: `sleuth`, `git`, or `path` |
| `repositoryUrl` | Vault URL or path |
| `serverUrl` | (Sleuth only) Server URL if different from `repositoryUrl` |
| `authToken` | (Sleuth only) OAuth authentication token |
| `identity` | Email used to resolve team/user scopes (defaults to `git config user.email` when empty) |

### Top-level fields

| Field | Description |
|-------|-------------|
| `defaultProfile` | Write-target profile and conflict tiebreaker |
| `activeProfiles` | Ordered list of profiles `sx install` reads from |
| `forceEnabledClients` / `forceDisabledClients` | Global client toggles |

## Examples

### Work + personal active at the same time

```bash
sx profile add work       # configure work vault + identity
sx profile add personal   # configure personal vault + identity
sx profile activate personal  # add to active set alongside work

# sx install now resolves assets from both vaults
sx install
```

### One-off command against a single profile

```bash
sx install --profile work    # ignore other active profiles for this run
sx add ./my-skill --profile personal  # publish to personal vault
```

### Switch exclusively

```bash
sx profile use work    # work is now the only active profile
```

### CI/CD

```yaml
jobs:
  deploy:
    env:
      SX_PROFILE: production
    steps:
      - run: sx install
```
