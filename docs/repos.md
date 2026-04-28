# Repo- and path-scoped installs

`--repo` and `--path` install an asset only for callers working inside
a specific git repository (or a subpath of one). Use repo scope when an
asset is meaningful to one project's codebase but not the whole
organization. Use path scope when a monorepo holds multiple projects
that each want different assets.

For the manifest schema, see
[manifest-spec.md](manifest-spec.md#assetsscopes--install-targets). For
the broader scope picker, see [scoping.md](scoping.md).

## Repo scope

```bash
sx install my-skill --repo git@github.com:acme/myapp.git
```

The asset installs to `<repo>/.claude/` (and the equivalent for any
other configured client) for any caller running `sx install` inside a
clone of the named repo. Outside that repo, the asset is invisible.

Repo URLs are normalized before comparison — `git@github.com:acme/x`,
`https://github.com/acme/x`, and `https://github.com/acme/x.git` all
resolve to the same scope row.

> **Vault vs project:** the repo URL in `--repo` is your *project's*
> git remote — the codebase where you want the asset installed — not
> your sx vault, where assets are stored.

You can install the same asset to multiple repos by passing the flag
more than once (or by re-running `sx install` with each new repo —
each call appends a scope row, deduped against existing entries):

```bash
sx install my-skill --repo git@github.com:acme/app-a.git
sx install my-skill --repo git@github.com:acme/app-b.git
```

## Path scope

Path scope narrows a repo install to one or more subdirectories.
Useful for monorepos where `services/api` wants Python tooling and
`services/web` wants TypeScript tooling.

```bash
sx install my-skill --path "git@github.com:acme/myapp.git#services/api"
```

The format is `<repo-url>#<path>`. Multiple paths in the same repo go
in a comma-separated list:

```bash
sx install my-skill --path "git@github.com:acme/myapp.git#services/api,services/web"
```

The asset installs to `<repo>/<path>/.claude/` for any caller running
`sx install` from inside one of the listed paths. From elsewhere in
the repo it's invisible.

## Setting scope at `sx add` time

`sx add` configures scope when an asset is first published — equivalent
to running `sx install` with the same flag right after:

```bash
# scope a new asset to one repo
sx add my-skill --scope-repo git@github.com:acme/myapp.git

# scope a new asset to specific paths
sx add my-skill --scope-repo "git@github.com:acme/myapp.git#services/api"
```

Already-added assets can have their scope changed by re-running
`sx install <name>` with new scope flags.

## Resolution model

When a caller runs `sx install` inside a working tree, sx reads the
git remote, normalizes the URL, and matches it against every asset's
scope rows:

* `kind = "repo"` matches when the normalized URLs are equal.
* `kind = "path"` matches when the URLs are equal **and** the caller's
  current working directory is inside one of the listed paths.

Outside any matching repo, the asset is filtered out — it doesn't
appear in the resolved lock file and isn't written to any client
directory. `sx install --dry-run` shows you the resolved set without
touching the filesystem.
