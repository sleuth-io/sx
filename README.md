# sx

sx is your team's private npm for AI assets - skills, MCP configs, commands, and more. Capture
what your best AI users have learned and spread it to everyone automatically.

![Demo](docs/demo.gif)

## Why sx?

Your best developers have figured out how to make AI assistants incredibly productive - custom skills, MCP configs, slash commands, proven patterns. But that knowledge is stuck on their machines.

**Current workarounds don't scale:**
- **Copy into each repo** - Duplication nightmare, no central updates, version drift
- **Global config** - Bloats context for projects/tasks that don't need those skills
- **Client plugins** - Manually install each one, locked to one AI client, no bundling

**sx solves this by:**
- **Sharing expertise** - Turn individual discoveries into team assets
- **Instant onboarding** - New devs inherit the team's AI playbook on day one
- **Central updates** - Change once in your vault, everyone gets the update
- **Scoped installation** - Right assets for each repo, no context bloat
- **Works with any AI client** - Claude Code, Cursor, and more (coming soon)

## Quickstart

```bash
curl -fsSL https://raw.githubusercontent.com/sleuth-io/sx/main/install.sh | bash
```

then

```bash
# Initialize
sx init

# Add an asset from your vault
sx add /path/to/my-skill

# Install assets to your current project
sx install
```

### Already using Claude Code?

If you've built up skills, plugins, or MCP configs in your `.claude` directory, `sx` helps you version, sync across machines, and share with teammates.

```bash
# Add your existing skills/commands (sx auto-detects the type)
sx add ~/.claude/commands/my-command
sx add ~/.claude/skills/my-skill
sx add code-review@claude-plugins-official
```

Your prompt files stay exactly as they are - `sx` just wraps them with metadata for versioning.

## What can you build and share?

- **Skills** - Custom prompts and behaviors for specific tasks
- **Agents** - Autonomous AI agents with specific goals
- **Commands** - Slash commands for quick actions
- **Hooks** - Automation triggers for lifecycle events
- **MCP Servers** (experimental) - Model Context Protocol (MCP) servers for external integrations
- **Plugins** - Claude Code plugin bundles with commands, skills, and more

## Distribution models

Choose the right distribution model for your team:

### Local (Personal)

Perfect for easily sharing personal tools across multiple personal projects

```bash
sx init --type path --path my/vault/path
```

### Git vault (Small teams)

Share assets through a shared git vault

```bash
sx init --type git --repo git@github.com:yourteam/skills.git
```

### Skills.new (Large teams and enterprise)

Centralized management with a UI for discovery, creation, sharing, and usage analytics

```bash
sx init --type sleuth
```

## How it works

sx uses a lock file (like package-lock.json) for deterministic installations across your team:

1. **Create** assets with metadata (name, version, dependencies)
2. **Share** to your vault
3. **Install** globally, per repository, or even per path (monorepo support!)
4. **Auto-install** on new Claude Code sessions
5. **Stay synchronized** - everyone gets the same tools automatically

## Supported Clients

| Client | Status         | Notes |
|--------|----------------|-------|
| Claude Code | ✅ Supported    | Full support for all asset types |
| Cursor | ✅ Experimental | Skills, MCP servers, commands, hooks |
| GitHub Copilot | Coming soon    | |
| Gemini | Coming soon    | |
| Codex | Coming soon    | |

## Roadmap
- ✅ Local, Git, and Skills.new vaults
- ✅ Claude Code support
- ✅ Cursor support (experimental)
- **More clients** - GitHub Copilot, Gemini, Codex
- **Skill discovery** - Use Skills.new to discover relevant skills from your code and architecture
- **Analytics** - Track skill usage and impact

## License

See LICENSE file for details.

---

## Development

<details>
<summary>Click to expand development instructions</summary>

### Documentation

- [Vault Spec](docs/vault-spec.md) - Skills vault structure
- [Metadata Spec](docs/metadata-spec.md) - Skill metadata format
- [Lock Spec](docs/lock-spec.md) - Lock file format
- [MCP Spec](docs/mcp-spec.md) - MCP server and query tool


### Prerequisites

Go 1.25 or later is required. Install using [gvm](https://github.com/moovweb/gvm):

```bash
# Install gvm
bash < <(curl -s -S -L https://raw.githubusercontent.com/moovweb/gvm/master/binscripts/gvm-installer)

# Install Go (use go1.4 as bootstrap if needed)
gvm install go1.4 -B
gvm use go1.4
export GOROOT_BOOTSTRAP=$GOROOT
gvm install go1.25
gvm use go1.25 --default
```

### Building from Source

```bash
make init           # First time setup (install tools, download deps)
make build          # Build binary
make install        # Install to GOPATH/bin
```

### Testing

```bash
make test           # Run tests with race detection
make format         # Format code with gofmt
make lint           # Run golangci-lint
make prepush        # Run before pushing (format, lint, test, build)
```

### Releases

Tag and push to trigger automated release via GoReleaser:

```bash
git tag v0.1.0
git push origin v0.1.0
```

</details>
