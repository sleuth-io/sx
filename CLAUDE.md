# sx - Coding Agent Guide

A CLI tool for managing AI assets - reusable units of AI agent behavior.

## Quick Commands

```bash
make build          # Build binary
make test           # Run tests
make format         # Format code
make lint           # Run linter
```

## Tech Stack

- Go 1.25+
- cobra (CLI framework)
- TOML (config format)

## Key Specifications

- [Vault Spec](docs/vault-spec.md) - Vault structure and management
- [Metadata Spec](docs/metadata-spec.md) - Asset metadata format and fields
- [Requirements Spec](docs/requirements-spec.md) - Dependency requirements syntax
- [Lock Spec](docs/lock-spec.md) - Lock file format for resolved dependencies
- [MCP Spec](docs/mcp-spec.md) - MCP server tools (read_skill, query)

## Development

- Format: `gofmt`
- Lint: `golangci-lint`
- Tests must pass with race detection
- Use conventional commit messages

## Releases

Tag and push to trigger automated release:

```bash
git tag v0.1.0
git push origin v0.1.0
```

GoReleaser builds for Linux/macOS/Windows and auto-generates changelog.
