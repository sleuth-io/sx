# sx - Coding Agent Guide

Your team's private npm for AI assets - skills, MCP configs, commands, and more.

## Quick Commands

```bash
make build          # Build binary to ./dist/sx
make test           # Run tests
make format         # Format code
make lint           # Run linter
make prepush        # Format + lint + test + build (run before claiming done)
```

## Before Reporting Work Complete

**ALWAYS run `make prepush` before claiming a task is finished.** CI runs
format, lint, tests, and build — if any of those fail locally, don't
tell the user the work is done.

## Testing Local Changes

After making code changes, build and test with:

```bash
make build && ./dist/sx <command>
```

## Tech Stack

- Go 1.25+
- cobra (CLI framework)
- TOML (config format)

## Key Specifications

- [Vault Spec](docs/vault-spec.md) - Vault structure and management
- [Manifest Spec](docs/manifest-spec.md) - sx.toml format (source of truth)
- [Lock Spec](docs/lock-spec.md) - Per-user resolved lock file format
- [Teams & installs](docs/teams.md) - Team management, targeted installs
- [Audit log](docs/audit.md) - Mutation audit trail format and queries
- [Usage analytics](docs/stats.md) - `sx stats` dashboard and event format
- [Metadata Spec](docs/metadata-spec.md) - Asset metadata format and fields
- [MCP Spec](docs/mcp-spec.md) - MCP server tools (query)

## Development

- Format: `gofmt`
- Lint: `golangci-lint`
- Tests must pass with race detection
- Use conventional commit messages
- **NEVER run `git push` without explicit user permission**

## Releases

Tag and push to trigger automated release:

```bash
git tag v0.1.0
git push origin v0.1.0
```

GoReleaser builds for Linux/macOS/Windows and auto-generates changelog.
