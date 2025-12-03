# Skills

A CLI tool for managing Sleuth skills - reusable units of AI agent behavior.

## Prerequisites

Go 1.22.2 or later is required. Install using [gvm](https://github.com/moovweb/gvm):

```bash
# Install gvm
bash < <(curl -s -S -L https://raw.githubusercontent.com/moovweb/gvm/master/binscripts/gvm-installer)

# Install Go (use go1.4 as bootstrap if needed)
gvm install go1.4 -B
gvm use go1.4
export GOROOT_BOOTSTRAP=$GOROOT
gvm install go1.23.4
gvm use go1.23.4 --default
```

## Installation

**Quick install (downloads pre-built binary):**

```bash
curl -fsSL https://raw.githubusercontent.com/sleuth-io/skills/main/install.sh | bash
```

**Or install from source:**

```bash
go install github.com/sleuth-io/skills/cmd/skills@latest
```

**Or build from source:**

```bash
make init      # First time setup (install tools, download deps)
make build
make install
```

## Usage

```bash
skills init                   # Initialize configuration
skills add <artifact>         # Add a local zip or directory
skills lock                   # Generate lock file from requirements
skills install                # Install artifacts from lock file
```

## Development

```bash
make prepush        # Run before pushing (format, lint, test, build)
make postpull       # Run after pulling (download dependencies)
make build          # Build binary
make test           # Run tests
```

## Documentation

- [Repository Spec](docs/repository-spec.md)
- [Metadata Spec](docs/metadata-spec.md)
- [Requirements Spec](docs/requirements-spec.md)
- [Lock Spec](docs/lock-spec.md)

## License

See LICENSE file for details.
