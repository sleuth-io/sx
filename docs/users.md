# User-scoped installs

`--user <email>` installs an asset for a single human caller. The asset
becomes "global" for that user — visible from every directory they run
`sx install` in, on any client they have configured. It's the right
choice for personal tooling that other team members don't need to see.

For the manifest schema, see
[manifest-spec.md](manifest-spec.md#assetsscopes--install-targets). For
the broader scope picker, see [scoping.md](scoping.md).

## Installing for yourself

```bash
sx install my-skill --user alice@acme.com
```

The Sleuth-only **"personal"** option offered in the interactive
`sx add` TUI is the same scope — included as a convenience so you
don't have to type your own address.

## Self-only restriction

`--user <email>` is allowed only when `<email>` matches the caller's
git identity (`git config user.email`, normalized for case). This stops
someone with vault write access from silently flipping an asset to
"global" in a teammate's resolved lock file.

The check runs inside the vault mutation transaction, after the flock
+ manifest reload — not just in the CLI — so it cannot be bypassed by
racing two processes.

## Identity model

User-scope resolution uses the email from `git config user.email`,
cached per vault root for the CLI's lifetime.

If git is unconfigured, sx synthesizes a read-only identity of the form
`local:$USER@$HOST`. The `local:` prefix guarantees it cannot collide
with a real email — any mutation using this identity is rejected with
a clear "set git config user.email" message. Read-only commands
(`sx install --dry-run`, `sx stats`, `sx audit`) still work.

## Bots are not users

Bot identities (`SX_BOT=<name>`) deliberately do **not** match
user-scoped installs — bots are not human users, so a `kind = "user"`
scope is silently filtered out of a bot's resolved lock file. Use
`--bot <name>` instead. See [bots.md](bots.md).
