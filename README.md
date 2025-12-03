# Skills

A CLI tool for managing Sleuth skills - reusable units of AI agent behavior.

## Installation

```bash
go install github.com/sleuth-io/skills/cmd/skills@latest
```

Or build from source:

```bash
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
make build          # Build binary
make test           # Run tests
make format         # Format code
make lint           # Run linter
```

## Documentation

- [Repository Spec](docs/repository-spec.md)
- [Metadata Spec](docs/metadata-spec.md)
- [Requirements Spec](docs/requirements-spec.md)
- [Lock Spec](docs/lock-spec.md)

## License

See LICENSE file for details.
