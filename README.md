# Sleuth Skills

Sleuth Skills is a package manager for AI coding assistants. Create, version, and distribute reusable AI 
tools across your entire team. Think NPM for AI agents -- install once, use everywhere.

## Why Sleuth Skills?
- Onboard new developers instantly with your team's tribal knowledge
- Expand successful AI use from experts to everyone
- Spread best practices to any AI tool (coming soon)

## Quickstart

```bash
curl -fsSL https://raw.githubusercontent.com/sleuth-io/skills/main/install.sh | bash
```

then

```bash
# Initialize
skills init

# Add a skill from your repository
skills add /path/to/my-skill

# Install skills to your current project
skills install
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
skills init --type path --path my/repository/path
```

### Git repository (Small teams)

Share skills through a shared git repository

```bash
skills init --type git --repo git@github.com:yourteam/skills.git
```

### Sleuth (Large teams and enterprise)

Centralized, effortless management with a UI for discovery, creation, and sharing at scale

```bash
skills init --type sleuth
```

## How it works

Sleuth Skills uses a lock file, like package-lock.json, for deterministic installations in the right context:

1. **Create** skills with metadata (name, version, dependencies)
2. **Publish** to your chosen repository
3. **Share** the skill globally, per repository, or even per path in a repository (monorepo support!)
4. **Auto-install** on new Claude Code sessions
5. **Stay synchronized** - everyone gets the same tools automatically

## Roadmap
- ✅ Local, Git, and Sleuth repositories
- ✅ Claude Code support
- **Multi-client support** - Use the same skills in Cursor, Windsurf, Cline
- **Skill discovery** - Use Sleuth to discover relevant skills from your code and architecture
- **Analytics** - Track skill usage and impact

## License

See LICENSE file for details.

---

## Development

<details>
<summary>Click to expand development instructions</summary>

### Documentation

- [Repository Spec](docs/repository-spec.md) - Skills repository structure
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
