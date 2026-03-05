# SX Requirements File Specification

## Overview

This specification defines `sx.txt`, a simple requirements format for declaring AI client assets before resolution into a lock file. Inspired by pip's `requirements.txt`, it prioritizes simplicity and human editability.

## File Naming

Requirements files must be named:

- `sx.txt` (default)
- `sx-<name>.txt` (named variants)

## Format

Plain text file with one requirement per line:

```txt
# Comments start with #
# Blank lines are ignored

# Registry artifacts with version specifiers
github-mcp==1.2.3
code-reviewer>=3.0.0
database-mcp~=2.0.0
helper-agent>=1.0,<2.0
awesome-skill

# Git sources
git+https://github.com/user/repo.git@main#name=artifact-name
git+https://github.com/user/repo.git@v1.2.3#name=artifact-name&path=subdir

# Local paths
./relative/path/artifact.zip
~/home/relative/artifact.zip
/absolute/path/artifact.zip

# HTTP sources
https://example.com/artifacts/skill.zip
```

## Requirement Types

### Vault Assets

Format: `<name>[<version-spec>]`

```txt
# Exact version
github-mcp==1.2.3

# Minimum version
code-reviewer>=3.0.0

# Compatible version (>= 2.0.0, < 2.1.0)
database-mcp~=2.0.0

# Range
helper-agent>=1.0,<2.0

# Latest version (no specifier)
awesome-skill
```

**Version Specifiers**:

- `==X.Y.Z` - Exact version
- `>=X.Y.Z` - Minimum version
- `>X.Y.Z` - Greater than
- `<=X.Y.Z` - Maximum version
- `<X.Y.Z` - Less than
- `~=X.Y.Z` - Compatible release (>= X.Y.Z, < X.(Y+1).0)
- `X.Y.Z` - Exact version (same as ==)
- Multiple specifiers separated by comma: `>=1.0,<2.0`

**Resolution**:

- Uses default vault configured in `config.toml` (see `vault-spec.md`)
- Queries vault for available versions
- Filters versions matching specifier
- Resolves dependencies recursively
- Selects highest compatible version
- Generates lock file entry with concrete source (HTTP or path)

### Git Sources

Format: `git+<url>@<ref>#name=<artifact-name>[&path=<subdirectory>]`

```txt
# Branch reference
git+https://github.com/user/repo.git@main#name=custom-agent

# Tag reference
git+https://github.com/user/repo.git@v1.2.3#name=my-mcp

# Commit SHA
git+https://github.com/user/repo.git@abc123def456#name=pinned-skill

# With subdirectory
git+https://github.com/user/monorepo.git@main#name=api-agent&path=packages/agents
```

**Components**:

- `git+` prefix (required)
- URL: Repository URL (HTTPS or SSH)
- `@<ref>`: Git reference - branch, tag, or commit SHA
- `#name=<name>`: Asset name (required)
- `&path=<subdir>`: Subdirectory within repo (optional)

**Resolution**:

- Branch/tag: Resolved to commit SHA via `git ls-remote`
- Commit SHA: Used as-is
- Client clones repo and extracts asset from `path` (or root)

### Local Paths

Format: `<path>`

```txt
# Relative to current directory
./skills/my-skill.zip

# Relative to home directory
~/dev/artifacts/my-agent.zip

# Absolute path
/var/artifacts/production-mcp.zip
```

**Resolution**:

- Used as-is (no version resolution)
- Must point to valid `.zip` file
- Version extracted from zip metadata or generated from file mtime

### skills.sh Sources

Format: `skills.sh:<owner>/<repo>` or `skills.sh:<owner>/<repo>/<skill-name>`

```txt
# All skills from a public skills.sh repository
skills.sh:vercel-labs/agent-skills

# A specific skill from the repository
skills.sh:vercel-labs/agent-skills/find-skills
```

**Components**:

- `skills.sh:` prefix (required)
- `<owner>/<repo>`: GitHub owner and repository name (required)
- `/<skill-name>`: Specific skill subdirectory within the repo (optional)

**Resolution**:

- Resolves to the latest commit SHA on the default branch (`HEAD`) for reproducible installs
- When `<skill-name>` is specified, the lock entry uses subdirectory `skills/<skill-name>`
- When only `<owner>/<repo>` is specified, the entire repository is used
- Stored as a `SourceGit` lock entry so `sx update` re-resolves to the latest commit SHA
- Asset name: `<skill-name>` when specified, otherwise `<repo>` name

### HTTP Sources

Format: `<url>`

```txt
https://example.com/artifacts/skill-1.2.3.zip
https://cdn.company.com/mcps/custom.zip
```

**Resolution**:

- Downloaded directly from URL
- Version extracted from zip metadata or HTTP `Last-Modified` header
- No dependency resolution (unless metadata found in zip)

## Version Detection

When version is not specified in requirement (git, path, HTTP sources), it's determined by:

### From Asset Metadata

Client extracts zip and checks for version in:

1. `package.json` - read `version` field
2. `metadata.yml` - read `version` field
3. `metadata.toml` - read `version` field

Example `package.json`:

```json
{
  "name": "my-skill",
  "version": "1.2.3"
}
```

Example `metadata.yml`:

```yaml
name: my-skill
version: 1.2.3
type: skill
```

### From Source Timestamps

If no metadata found, generate version using format `0.0.0+YYYYMMDD` based on:

- **Local paths**: File system modification time
- **HTTP sources**: `Last-Modified` header, fallback to `Date` header, fallback to current date
- **Git sources**: Commit timestamp

## Comments and Whitespace

```txt
# Full-line comments start with #

github-mcp==1.2.3  # Inline comments not supported

# Blank lines are ignored


# Indentation is ignored
  code-reviewer>=3.0.0
```

## Dependencies

Requirements file specifies **top-level** assets only. Dependencies are:

- Declared in asset metadata (for vault assets)
- Declared in `package.json` or `metadata.yml` (for git/path/HTTP assets)
- Resolved recursively during lock file generation

## Lock File Generation

Command: `sx lock`

Process:

1. Parse `sx.txt`
2. For each requirement:
   - Vault assets: Query vault (see `vault-spec.md`), select best match
   - Git: Resolve refs to commit SHAs, extract asset
   - Path/HTTP: Download, extract version from metadata or timestamp
3. Resolve dependencies recursively
4. Detect conflicts (multiple assets require incompatible versions)
5. Generate `sx.lock` with:
   - Exact versions for all assets
   - Commit SHAs for git sources
   - Hashes for HTTP sources
   - Full dependency graph

See `lock-spec.md` for lock file format and `vault-spec.md` for vault structure.

## Examples

### Simple Project

`sx.txt`:

```txt
# Core MCPs
github-mcp>=1.2.0
database-mcp~=2.0.0

# Skills
code-reviewer>=3.0.0
```

Generates `sx.lock` with resolved versions, dependencies, and hashes.

### Mixed Sources

`sx.txt`:

```txt
# From vault
github-mcp==1.2.3

# Public skill from skills.sh registry
skills.sh:vercel-labs/agent-skills/find-skills

# All skills from a skills.sh repository
skills.sh:org/shared-skills

# From git (internal tool)
git+https://github.com/company/agents.git@main#name=api-helper&path=dist

# Local development
./local-skills/debug-skill.zip

# External URL
https://cdn.example.com/skills/formatter.zip
```

### Development Workflow

1. Create `sx.txt` with high-level requirements
2. Run `sx lock` to generate `sx.lock`
3. Commit both files to version control
4. Team members run `sx install` to install from lock file
5. Update `sx.txt` when adding/changing assets
6. Run `sx lock` to regenerate lock file

## Comparison with Lock File

| Aspect           | Requirements (sx.txt)     | Lock File (sx.lock)       |
| ---------------- | ------------------------- | ------------------------- |
| **Purpose**      | Declare desired assets    | Pin exact versions        |
| **Format**       | Plain text, line-based    | TOML, structured          |
| **Versions**     | Ranges (>=, ~=, etc.)     | Exact versions only       |
| **Git refs**     | Branches, tags, commits   | Commit SHAs only          |
| **Dependencies** | Top-level only            | Full dependency graph     |
| **Hashes**       | Not included              | Required for HTTP sources |
| **Editability**  | Hand-edited by users      | Machine-generated         |
| **Scope**        | Implicit (file location)  | Explicit per asset        |

## Edge Cases

### Conflicting Versions

If multiple assets require incompatible versions:

```txt
asset-a  # depends on helper>=2.0
asset-b  # depends on helper<2.0
```

Resolution fails with clear error message listing conflict.

### Missing Git Ref

```txt
git+https://github.com/user/repo.git@nonexistent#name=foo
```

Resolution fails: "Git ref 'nonexistent' not found in repository"

### Invalid Path

```txt
./nonexistent/asset.zip
```

Resolution fails: "File not found: ./nonexistent/asset.zip"

### Circular Dependencies

If asset A depends on B, and B depends on A, resolution fails with circular dependency error.

## Future Enhancements

Potential additions:

- Environment markers: `github-mcp>=1.2.0 ; client=="claude-code"`
- Constraints files: `-c constraints.txt`
- Include other requirements: `-r base-requirements.txt`
- Editable installs: `-e ./local-dev-artifact`
