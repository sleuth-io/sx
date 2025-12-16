# SX Vault Specification

## Overview

This specification defines the structure and protocol for asset vaults. A vault stores versioned assets and their metadata in a standardized layout that supports both filesystem and HTTP access patterns.

Inspired by Maven Central, PyPI, and Go module proxies, this design prioritizes:

- Simple filesystem layout that works locally or over HTTP
- Efficient version discovery without downloading assets
- Metadata alongside assets (Maven pattern)
- Minimal protocol overhead

## Vault Types

Vaults can be accessed via:

- **Filesystem**: Local or network-mounted directories
- **HTTP**: Web servers serving static files or dynamic APIs

Both use identical directory structure.

## Directory Structure

```
{vault-base}/
  {asset-name}/
    list.txt                              # Version listing
    {version}/
      metadata.toml                       # Asset metadata
      {asset-name}-{version}.zip          # Asset package
```

### Example: Filesystem Vault

```
./assets/
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

### Example: HTTP Vault

```
https://vault.example.com/assets/
  github-mcp/
    list.txt
    1.2.3/
      metadata.toml
      github-mcp-1.2.3.zip
```

## Version Listing (`list.txt`)

This file is **required** for each asset in a vault. It enables efficient version discovery - clients can check available versions with a single small file fetch instead of directory traversal or downloading full assets.

**Who creates it**: Vault maintainers or publishing tools (like `sx publish`) must create and update this file when publishing new versions.

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

- Downloading full assets
- Directory traversal (filesystem)
- HTML parsing (HTTP)
- Complex API calls

### Updates

When adding a new version:

1. Create version directory: `{name}/{version}/`
2. Add asset and metadata files
3. Append version to `list.txt`

## Metadata Location

Following Maven conventions, metadata is stored **alongside** the asset at:

```
{name}/{version}/metadata.toml
```

This enables:

- Fetching metadata without downloading asset
- Validation before download
- Dependency resolution without full asset access

See `metadata-spec.md` for metadata file format.

## URL/Path Construction

### Version Listing

**Filesystem**: `{base}/{name}/list.txt`
**HTTP**: `GET {base}/{name}/list.txt` or `GET {base}/{name}/list`

### Metadata

**Filesystem**: `{base}/{name}/{version}/metadata.toml`
**HTTP**: `GET {base}/{name}/{version}/metadata.toml`

### Asset

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
   [[assets]]
   name = "github-mcp"
   version = "2.0.0"
   type = "mcp"

   [assets.source-http]  # or source-path
   url = "{base}/github-mcp/2.0.0/github-mcp-2.0.0.zip"
   hashes = {sha256 = "..."}
   ```

## Configuration

Default vault configured in `config.toml`:

### Filesystem Vault

```toml
[default-source]
type = "path"
base = "./assets"
```

Or relative to requirements file:

```toml
[default-source]
type = "path"
base = "."  # Same directory as sx.txt
```

### HTTP Vault

```toml
[default-source]
type = "http"
base = "https://vault.example.com/assets"
```

### Multiple Vaults

Future enhancement - search multiple vaults in order:

```toml
[[vaults]]
name = "local"
type = "path"
base = "./local-assets"

[[vaults]]
name = "company"
type = "http"
base = "https://vault.company.com"

[[vaults]]
name = "public"
type = "http"
base = "https://vault.sx.io"
```

## HTTP Vault Requirements

### Static File Serving

HTTP vaults can be simple static file servers (nginx, S3, GitHub Pages):

```nginx
location /assets/ {
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
Cache-Control: public, max-age=31536000, immutable  # For assets and metadata
ETag: "{hash}"  # For cache validation
```

### Content Types

```
*.toml -> application/toml
*.zip  -> application/zip
list.txt -> text/plain; charset=utf-8
```

## Filesystem Vault Requirements

### Permissions

```bash
# Vault structure
chmod 755 {base}/{name}/
chmod 644 {base}/{name}/list.txt
chmod 755 {base}/{name}/{version}/
chmod 644 {base}/{name}/{version}/*
```

### Case Sensitivity

- Asset names are case-sensitive
- Use consistent casing (recommend lowercase)

## Security Considerations

### Integrity Verification

- HTTP sources: Lock file includes hashes (required)
- Filesystem sources: Hashes optional (filesystem trusted)

### Authentication

**HTTP vaults** can require authentication:

```bash
# Bearer token
Authorization: Bearer {token}

# Basic auth
Authorization: Basic {base64(user:pass)}
```

**Filesystem vaults**: Use OS-level permissions

### HTTPS

Production HTTP vaults SHOULD use HTTPS to prevent MITM attacks.

## Asset Packaging

Assets must be packaged as zip files with `metadata.toml` at the root:

```bash
# Directory structure
my-asset/
  metadata.toml
  SKILL.md
  (other files)

# Create zip
cd my-asset
zip -r ../my-asset-1.0.0.zip .
```

The zip contains the asset contents with metadata.toml at the root.

## Publishing Assets

### Manual Publishing (Filesystem)

```bash
# Create asset structure
ASSET_NAME="my-skill"
VERSION="1.0.0"
VAULT_BASE="./assets"

# Create directories
mkdir -p "$VAULT_BASE/$ASSET_NAME/$VERSION"

# Copy asset and metadata
cp my-skill-1.0.0.zip "$VAULT_BASE/$ASSET_NAME/$VERSION/"
cp metadata.toml "$VAULT_BASE/$ASSET_NAME/$VERSION/"

# Update version list
echo "$VERSION" >> "$VAULT_BASE/$ASSET_NAME/list.txt"

# Sort and deduplicate (optional)
sort -u "$VAULT_BASE/$ASSET_NAME/list.txt" -o "$VAULT_BASE/$ASSET_NAME/list.txt"
```

## Vault Migration

### From HTTP to Filesystem

```bash
# Clone entire vault
wget -r -np -nH --cut-dirs=2 https://vault.example.com/assets/
```

### From Filesystem to HTTP

```bash
# Sync to web server
rsync -avz ./assets/ user@server:/var/www/assets/
```

## Examples

### Example 1: Local Development

**Directory**: `./my-project/assets/`

**sx.txt**:

```txt
my-skill>=1.0.0
```

**config.toml**:

```toml
[default-source]
type = "path"
base = "./assets"
```

**Vault structure**:

```
./assets/
  my-skill/
    list.txt          # Contains: "1.0.0\n1.1.0"
    1.0.0/
      metadata.toml
      my-skill-1.0.0.zip
    1.1.0/
      metadata.toml
      my-skill-1.1.0.zip
```

### Example 2: Company Internal Vault

**sx.txt**:

```txt
github-mcp==1.2.3
code-reviewer>=3.0.0
```

**config.toml**:

```toml
[default-source]
type = "http"
base = "https://vault.company.com/assets"
```

**HTTP requests**:

```
GET https://vault.company.com/assets/github-mcp/list.txt
GET https://vault.company.com/assets/github-mcp/1.2.3/metadata.toml
GET https://vault.company.com/assets/github-mcp/1.2.3/github-mcp-1.2.3.zip

GET https://vault.company.com/assets/code-reviewer/list.txt
GET https://vault.company.com/assets/code-reviewer/3.0.0/metadata.toml
GET https://vault.company.com/assets/code-reviewer/3.0.0/code-reviewer-3.0.0.zip
```

### Example 3: Mixed Sources

**sx.txt**:

```txt
# From default vault
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
base = "./assets"
```

Only `github-mcp` uses the default vault. Others use explicit sources.

## Future Enhancements

### Signed Assets

```
{name}/{version}/{name}-{version}.zip.sig  # GPG signature
```

### Compressed Version Lists

```
{name}/list.txt.gz  # For vaults with many versions
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
base = "https://vault.sx.io"
mirrors = [
  "https://mirror1.example.com",
  "https://mirror2.example.com"
]
```

## Comparison with Other Systems

| System   | Version List         | Metadata Location   | Protocol   |
| -------- | -------------------- | ------------------- | ---------- |
| **Maven**| `maven-metadata.xml` | Per asset           | XML + HTTP |
| **PyPI** | JSON API endpoint    | Per version         | JSON API   |
| **npm**  | Packument            | All in one          | JSON API   |
| **Go**   | `@v/list`            | `@v/{version}.info` | Plain text |
| **NuGet**| `index.json`         | Per version         | JSON       |
| **SX**   | `list.txt`           | Per version         | Plain text |

SX follows Go's simplicity with Maven's metadata-alongside-asset pattern.
