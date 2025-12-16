# sx

sx is a package manager for AI coding assistants. Create, version, and distribute reusable AI
tools across your entire team. Think NPM for AI agents -- install once, use everywhere.

![Demo](docs/demo.gif)

## Why sx?
- Onboard new developers instantly with your team's tribal knowledge
- Expand successful AI use from experts to everyone
- Spread best practices to any AI tool (coming soon)

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

## What can you build and share?

- **Skills** - Custom prompts and behaviors for specific tasks
- **Agents** - Autonomous AI agents with specific goals
- **Commands** - Slash commands for quick actions
- **Hooks** - Automation triggers for lifecycle events
- **MCP Servers** - Model Context Protocol (MCP) servers for external integrations

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

Centralized, effortless management with a UI for discovery, creation, and sharing at scale

```bash
sx init --type sleuth
```

## How it works

sx uses a lock file, like package-lock.json, for deterministic installations in the right context:

1. **Create** assets with metadata (name, version, dependencies)
2. **Publish** to your chosen vault
3. **Share** the asset globally, per repository, or even per path in a repository (monorepo support!)
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
