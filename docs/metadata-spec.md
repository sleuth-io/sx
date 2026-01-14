# SX Asset Metadata Specification

## Overview

This specification defines `metadata.toml`, a standardized format for declaring metadata about AI client assets (MCPs, skills, agents, commands, hooks). The format provides a single source of truth for asset information, dependencies, and configuration.

## Metadata Location

Metadata files are stored in **two locations**:

1. **Alongside the asset** - In vaults, at `{name}/{version}/metadata.toml`
   - For efficient access without downloading the full asset
   - Used for: Version detection, dependency resolution, validation before download
   - See `vault-spec.md` for vault structure

2. **Inside the asset** - At the root of the zip file
   - Travels with the asset, ensures canonical metadata is always available
   - Used for: Installation-time validation, offline scenarios

This dual-location approach provides both **performance** and **portability**.

## Design Philosophy

1. **Explicit over implicit** - All file references and configuration declared in metadata
2. **Minimal required fields** - Only name, version, and type are required
3. **TOML format** - Human-readable, comment-friendly, modern standard
4. **Type-driven** - Asset type determines required configuration sections
5. **Single source of truth** - All configuration in metadata.toml, no separate config files needed

## File Naming

Metadata files must be named:

- `metadata.toml` (only valid name)

## Core Structure

### Top-Level Required Fields

```toml
[asset]
name = "asset-name"          # Required; normalized name
version = "1.2.3"            # Required; semantic version
type = "mcp"                 # Required; asset type
```

### Metadata Version (Optional)

```toml
metadata-version = "1.0"     # Optional; metadata format version
```

**Metadata Version**:

- `metadata-version` is an optional top-level field (not inside `[artifact]`)
- If omitted, tools assume the latest version they support
- Current version: `"1.0"`
- Format: `"MAJOR.MINOR"`
- Tools should reject metadata files with unknown major versions
- Tools should support all minor versions within the same major version
- Recommended to include for forward compatibility

### Top-Level Optional Fields

```toml
[asset]
name = "asset-name"
version = "1.2.3"
type = "mcp"

# Optional metadata
description = "Brief description of the asset"
license = "MIT"              # SPDX license identifier
authors = ["Alice Smith <alice@example.com>", "Bob Jones <bob@example.com>"]
keywords = ["keyword1", "keyword2", "keyword3"]

# Optional links
homepage = "https://example.com/project"
repository = "https://github.com/user/repo"
documentation = "https://docs.example.com"
readme = "README.md"         # Path to readme file in package
```

## Asset Types

- `skill`: AI skill with prompt file
- `command`: Slash command with prompt file
- `agent`: AI agent with prompt file
- `hook`: Event hook with script or prompt file
- `mcp`: Packaged MCP server (includes server code)
- `mcp-remote`: Remote MCP configuration (no server code, just connection config)
- `claude-code-plugin`: Claude Code plugin bundle (contains commands, skills, agents, etc.)

## Type-Specific Configuration

Each asset type requires a corresponding section with specific fields.

### Skills (`type = "skill"`)

**Required Section**: `[skill]`

**Required Fields**:

- `prompt-file`: Path to the skill prompt markdown file

**Optional Fields**:

- `triggers`: Array of trigger phrases
- `requires`: Array of required tools/commands
- `supported-languages`: Array of programming languages

```toml
[asset]
name = "code-reviewer"
version = "3.0.0"
type = "skill"
description = "AI code review skill"

[skill]
prompt-file = "SKILL.md"
triggers = ["review", "code quality", "check code"]
requires = ["git"]
supported-languages = ["python", "javascript", "rust", "go"]
```

**Package Structure**:

```
code-reviewer/
  metadata.toml
  SKILL.md
  (other optional files)
```

### Commands (`type = "command"`)

**Required Section**: `[command]`

**Required Fields**:

- `prompt-file`: Path to the command prompt markdown file

**Optional Fields**:

- `aliases`: Array of alternative command names
- `requires-auth`: Boolean indicating if authentication is required
- `dangerous`: Boolean indicating if command performs destructive operations

```toml
[asset]
name = "deploy"
version = "1.0.0"
type = "command"
description = "Deploy application to environments"

[command]
prompt-file = "COMMAND.md"
aliases = ["deployment", "ship"]
requires-auth = true
dangerous = true
```

**Package Structure**:

```
deploy/
  metadata.toml
  COMMAND.md
  (other optional files)
```

### Agents (`type = "agent"`)

**Required Section**: `[agent]`

**Required Fields**:

- `prompt-file`: Path to the agent prompt markdown file

**Optional Fields**:

- `triggers`: Array of trigger phrases
- `requires`: Array of required tools/commands

```toml
[asset]
name = "api-helper"
version = "0.5.0"
type = "agent"
description = "Agent for API development and testing"

[agent]
prompt-file = "AGENT.md"
triggers = ["api", "rest", "endpoint"]
requires = ["curl", "jq"]
```

**Package Structure**:

```
api-helper/
  metadata.toml
  AGENT.md
  (other optional files)
```

### Hooks (`type = "hook"`)

**Required Section**: `[hook]`

**Required Fields**:

- `event`: Hook event name (e.g., "pre-commit", "post-commit", "pre-push")
- `script-file`: Path to the hook script or prompt file

**Optional Fields**:

- `async`: Boolean indicating if hook runs asynchronously (default: false)
- `fail-on-error`: Boolean indicating if hook failure should block the event (default: true)
- `timeout`: Timeout in seconds

**Hook Types**:

- **AI-based hooks**: Use `.md` file with prompt for AI to execute
- **Script-based hooks**: Use `.sh`, `.py`, `.js`, or other executable scripts

```toml
[asset]
name = "pre-commit-linter"
version = "2.0.0"
type = "hook"
description = "Pre-commit hook for linting"

[hook]
event = "pre-commit"
script-file = "pre-commit.sh"
fail-on-error = true
timeout = 60
```

**Package Structure**:

```
pre-commit-linter/
  metadata.toml
  pre-commit.sh
  lib/
    helpers.sh
  (other optional files)
```

### MCP Servers (`type = "mcp"`)

**Required Section**: `[mcp]`

**Required Fields**:

- `command`: Command to run the MCP server
- `args`: Array of command arguments

**Optional Fields**:

- `env`: Map of environment variables
- `timeout`: Timeout in milliseconds
- `capabilities`: Array of MCP capabilities

**Important**: All MCP configuration is in metadata.toml. No separate JSON config file is needed.

```toml
[asset]
name = "database-mcp"
version = "2.0.0"
type = "mcp"
description = "Database operations MCP server"

dependencies = [
    "sql-formatter~=1.5.0",
]

[mcp]
command = "node"
args = ["dist/index.js"]
env = {
  DB_HOST = "${DB_HOST}",
  DB_PORT = "5432",
  LOG_LEVEL = "info"
}
timeout = 30000
capabilities = ["query", "schema", "migration"]
```

**Package Structure**:

```
database-mcp/
  metadata.toml
  package.json
  dist/
    index.js
    lib/
      db.js
      query.js
  node_modules/
  (other server files)
```

### MCP Remote (`type = "mcp-remote"`)

**Required Section**: `[mcp]`

**Required Fields**:

- `command`: Command to connect to the remote MCP server
- `args`: Array of command arguments

**Optional Fields**:

- `env`: Map of environment variables
- `timeout`: Timeout in milliseconds

**Important**: MCP Remote assets contain ONLY metadata.toml. No server code is included - the configuration points to an external server (hosted service, npm package, etc.).

```toml
[asset]
name = "hosted-github"
version = "1.0.0"
type = "mcp-remote"
description = "GitHub MCP hosted service"

[mcp]
command = "npx"
args = ["-y", "@modelcontextprotocol/server-github"]
env = {
  GITHUB_PERSONAL_ACCESS_TOKEN = "${GITHUB_PERSONAL_ACCESS_TOKEN}"
}
```

**Package Structure**:

```
hosted-github/
  metadata.toml
  (that's it!)
```

### Claude Code Plugin (`type = "claude-code-plugin"`)

**Required Section**: `[claude-code-plugin]`

**Optional Fields**:

- `manifest-file`: Path to the plugin manifest (default: `.claude-plugin/plugin.json`)
- `auto-enable`: Whether to automatically enable the plugin on install (default: true)
- `marketplace`: Name of the marketplace where the plugin is published
- `min-client-version`: Minimum required Claude Code version

**Important**: Claude Code plugins are bundles that can contain multiple sub-assets (commands, skills, agents, hooks, MCP servers). The plugin must include a `.claude-plugin/plugin.json` manifest file.

```toml
[asset]
name = "my-dev-plugin"
version = "1.0.0"
type = "claude-code-plugin"
description = "Development utilities plugin for Claude Code"

[claude-code-plugin]
manifest-file = ".claude-plugin/plugin.json"
auto-enable = true
min-client-version = "1.0.0"
```

**Package Structure**:

```
my-dev-plugin/
  metadata.toml
  .claude-plugin/
    plugin.json              # Required manifest
  commands/                  # Optional slash commands
    deploy.md
    test.md
  skills/                    # Optional skills
    code-review/
      SKILL.md
  agents/                    # Optional agents
  hooks/                     # Optional hooks
    hooks.json
  .mcp.json                  # Optional MCP server config
  README.md
```

**plugin.json format**:

```json
{
  "name": "my-dev-plugin",
  "description": "Development utilities plugin",
  "version": "1.0.0",
  "author": { "name": "Your Name" }
}
```

**Installation**:

- Plugins install to `~/.claude/plugins/{plugin-name}/` (global)
- When `auto-enable = true` (default), the plugin is automatically enabled in `settings.json`
- Claude Code handles sub-asset loading internally

**Note**: This asset type is only supported by Claude Code clients.

## Dependencies

Dependencies are specified as an array of dependency strings, following PEP 508 style:

```toml
dependencies = [
    "sql-formatter~=1.5.0",
    "helper-agent>=1.0.0",
    "git-helper>=2.0.0,<3.0.0",
]
```

### Dependency Resolution

- Dependencies reference assets that will be in the lock file
- All dependencies must be resolved during lock file generation
- Cross-type dependencies are supported (MCPs can depend on skills, etc.)
- Circular dependencies are detected and reported as errors

### Dependency String Format

Each dependency string follows the format: `name[version-specifiers]`

**Examples**:

```toml
dependencies = [
    "simple-artifact",              # No version constraint (latest)
    "minimum-version>=2.0.0",       # Minimum version
    "compatible~=1.5.0",            # Compatible release
    "version-range>=1.0.0,<2.0.0",  # Multiple constraints
]
```

### Version Specifiers

**Supported Operators**:

- `>=X.Y.Z` - Minimum version (inclusive)
- `~=X.Y.Z` or `~X.Y.Z` - Compatible release (>= X.Y.Z, < X.(Y+1).0)
- Multiple constraints separated by comma: `>=1.0.0,<2.0.0`

**Whitespace**: Optional around operators for readability:

```toml
dependencies = [
    "package >= 2.0.0, < 3.0.0",  # Spaces allowed
    "package>=2.0.0,<3.0.0",      # Or no spaces
]
```

**Future operators** (reserved for future versions):

- `==X.Y.Z` - Exact version match
- `!=X.Y.Z` - Exclude specific version
- `>X.Y.Z`, `<=X.Y.Z`, `<X.Y.Z` - Other comparison operators
- `===X.Y.Z` - Arbitrary equality

## Custom Metadata

For tool-specific or custom metadata, use the `[custom]` section:

```toml
[custom]
internal-id = "mcp-001"
team = "platform"
deployed-at = "2025-01-15T10:30:00Z"
complexity = "intermediate"
```

This section is ignored by the core SX tooling but available for custom tools and workflows.

## Validation Rules

### All Assets

- `[asset]` section required
- `name`, `version`, `type` fields required
- `version` must be valid semantic version (X.Y.Z)
- `type` must be one of: skill, command, agent, hook, mcp, mcp-remote, claude-code-plugin

### Type-Specific Validation

**skill, command, agent**:

- Must have corresponding section (`[skill]`, `[command]`, `[agent]`)
- Must have `prompt-file` field
- File specified in `prompt-file` must exist in package

**hook**:

- Must have `[hook]` section
- Must have `event` and `script-file` fields
- File specified in `script-file` must exist in package

**mcp**:

- Must have `[mcp]` section
- Must have `command` and `args` fields
- Package must include server code files

**mcp-remote**:

- Must have `[mcp]` section
- Must have `command` and `args` fields
- Package may contain only metadata.toml

**claude-code-plugin**:

- Must have `[claude-code-plugin]` section
- Package must include `.claude-plugin/plugin.json` manifest
- Plugin manifest must be valid JSON with name field

## Integration with Lock File

The lock file (`sx.lock`) references assets with their resolved metadata:

```toml
[[assets]]
name = "code-reviewer"
version = "3.0.0"
type = "skill"

[assets.source-http]
url = "https://vault.example.com/assets/code-reviewer/3.0.0/code-reviewer-3.0.0.zip"
hashes = {sha256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"}
```

When the client installs:

1. Downloads/fetches asset based on source
2. Extracts metadata.toml
3. Validates metadata against asset type rules
4. Reads type-specific configuration
5. Locates required files (prompt-file, script-file, etc.)
6. Installs to appropriate location based on scope

## Complete Examples

### Minimal Skill

```toml
[asset]
name = "hello-world"
version = "1.0.0"
type = "skill"

[skill]
prompt-file = "SKILL.md"
```

### Full-Featured MCP Server

```toml
[asset]
name = "database-mcp"
version = "2.0.0"
type = "mcp"
description = "Database operations MCP server with PostgreSQL and MySQL support"
license = "Apache-2.0"
authors = ["Database Team <db-team@company.com>"]
keywords = ["database", "sql", "postgres", "mysql", "mcp"]
homepage = "https://company.com/mcps/database"
repository = "https://github.com/company/database-mcp"
documentation = "https://docs.company.com/database-mcp"
readme = "README.md"

dependencies = [
    "sql-formatter~=1.5.0",
    "connection-pool>=3.0.0,<4.0.0",
]

[mcp]
command = "node"
args = ["dist/server.js"]
env = {
  DB_HOST = "${DB_HOST}",
  DB_PORT = "5432",
  DB_TIMEOUT = "30000",
  LOG_LEVEL = "info"
}
timeout = 30000
capabilities = ["query", "schema", "migration", "backup"]

[custom]
internal-id = "mcp-001"
team = "platform"
complexity = "intermediate"
```

### Command with Aliases

```toml
[asset]
name = "deploy"
version = "1.0.0"
type = "command"
description = "Deploy application to staging or production environments"
license = "MIT"
authors = ["DevOps Team <devops@company.com>"]
keywords = ["deploy", "devops", "ci-cd"]
repository = "https://github.com/company/deploy-command"

[command]
prompt-file = "COMMAND.md"
aliases = ["deployment", "ship"]
requires-auth = true
dangerous = true

[custom]
requires-vpn = true
approved-by = "security-team"
```

### AI-Based Hook

```toml
[asset]
name = "pre-commit-ai"
version = "1.0.0"
type = "hook"
description = "AI-powered pre-commit validation"
license = "MIT"
authors = ["AI Team <ai@company.com>"]

[hook]
event = "pre-commit"
script-file = "HOOK.md"
fail-on-error = true
timeout = 120

dependencies = [
    "linter-mcp>=1.0.0",
]
```

### MCP Remote

```toml
[asset]
name = "github-remote"
version = "1.0.0"
type = "mcp-remote"
description = "Connect to GitHub MCP via npx"
license = "MIT"
authors = ["GitHub Team <github@company.com>"]
keywords = ["github", "mcp", "remote"]
homepage = "https://github.com/modelcontextprotocol/servers"

[mcp]
command = "npx"
args = ["-y", "@modelcontextprotocol/server-github"]
env = {
  GITHUB_PERSONAL_ACCESS_TOKEN = "${GITHUB_PERSONAL_ACCESS_TOKEN}"
}
timeout = 30000
```

### Agent with Dependencies

```toml
[asset]
name = "api-helper"
version = "0.5.0"
type = "agent"
description = "Agent for API development, testing, and documentation"
license = "MIT"
authors = ["Dave Johnson <dave@company.com>"]
keywords = ["api", "rest", "testing", "swagger"]
repository = "https://github.com/company/api-helper"

dependencies = [
    "http-client>=2.0.0",
    "json-formatter>=1.0.0",
]

[agent]
prompt-file = "AGENT.md"
triggers = ["api", "rest", "endpoint", "swagger"]
requires = ["curl", "jq"]

[custom]
supported-protocols = ["rest", "graphql", "grpc"]
```

### Claude Code Plugin

```toml
[asset]
name = "devops-toolkit"
version = "2.0.0"
type = "claude-code-plugin"
description = "DevOps utilities plugin with deployment commands and monitoring skills"
license = "MIT"
authors = ["DevOps Team <devops@company.com>"]
keywords = ["devops", "deployment", "monitoring", "ci-cd"]
repository = "https://github.com/company/devops-toolkit"

[claude-code-plugin]
manifest-file = ".claude-plugin/plugin.json"
auto-enable = true
min-client-version = "1.0.0"

[custom]
internal-team = "platform"
requires-vpn = true
```

## Migration from Current System

For existing assets without metadata.toml:

1. **Create metadata.toml** in asset directory
2. **Extract metadata** from filename or existing config files
3. **Add required fields**: name, version, type
4. **Add type-specific section** with file references
5. **Optionally add** description, license, authors, etc.

Example migration for a skill:

**Before**:

```
code-reviewer.zip
  - skill.md
  - (no metadata file)
```

**After**:

```
code-reviewer/
  - metadata.toml
  - SKILL.md
```

```toml
[asset]
name = "code-reviewer"
version = "1.0.0"  # extracted from filename or generated
type = "skill"

[skill]
prompt-file = "SKILL.md"  # renamed from skill.md
```

## Reserved Fields

The following field names are reserved and must not be used for custom metadata in the `[asset]`, type-specific sections, or `dependencies` array:

- Any field defined in this specification
- Fields starting with underscore (`_`)

Custom metadata should go in the `[custom]` section.

## Future Enhancements

Potential additions for future versions:

- **Optional dependencies**: Feature-grouped dependencies that can be optionally installed
  ```toml
  [optional-dependencies]
  testing = ["test-framework>=1.0.0"]
  advanced = ["ai-helper>=2.0.0"]
  ```
- **Additional version operators**: `==`, `!=`, `>`, `<=`, `<`, `===` for more precise version constraints
- **Platform targeting**: `platforms = ["macos", "linux", "windows"]`
- **Client targeting**: `clients = ["claude-code", "gemini"]` (currently in lock file)
- **License files**: `license-files = ["LICENSE", "LICENSES/*"]` with glob support
- **Changelog tracking**: `changelog = "CHANGELOG.md"`
- **Artifact signing**: Digital signatures for verification
