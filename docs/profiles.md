# SX Profiles

## Overview

Profiles allow you to maintain multiple sx configurations and switch between them. This is useful when you need to connect to different vaults for different contexts, such as:

- Personal projects vs. work projects
- Different teams or organizations
- Development vs. production vaults
- Local testing vs. shared team vaults

Each profile stores its own vault configuration (type, URL, authentication), while global settings like enabled clients are shared across all profiles.

## Commands

### List Profiles

Show all configured profiles:

```bash
sx profile list
```

**Output:**

```
✓ default  https://app.skills.new
  work     git@github.com:company/skills.git
  local    file:///Users/me/.sx/vault
```

The active profile is marked with a checkmark (`✓`).

### Add a Profile

Create a new profile:

```bash
sx profile add <profile-name>
```

**Example:**

```bash
sx profile add work
```

This launches the interactive configuration flow (same as `sx init`) to set up the new profile's vault connection. Your existing profiles and default profile setting are preserved.

### Switch Profiles

Change the active profile:

```bash
sx profile use <profile-name>
```

**Example:**

```bash
sx profile use work
```

This sets the default profile for all future sx commands.

### Show Current Profile

Display the currently active profile:

```bash
sx profile current
```

**Output:**

```
work
```

### Remove a Profile

Delete a profile:

```bash
sx profile remove <profile-name>
```

**Example:**

```bash
sx profile remove old-profile
```

You cannot remove the last remaining profile.

## Profile Selection Priority

The active profile is determined by (in order of priority):

1. **`--profile` flag** - Explicit flag on any command
2. **`SX_PROFILE` environment variable** - Session-level override
3. **Default profile** - Configured via `sx profile use`

### Using the `--profile` Flag

Run any sx command with a specific profile:

```bash
sx install --profile work
sx list --profile personal
```

### Using Environment Variables

Set the profile for an entire shell session:

```bash
export SX_PROFILE=work
sx install  # Uses "work" profile
sx list     # Uses "work" profile
```

This is useful for CI/CD environments or scripts.

## Configuration Storage

Profiles are stored in the sx configuration file at `~/.config/sx/config.json`:

```json
{
  "defaultProfile": "default",
  "profiles": {
    "default": {
      "type": "sleuth",
      "repositoryUrl": "https://app.skills.new",
      "authToken": "..."
    },
    "work": {
      "type": "git",
      "repositoryUrl": "git@github.com:company/skills.git"
    },
    "local": {
      "type": "path",
      "repositoryUrl": "file:///Users/me/.sx/vault"
    }
  },
  "enabledClients": ["claude-code", "cursor"]
}
```

### Profile Fields

Each profile contains:

| Field | Description |
|-------|-------------|
| `type` | Vault type: `sleuth`, `git`, or `path` |
| `repositoryUrl` | Vault URL or path |
| `serverUrl` | (Sleuth only) Server URL if different from repositoryUrl |
| `authToken` | (Sleuth only) OAuth authentication token |

### Global Settings

These settings apply to all profiles:

| Field | Description |
|-------|-------------|
| `defaultProfile` | Name of the default profile |
| `enabledClients` | List of AI clients to install assets to |

## Examples

### Example 1: Personal and Work Separation

Set up separate profiles for personal and work projects:

```bash
# Initial setup creates "default" profile
sx init

# Add work profile connected to company vault
sx profile add work
# Select "Share with my team" -> "Git repository"
# Enter: git@github.com:company/skills.git

# Switch between them
sx profile use default   # Personal projects
sx profile use work      # Work projects
```

### Example 2: Local Development

Create a local profile for testing new skills:

```bash
sx profile add local
# Select "Just for myself"
# Uses local vault at ~/.config/sx/vault

# Test skills locally
sx profile use local
sx add ./my-new-skill

# Switch back to shared vault
sx profile use default
```

### Example 3: CI/CD Pipeline

Use environment variables in CI:

```yaml
# .github/workflows/deploy.yml
jobs:
  deploy:
    env:
      SX_PROFILE: production
    steps:
      - run: sx install
```

### Example 4: Quick Profile Override

Use a different profile for a single command:

```bash
# Install from work vault into current project
sx install --profile work

# List assets from personal vault
sx list --profile personal
```
