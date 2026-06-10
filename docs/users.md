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
sx install my-skill --user me            # 'me' = your own account
sx install my-skill --user alice@acme.com
```

The magic value **`me`** resolves to your own account (via the same
identity lookup described below), so you never have to type your own
address. It works wherever a user email is accepted — the `--user`
flag and the interactive `sx add` scope editor, where the "Add a user
scope" prompt is prefilled with `me` (just press Enter) and also
accepts a comma-separated list of emails.

Who you may target depends on the vault:

- **File-backed vaults (Path/Git):** the self-only restriction below
  applies, so `me` (your own address) is the only value `--user`
  accepts.
- **Sleuth vault:** you can also scope to other org users by email,
  subject to your `INSTALL_SELF` permission on the asset — the server
  checks the permission but does not pin the target to the caller.

The Sleuth-only **"personal"** option offered in the interactive
`sx add` TUI is the same scope — another convenience for the same thing.

## Self-only restriction (file-backed vaults)

On Path/Git vaults, `--user <email>` is allowed only when `<email>`
matches the caller's git identity (`git config user.email`, normalized
for case). This stops someone with vault write access from silently
flipping an asset to "global" in a teammate's resolved lock file.

The check runs inside the vault mutation transaction, after the flock
+ manifest reload — not just in the CLI — so it cannot be bypassed by
racing two processes.

The Sleuth vault does not apply this restriction; it gates user-scoped
installs on the server-side `INSTALL_SELF` permission instead (see
above), which permits targeting other org users.

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
