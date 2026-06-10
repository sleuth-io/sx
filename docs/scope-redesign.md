# Scope redesign — target

One consistent way to scope an asset, shared by `sx add` and `sx install`.

## Scope kinds (the only six)

| Kind | Meaning |
|------|---------|
| org  | global — available everywhere, no restrictions (exclusive) |
| repo | everyone working in a repository |
| path | specific subpaths within a repo |
| team | every member of a team |
| user | a single user (email) |
| bot  | a bot identity |

## Non-interactive flags (same on both commands)

```
--org
--repo <url>            (repeatable)
--path <url#p1,p2>      (repeatable)
--team <name>           (repeatable)
--user <email>          (repeatable)
--bot <name>            (repeatable)
--replace-scope         replace the whole set instead of appending
```

- Default = **ADD**: the named targets are appended to whatever scope already exists, so repeating `--repo` across calls grows the set.
- `--replace-scope` = **REPLACE**: the named flags become the asset's complete scope set; anything unnamed is dropped.
- `--org` is **exclusive**: can't combine with other targets; it always replaces the whole set with a single global target.
- Both commands resolve identical flags to identical scope. Errors (bad `--path`, `--org`+other) surface up front, before any vault write.

## Interactive editor

Shown when scope flags aren't given. One cohesive panel — title states the asset
and its current scope; options follow directly under it.

```
Scope for <asset> — currently <global | not installed | repo X, team Y, …>

→ Keep current settings        No changes
  Make it available globally    Org-wide, no restrictions
  Just for me                   Install only for your account
  Edit scopes                   Add/remove repos, paths, teams, users, bots
  Remove from installation      Uninstall (keeps it in vault)
```

- `Keep` always offered; esc = no changes (never an implicit uninstall).
- Editing down to an empty set = global.
- Picking a value should be as low-friction as possible (e.g. prefill the
  caller's own email for `user`; ideally pick teams from a list rather than typing).

## Behavior / correctness requirements

- Team targets require team-admin; check **before** any write so a failure can't
  leave a half-applied REPLACE.
- The "currently …" line must reflect the **real** current scope. Note the trap:
  an asset can be reachable by slug (REST / lockfile) yet absent from the vault's
  GraphQL `assets` connection — don't report such an asset as "not installed" when
  the user actually has it, and don't offer scope edits that will fail on apply.
- Decide explicitly: does `sx install` keep a scope-setting mode at all, or does
  scope-setting live only on `sx add`? (`install` = "pull to my machine" is the
  cleaner mental model.)

## Done means (all of it, or it's not done)

The point of this code is to BE the scope behavior of the commands. It is
worthless sitting on the side. So "done" requires:

- `sx add` uses it for every path (new asset, existing asset, identical
  content, rule, vault-only) — flags and interactive.
- `sx install` uses it (or scope-setting is deliberately removed from install
  and lives only on `sx add` — decide, don't leave both half-wired).
- The old `--scope-global/--scope-repo/--scope` flags and the old repo/path-only
  prompt are GONE (or fully forwarded to the new path). No two parallel ways to
  scope.
- Every test that exercised scope works against the new flow — interactive
  menus, integration tests, all of it. Green `make prepush`. No "leave the
  tests alone."
- It actually works end to end against the real skills.io vault, not just unit
  tests.
```
