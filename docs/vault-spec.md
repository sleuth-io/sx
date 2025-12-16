# Sleuth Artifact Repository Specification

## Overview

This specification defines the structure and protocol for artifact repositories. A repository stores versioned artifacts and their metadata in a standardized layout that supports both filesystem and HTTP access patterns.

Inspired by Maven Central, PyPI, and Go module proxies, this design prioritizes:

- Simple filesystem layout that works locally or over HTTP
- Efficient version discovery without downloading artifacts
- Metadata alongside artifacts (Maven pattern)
- Minimal protocol overhead

## Repository Types

Repositories can be accessed via:

- **Filesystem**: Local or network-mounted directories
- **HTTP**: Web servers serving static files or dynamic APIs

Both use identical directory structure.

## Directory Structure

```
{repository-base}/
  {artifact-name}/
    list.txt                              # Version listing
    {version}/
      metadata.toml                       # Artifact metadata
      {artifact-name}-{version}.zip       # Artifact package
```

### Example: Filesystem Repository

```
./artifacts/
  github-mcp/
    list.txt
    1.2.3/
      metadata.toml
      github-mcp-1.2.3.zip
    1.2.4/
      metadata.toml
      github-mcp-1.2.4.zip
  code-reviewer/
    list.txt
    3.0.0/
      metadata.toml
      code-reviewer-3.0.0.zip
```

### Example: HTTP Repository

```
https://registry.example.com/artifacts/
  github-mcp/
    list.txt
    1.2.3/
      metadata.toml
      github-mcp-1.2.3.zip
```

## Version Listing (`list.txt`)

### Format

Plain text file with one version per line:

```
1.0.0
1.2.3
2.0.0
```

### Rules

- One semantic version per line
- Versions in any order (client will sort/filter)
- No blank lines or comments
- UTF-8 encoding
- Unix line endings (`\n`) preferred but `\r\n` accepted

### Purpose

Enables efficient version discovery without:

- Downloading full artifacts
- Directory traversal (filesystem)
- HTML parsing (HTTP)
- Complex API calls

### Updates

When adding a new version:

1. Create version directory: `{name}/{version}/`
2. Add artifact and metadata files
3. Append version to `list.txt`

## Metadata Location

Following Maven conventions, metadata is stored **alongside** the artifact at:

```
{name}/{version}/metadata.toml
```

This enables:

- Fetching metadata without downloading artifact
- Validation before download
- Dependency resolution without full artifact access

See `metadata-spec.md` for metadata file format.

## URL/Path Construction

### Version Listing

**Filesystem**: `{base}/{name}/list.txt`
**HTTP**: `GET {base}/{name}/list.txt` or `GET {base}/{name}/list`

### Metadata

**Filesystem**: `{base}/{name}/{version}/metadata.toml`
**HTTP**: `GET {base}/{name}/{version}/metadata.toml`

### Artifact

**Filesystem**: `{base}/{name}/{version}/{name}-{version}.zip`
**HTTP**: `GET {base}/{name}/{version}/{name}-{version}.zip`

## Resolution Protocol

When resolving `github-mcp>=1.2.3`:

1. **List versions**: Read/fetch `{base}/github-mcp/list.txt`

   ```
   1.2.2
   1.2.3
   1.2.4
   2.0.0
   ```

2. **Filter versions**: Parse and filter by specifier `>=1.2.3`

   ```
   1.2.3, 1.2.4, 2.0.0
   ```

3. **Select best**: Choose highest compatible version: `2.0.0`

4. **Fetch metadata**: Read/fetch `{base}/github-mcp/2.0.0/metadata.toml`

5. **Resolve dependencies**: Parse dependencies from metadata, recurse

6. **Generate lock entry**:

   ```toml
   [[artifacts]]
   name = "github-mcp"
   version = "2.0.0"
   type = "mcp"

   [artifacts.source-http]  # or source-path
   url = "{base}/github-mcp/2.0.0/github-mcp-2.0.0.zip"
   hashes = {sha256 = "..."}
   ```

## Configuration

Default repository configured in `config.toml`:

### Filesystem Repository

```toml
[default-source]
type = "path"
base = "./artifacts"
```

Or relative to requirements file:

```toml
[default-source]
type = "path"
base = "."  # Same directory as sleuth.txt
```

### HTTP Repository

```toml
[default-source]
type = "http"
base = "https://registry.example.com/artifacts"
```

### Multiple Repositories

Future enhancement - search multiple repositories in order:

```toml
[[repositories]]
name = "local"
type = "path"
base = "./local-artifacts"

[[repositories]]
name = "company"
type = "http"
base = "https://artifacts.company.com"

[[repositories]]
name = "public"
type = "http"
base = "https://registry.sleuth.io"
```

## HTTP Repository Requirements

### Static File Serving

HTTP repositories can be simple static file servers (nginx, S3, GitHub Pages):

```nginx
location /artifacts/ {
    root /var/www;
    autoindex off;  # Don't expose directory listings
}
```

### CORS Headers (if browser access needed)

```
Access-Control-Allow-Origin: *
Access-Control-Allow-Methods: GET, HEAD
```

### Caching Headers

```
Cache-Control: public, max-age=31536000, immutable  # For artifacts and metadata
ETag: "{hash}"  # For cache validation
```

### Content Types

```
*.toml -> application/toml
*.zip  -> application/zip
list.txt -> text/plain; charset=utf-8
```

## Filesystem Repository Requirements

### Permissions

```bash
# Repository structure
chmod 755 {base}/{name}/
chmod 644 {base}/{name}/list.txt
chmod 755 {base}/{name}/{version}/
chmod 644 {base}/{name}/{version}/*
```

### Case Sensitivity

- Artifact names are case-sensitive
- Use consistent casing (recommend lowercase)

## Security Considerations

### Integrity Verification

- HTTP sources: Lock file includes hashes (required)
- Filesystem sources: Hashes optional (filesystem trusted)

### Authentication

**HTTP repositories** can require authentication:

```bash
# Bearer token
Authorization: Bearer {token}

# Basic auth
Authorization: Basic {base64(user:pass)}
```

**Filesystem repositories**: Use OS-level permissions

### HTTPS

Production HTTP repositories SHOULD use HTTPS to prevent MITM attacks.

## Artifact Packaging

Artifacts must be packaged as zip files with `metadata.toml` at the root:

```bash
# Directory structure
my-artifact/
  metadata.toml
  SKILL.md
  (other files)

# Create zip
cd my-artifact
zip -r ../my-artifact-1.0.0.zip .
```

The zip contains the artifact contents with metadata.toml at the root.

## Publishing Artifacts

### Manual Publishing (Filesystem)

```bash
# Create artifact structure
ARTIFACT_NAME="my-skill"
VERSION="1.0.0"
REPO_BASE="./artifacts"

# Create directories
mkdir -p "$REPO_BASE/$ARTIFACT_NAME/$VERSION"

# Copy artifact and metadata
cp my-skill-1.0.0.zip "$REPO_BASE/$ARTIFACT_NAME/$VERSION/"
cp metadata.toml "$REPO_BASE/$ARTIFACT_NAME/$VERSION/"

# Update version list
echo "$VERSION" >> "$REPO_BASE/$ARTIFACT_NAME/list.txt"

# Sort and deduplicate (optional)
sort -u "$REPO_BASE/$ARTIFACT_NAME/list.txt" -o "$REPO_BASE/$ARTIFACT_NAME/list.txt"
```

### Automated Publishing

Future `sleuth publish` command:

```bash
sleuth publish ./my-skill.zip --repository=local
```

## Repository Migration

### From HTTP to Filesystem

```bash
# Clone entire repository
wget -r -np -nH --cut-dirs=2 https://registry.example.com/artifacts/
```

### From Filesystem to HTTP

```bash
# Sync to web server
rsync -avz ./artifacts/ user@server:/var/www/artifacts/
```

## Examples

### Example 1: Local Development

**Directory**: `./my-project/artifacts/`

**sleuth.txt**:

```txt
my-skill>=1.0.0
```

**config.toml**:

```toml
[default-source]
type = "path"
base = "./artifacts"
```

**Repository structure**:

```
./artifacts/
  my-skill/
    list.txt          # Contains: "1.0.0\n1.1.0"
    1.0.0/
      metadata.toml
      my-skill-1.0.0.zip
    1.1.0/
      metadata.toml
      my-skill-1.1.0.zip
```

### Example 2: Company Internal Registry

**sleuth.txt**:

```txt
github-mcp==1.2.3
code-reviewer>=3.0.0
```

**config.toml**:

```toml
[default-source]
type = "http"
base = "https://artifacts.company.com/registry"
```

**HTTP requests**:

```
GET https://artifacts.company.com/registry/github-mcp/list.txt
GET https://artifacts.company.com/registry/github-mcp/1.2.3/metadata.toml
GET https://artifacts.company.com/registry/github-mcp/1.2.3/github-mcp-1.2.3.zip

GET https://artifacts.company.com/registry/code-reviewer/list.txt
GET https://artifacts.company.com/registry/code-reviewer/3.0.0/metadata.toml
GET https://artifacts.company.com/registry/code-reviewer/3.0.0/code-reviewer-3.0.0.zip
```

### Example 3: Mixed Sources

**sleuth.txt**:

```txt
# From default repository
github-mcp==1.2.3

# Explicit HTTP source
https://cdn.example.com/skills/formatter-2.0.0.zip

# Local development
./local-skills/debug-skill.zip

# Git source
git+https://github.com/company/tools.git@main#name=api-helper
```

**config.toml**:

```toml
[default-source]
type = "path"
base = "./artifacts"
```

Only `github-mcp` uses the default repository. Others use explicit sources.

## Future Enhancements

### Signed Artifacts

```
{name}/{version}/{name}-{version}.zip.sig  # GPG signature
```

### Compressed Version Lists

```
{name}/list.txt.gz  # For repositories with many versions
```

### Search API

```
GET {base}/search?q=github
```

### Statistics

```
{name}/{version}/stats.json  # Download counts, etc.
```

### Mirrors

```toml
[default-source]
type = "http"
base = "https://registry.sleuth.io"
mirrors = [
  "https://mirror1.example.com",
  "https://mirror2.example.com"
]
```

## Comparison with Other Systems

| System     | Version List         | Metadata Location   | Protocol   |
| ---------- | -------------------- | ------------------- | ---------- |
| **Maven**  | `maven-metadata.xml` | Per artifact        | XML + HTTP |
| **PyPI**   | JSON API endpoint    | Per version         | JSON API   |
| **npm**    | Packument            | All in one          | JSON API   |
| **Go**     | `@v/list`            | `@v/{version}.info` | Plain text |
| **NuGet**  | `index.json`         | Per version         | JSON       |
| **Sleuth** | `list.txt`           | Per version         | Plain text |

Sleuth follows Go's simplicity with Maven's metadata-alongside-artifact pattern.
