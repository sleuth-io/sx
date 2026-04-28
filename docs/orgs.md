# Org-scoped installs

`--org` installs an asset for **every** caller in the vault, regardless
of which repo they're working in or what teams they belong to. It's
the default scope for shared coding standards, common slash commands,
and any other asset everyone benefits from. The asset installs to the
caller's home `.claude/` (and the equivalent for any other configured
client) so it's available from every directory.

For the manifest schema, see
[manifest-spec.md](manifest-spec.md#assetsscopes--install-targets). For
the broader scope picker, see [scoping.md](scoping.md).

## Setting org scope

```bash
sx install my-skill --org
```

Org scope **clears every other scope row** on the asset — the asset
becomes globally visible, period. There's no "org plus team X" mode;
once an asset is org-scoped, the team/user/bot/repo scope rows are
removed from the manifest. (You can always switch back later by
re-running `sx install` with a narrower scope.)

`sx add` with no scope flag is the equivalent on a brand-new asset:

```bash
# org-wide is the default for sx add
sx add my-skill

# explicit form
sx add my-skill --scope-global
```

## Who sees an org-scoped asset

Every caller that runs `sx install` against the vault, including:

* human users with any git identity
* bots (`SX_BOT=<name>`) — bots see org-wide installs the same way
  human team members do (see [bots.md](bots.md))

Read-only callers without a configured git identity (the `local:` synthetic
identity) also see org-wide assets — `sx install --dry-run` works for
them. Write operations still require a real `git config user.email`.

## When to choose another scope

Org scope is the right default for things everyone wants. Reach for a
narrower scope when:

| Symptom | Scope to use |
|---------|--------------|
| Asset is only relevant to one project's codebase | [`--repo` or `--path`](repos.md) |
| Asset is for a specific group of people | [`--team`](teams.md) |
| Asset is for a single human, including yourself | [`--user`](users.md) |
| Asset is for an automated runner | [`--bot`](bots.md) |
