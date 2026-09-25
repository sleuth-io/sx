# Contributing To sx

Pull requests are welcome. Keep changes focused, add or update tests for
behavior changes, and update docs when a command's behavior or setup changes.

## Development Setup

You need Go (see the version in `go.mod`) and `make`.

```bash
git clone https://github.com/sleuth-io/sx.git
cd sx
make init          # download dependencies
make build         # build to ./dist/sx
```

Run your build directly:

```bash
./dist/sx <command>
```

## Before Opening A Pull Request

```bash
make prepush       # format, lint, test, build
```

CI runs the same checks. If `make prepush` fails locally, it will fail on your
pull request.

Useful individual targets:

| Target | Description |
|---|---|
| `make build` | Build the binary to `./dist/sx` |
| `make test` | Run tests |
| `make lint` | Run linters |
| `make format` | Format code |
| `make dev` | Run with hot reload (requires air) |
| `make tidy` | Tidy `go.mod` |

`make help` lists everything.

## Pull Requests

- One logical change per pull request. Unrelated fixes are easier to review,
  and easier to revert, on their own.
- Explain what changed and why. If the reasoning is not obvious from the diff,
  it belongs in the description.
- Say how you verified the change. "Ran `make prepush`" is a fine answer; so is
  a command transcript showing the new behavior.
- Pull requests from forks skip the automated Claude review, which needs
  repository secrets. A maintainer will review those by hand.

### Commit Messages

- Keep the commit **subject line ≤ 72 characters**. A CI status
  (`pr-check/sleuth`) enforces this maximum subject length and blocks the pull
  request when a subject is longer. Put detail in the body, not the subject.

## Reporting Bugs

Open an issue with the version (`sx --version`), your OS, the command you ran,
what you expected, and what happened. Include the full error output when there
is one.

For anything security-related, do **not** open a public issue — see
[SECURITY.md](SECURITY.md).

## Code Of Conduct

Participation is covered by our [Code of Conduct](CODE_OF_CONDUCT.md).
