# SX Asset Lock File Specification

## Overview

This specification defines `sx.lock`, a standardized format for recording AI client assets (MCPs, skills, agents) to enable reproducible configuration across environments and client tools (Claude Code, Gemini, ChatGPT, etc.). The format is inspired by PEP 751, prioritizing human readability, machine generation, and clear dependency tracking.

## File Naming

Lock files must be named:

- `sx.lock` (default)
- `sx.<name>.lock` (named variants for specific configurations)

## Core Structure (TOML Format)

### Top-Level Metadata

```toml
lock-version = "1.0"                    # Required; format version
version = "abc123def456..."             # Required; hash/version of this lock file instance
created-by = "sx-cli/0.1.0"             # Required; tool that created the lock

[[assets]]
# Asset entries (see below)
```

## Asset Entry Structure

Each `[[assets]]` table contains:

```toml
[[assets]]
name = "github-mcp"                     # Required; normalized name
version = "1.2.3"                       # Required; semantic version
type = "mcp"                            # Required; asset type
clients = ["claude-code", "gemini"]     # Optional; applicable client tools

# Source specification (required, exactly one source table)
[assets.source-http]                    # HTTP source
# OR
[assets.source-path]                    # Path source
# OR
[assets.source-git]                     # Git source

# Repository scope specification (optional)
# If omitted, asset is installed globally
[[assets.scopes]]                       # Array of scope installations
repo = "https://github.com/user/repo"   # Required; repository URL
paths = ["services/api", "services/worker"]  # Optional; specific paths within repo
                                        # If omitted/empty, installed for entire repo

# Dependencies (optional)
dependencies = [ ... ]                  # Array of dependency references
```

### Asset Types

- `mcp`: MCP server (zip contains server code for packaged mode, or only metadata.toml for config-only mode)
  - Legacy alias: `mcp-remote` is accepted and treated as `mcp`
- `skill`: Packaged skill (zip contains skill code)
- `agent`: Packaged agent (zip contains agent code)
- `command`: Slash command (zip contains command markdown file)
- `hook`: Event hook (zip contains hook scripts and hook-config.yml)
- `rule`: Shared AI coding rule (installed to client-specific rules directories)

## Source Types

Assets use **source tables** following PEP 751 conventions. Each asset specifies exactly one source type using mutually-exclusive tables: `[assets.source-http]`, `[assets.source-path]`, `[assets.source-git]`, or `[assets.source-git-dir]`.

### HTTP Source

Used for assets hosted on web servers (primary SX registry, internal servers, etc.).

```toml
[[assets]]
name = "github-mcp"
version = "1.2.3"
type = "mcp"

[assets.source-http]
url = "https://vault.example.com/assets/github-mcp/1.2.3/github-mcp-1.2.3.zip"
hashes = {sha256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"}
size = 145678
uploaded-at = "2025-11-25T10:30:00Z"
```

**URL Structure**: Following Maven/PyPI patterns, assets and metadata are stored at predictable URLs:

- **Asset**: `{base-url}/{name}/{version}/{name}-{version}.zip`
- **Metadata**: `{base-url}/{name}/{version}/metadata.toml`

Example:

- Asset: `https://vault.example.com/assets/github-mcp/1.2.3/github-mcp-1.2.3.zip`
- Metadata: `https://vault.example.com/assets/github-mcp/1.2.3/metadata.toml`

See [vault-spec.md](vault-spec.md) for complete vault structure and protocols.

**Fields**:

- `url`: Full URL to asset zip file (required)
- `hashes`: Map of hash algorithm to hex digest (required for HTTP sources)
  - Supported algorithms: `sha256`, `sha512`
  - Client MUST verify hash before installation
  - Multiple hashes can be provided for defense in depth
- `size`: File size in bytes (optional but recommended)
  - If provided, client SHOULD verify before processing
- `uploaded-at`: ISO 8601 timestamp (optional)
  - For audit trails and cache management

**Hashes**: Required for HTTP sources to ensure integrity verification and tamper detection.

### Path Source

Used for local development assets on the filesystem.

```toml
[[assets]]
name = "local-skill"
version = "0.1.0-dev"
type = "skill"

[assets.source-path]
path = "/absolute/path/to/skill.zip"
```

**Fields**:

- `path`: Path to asset zip file (required)
  - Absolute paths: Used as-is
  - `~` prefix: Resolved to user home directory
  - Relative paths: Resolved from lock file directory

**Hashes**: Not required for path sources (local filesystem is trusted).

### Git Source

Used for assets stored in version control systems as packaged zip files.

```toml
[[assets]]
name = "custom-mcp"
version = "0.5.0"
type = "mcp"

[assets.source-git]
url = "https://github.com/company/custom-mcp.git"
ref = "abc123def456789"
subdirectory = "dist"
```

**Fields**:

- `url`: Git repository URL (required)
  - Supports HTTPS and SSH URLs
  - Uses local git installation and credentials
- `ref`: Git reference to checkout (required)
  - In lock files, MUST be a full commit SHA (40 hex characters for SHA-1)
  - Ensures reproducibility across environments
  - Branch/tag names from requirements.txt are resolved to commit SHAs during lock generation
- `subdirectory`: Path within repository to find asset (optional)
  - Client looks for `.zip` files in this directory
  - If omitted, looks in repository root

**Hashes**: Not required for git sources. Git commit history provides integrity verification through the commit SHA.

**Caching**: Repositories are cloned to client cache directory. Subsequent syncs reuse cached repo with `git fetch` + `git checkout`.

### Git Directory Source

Used for assets that exist as directories within a Git repository (not packaged as zips). This is useful for referencing skills that live alongside application code in existing repositories.

```toml
[[assets]]
name = "api-helper"
version = "0.5.0"
type = "skill"

[assets.source-git-dir]
url = "https://github.com/company/backend.git"
ref = "abc123def456789"
path = "tools/skills/api-helper"
```

**Fields**:

- `url`: Git repository URL (required)
  - Supports HTTPS and SSH URLs
  - Uses local git installation and credentials
- `ref`: Git reference to checkout (required)
  - In lock files, MUST be a full commit SHA (40 hex characters for SHA-1)
  - Ensures reproducibility across environments
- `path`: Path to the asset directory within the repository (required)
  - Must contain `metadata.toml` at its root
  - Directory contents are treated as the unpacked asset

**Hashes**: Not required. Git commit SHA provides integrity verification.

**Use Cases**:

- Skills maintained alongside application code in monorepos
- Teams that prefer keeping assets in existing repositories rather than separate vaults
- Gradual migration from ad-hoc skills to managed assets

**Caching**: Repositories are cloned to client cache directory. Asset directory is read directly from the checkout.

## Dependencies

Dependencies are specified as a simple array of asset references:

```toml
[[assets]]
name = "database-mcp"
version = "2.0.0"
type = "mcp"

[assets.source-http]
url = "https://vault.example.com/assets/database-mcp/2.0.0/database-mcp-2.0.0.zip"
hashes = {sha256 = "cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce"}

dependencies = [
  {name = "sql-formatter", version = "1.5.0"},
  {name = "helper-agent"}  # Version omitted if unambiguous
]
```

**Dependency Resolution**:

- Dependencies reference other assets in the same lock file by name
- Versions are optional if unambiguous (only one asset with that name)
- Cross-type dependencies are supported (MCPs can depend on skills, etc.)
- All dependencies must be present in the lock file (no runtime resolution)

## Scope

Assets can be scoped to different contexts using the `[[assets.scopes]]` array.

### Global Scope (default)

Assets apply to all projects and repositories when no `[[assets.scopes]]` entries are specified.

```toml
[[assets]]
name = "global-skill"
version = "1.0.0"
type = "skill"

[assets.source-http]
url = "https://vault.example.com/assets/global-skill/1.0.0/global-skill-1.0.0.zip"
hashes = {sha256 = "..."}
# No scopes = global
```

Installation: `~/.claude/skills/global-skill/`

### Repository Scope

Assets apply to entire repositories when `paths` is omitted or empty.

```toml
[[assets]]
name = "repo-agent"
version = "2.0.0"
type = "agent"

[assets.source-http]
url = "https://vault.example.com/assets/repo-agent/2.0.0/repo-agent-2.0.0.zip"
hashes = {sha256 = "..."}

[[assets.scopes]]
repo = "https://github.com/company/backend"

[[assets.scopes]]
repo = "https://github.com/company/frontend"
```

Installation:
- `{backend-repo-root}/.claude/`
- `{frontend-repo-root}/.claude/`

### Path Scope

Assets apply to specific paths within repositories when `paths` array is specified.

```toml
[[assets]]
name = "api-helper"
version = "0.5.0"
type = "agent"

[assets.source-http]
url = "https://vault.example.com/assets/api-helper/0.5.0/api-helper-0.5.0.zip"
hashes = {sha256 = "..."}

[[assets.scopes]]
repo = "https://github.com/company/backend"
paths = ["services/api", "services/worker", "services/scheduler"]

[[assets.scopes]]
repo = "https://github.com/company/platform"
paths = ["modules/auth"]
```

Installation:
- `{backend-repo-root}/services/api/.claude/`
- `{backend-repo-root}/services/worker/.claude/`
- `{backend-repo-root}/services/scheduler/.claude/`
- `{platform-repo-root}/modules/auth/.claude/`

### Mixed Scope

Scope entries can mix repo-scoped and path-scoped installations.

```toml
[[assets]]
name = "mixed-helper"
version = "1.0.0"
type = "skill"

[assets.source-http]
url = "https://vault.example.com/assets/mixed-helper/1.0.0/mixed-helper-1.0.0.zip"
hashes = {sha256 = "..."}

[[assets.scopes]]
repo = "https://github.com/company/backend"
# No paths = entire backend repo

[[assets.scopes]]
repo = "https://github.com/company/platform"
paths = ["modules/auth", "modules/billing"]
# Specific paths in platform repo
```

Installation:
- `{backend-repo-root}/.claude/` (entire repo)
- `{platform-repo-root}/modules/auth/.claude/` (specific path)
- `{platform-repo-root}/modules/billing/.claude/` (specific path)

## Complete Example

```toml
lock-version = "1.0"
version = "a3f8d92b1c4e5f6a7b8c9d0e1f2a3b4c"
created-by = "sx-cli/0.1.0"

# Global HTTP asset with hashes (no scopes = global)
[[assets]]
name = "github-mcp"
version = "1.2.3"
type = "mcp"

[assets.source-http]
url = "https://vault.example.com/assets/github-mcp/1.2.3/github-mcp-1.2.3.zip"
hashes = {sha256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"}
size = 125678
uploaded-at = "2025-11-20T14:30:00Z"

# Asset installed at multiple paths within one repo
[[assets]]
name = "service-linter"
version = "3.1.0"
type = "skill"

[assets.source-http]
url = "https://vault.example.com/assets/service-linter/3.1.0/service-linter-3.1.0.zip"
hashes = {sha256 = "cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce"}

[[assets.scopes]]
repo = "https://github.com/company/backend"
paths = ["services/api", "services/worker", "services/scheduler"]

# Asset installed across multiple repos with mixed scoping
[[assets]]
name = "auth-helper"
version = "4.2.1"
type = "agent"

[assets.source-http]
url = "https://vault.example.com/assets/auth-helper/4.2.1/auth-helper-4.2.1.zip"
hashes = {sha256 = "b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6a7b8c9"}

[[assets.scopes]]
repo = "https://github.com/company/backend"
# No paths = entire repo

[[assets.scopes]]
repo = "https://github.com/company/platform"
paths = ["modules/auth", "modules/billing"]

# Git-based asset with path scoping
[[assets]]
name = "custom-agent"
version = "0.5.0"
type = "agent"

[assets.source-git]
url = "https://github.com/company/agents.git"
ref = "a1b2c3d4e5f6789012345678901234567890abcd"
subdirectory = "dist"

[[assets.scopes]]
repo = "https://github.com/company/backend"
paths = ["services/api"]

# Asset with dependencies
[[assets]]
name = "database-mcp"
version = "2.0.0"
type = "mcp"

[assets.source-http]
url = "https://vault.example.com/assets/database-mcp/2.0.0/database-mcp-2.0.0.zip"
hashes = {sha256 = "d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2"}

dependencies = [
  {name = "service-linter", version = "3.1.0"}
]

[[assets.scopes]]
repo = "https://github.com/company/backend"
paths = ["services/api"]

# Claude Code-specific skill across three repos
[[assets]]
name = "code-reviewer"
version = "3.0.0"
type = "skill"
clients = ["claude-code"]

[assets.source-http]
url = "https://vault.example.com/assets/code-reviewer/3.0.0/code-reviewer-3.0.0.zip"
hashes = {sha256 = "e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3"}

[[assets.scopes]]
repo = "https://github.com/company/backend"

[[assets.scopes]]
repo = "https://github.com/company/frontend"

[[assets.scopes]]
repo = "https://github.com/company/mobile-app"
```

## Installation Process

1. **Client Filtering**: Filter assets by client tool compatibility (if `clients` field specified)
2. **Scope Resolution**: Determine which assets apply to current context (global, repo, or path)
3. **Dependency Resolution**: Build dependency graph using topological sort
4. **Validation**: Validate lock file structure and asset metadata
5. **Download/Fetch**: Fetch assets using appropriate source fetcher (HTTP, path, git)
6. **Integrity Verification**: Verify hashes and sizes (if provided)
7. **Asset Validation**:
   - For HTTP sources: Metadata already fetched separately during lock generation
   - For path/git sources: Extract and validate metadata from inside the asset
   - Validate zip structure and required files match metadata declarations
8. **Installation**: Install assets in dependency order to appropriate locations
9. **Configuration**: Apply scope-specific configurations

**Metadata Access Pattern**:

- **Lock generation**: Fetch metadata separately (alongside URL) to avoid downloading full assets
- **Installation**: Metadata from inside asset is canonical source for validation
- **Offline/local**: Metadata inside asset ensures it travels with the asset

## Scope Precedence

When multiple scopes apply, configuration is merged with this precedence (highest to lowest):

1. Path-specific (`path`)
2. Repository-specific (`repo`)
3. Global (`global`)

## Version and Caching

### Lock File Format Version

The `lock-version` field indicates the format specification version. Tools should:

- Reject lock files with unknown major versions
- Support all minor versions within the same major version
- Use `created-by` for diagnostics only, not behavioral changes

### Lock File Instance Version

The `version` field is a hash/identifier for this specific lock file instance. Used for:

- HTTP caching with `If-None-Match` and `ETag` headers
- Determining if the lock file has changed since last fetch

The value should be a hash of the lock file contents or a unique identifier generated by the server.

### ETag Caching Flow

1. Client fetches lock file: `GET /api/skills/sx.lock` with `User-Agent: claude-code/1.5.0`
2. Server responds with lock file and `ETag: "a3f8d92b1c4e5f6a7b8c9d0e1f2a3b4c"`
3. On subsequent requests, client sends: `If-None-Match: "a3f8d92b1c4e5f6a7b8c9d0e1f2a3b4c"`
4. Server returns `304 Not Modified` if unchanged, or new lock file with new ETag if updated

## Reserved Fields

The following field names are reserved and must not be used for custom metadata:

- Any field defined in this specification
- Fields starting with underscore (`_`)

## Use Cases

### Use Case 1: Standalone Lock File

User creates `sx.lock` by hand, commits it to their repository:

```toml
lock-version = "1.0"
version = "local-dev"
created-by = "manual"

[[assets]]
name = "my-skill"
version = "0.1.0"
type = "skill"

[assets.source-path]
path = "./skills/my-skill.zip"

[[assets]]
name = "upstream-mcp"
version = "1.2.3"
type = "mcp"

[assets.source-git]
url = "https://github.com/company/mcps.git"
ref = "7f8a9b0c1d2e3f4567890abcdef123456789abcd"
```

Team members run `sx install` to install assets from local and git sources.

### Use Case 2: Server-Managed Lock File

User authenticates with vault server. On sync:

1. Client fetches lock file from vault server
2. Server generates lock file with HTTP sources and hashes
3. Client installs assets based on server configuration
4. Server can update assets centrally by changing lock file

## Security Considerations

### Hash Verification

- HTTP sources SHOULD provide hashes for production deployments
- Clients MUST verify hashes if provided in source configuration
- Path and git sources do not require hashes (different trust models)

### Git Source Security

- Uses local git installation and credentials (SSH keys, credential helpers)
- Repository authenticity verified by git's security model
- Code review and git commit history provide integrity

### Path Source Security

- Trusts local filesystem
- Appropriate for development environments
- Should not be used in lock files distributed to untrusted users

## Future Enhancements

Potential additions for future versions:

- Additional source types (S3, OCI registries)
- Artifact signing and GPG verification
- Mirror/fallback sources
- Bandwidth optimization (compression, delta updates)
- Registry metadata section (for audit/SBOM context)
