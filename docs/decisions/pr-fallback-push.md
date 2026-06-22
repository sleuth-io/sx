# PR-fallback push — load-bearing decisions (SD-10194)

When RBAC blocks a direct publish, `sx add` pushes to a branch and opens a PR. A few things here are load-bearing — don't break them.

## Branch names are unique

PR branch names are `sx/add-<skill>-<version>-<hex>`, where `<skill>` is the asset's name and `<version>` is the asset's version (not the author), both sanitized to be git-ref-safe. The random `<hex>` suffix makes every attempt unique, which prevents collisions between users or retries and keeps the push fast-forward. If you ever make branch names deterministic again you'll be tempted to add `--force` back; don't — detect the existing branch instead.

## No `--force` on push

In the first attempt the code used `--force` when pushing, so that the user could modify the PR. We've removed this logic, because force-pushing is destructive, other people might have already pushed to that branch. But, maybe we will revert back to using `--force` in a future improvement. Let's first see, if this feature is used.

## Always clean up the clone

PR mode spans three steps (Start → AddAsset → Finish), and only Finish restores the clone to its base branch. So `addViaPullRequest` defers `AbortPRBranch` to cover every early exit — without it, a failure between Start and Finish leaves the shared cached clone stuck on the PR branch and corrupts later reads. Keep that defer.

## Base branch comes from the remote, not local HEAD

`StartPRBranch` resolves the PR's base via `GetDefaultBranch` (the `origin/HEAD` symbolic ref) and checks it out before branching — not from the clone's current HEAD. The clone is a long-lived cache that can be left on some other branch, and trusting its HEAD would open the PR against the wrong base.

## `gh pr create` pins `--repo`

The clone lives under a hidden cache dir a snap-confined `gh` can't read; without `--repo`, cwd detection fails. There's a regression test in `gitrepo_pr_test.go` — don't drop the flag.

## Possible future improvement: edit the PR instead of opening a new one

The random suffix means a same-version retry spawns a fresh branch/PR rather than updating the existing one. If that becomes annoying, drop the suffix (keep `sx/add-<skill>-<version>`) so the branch is reused. The catch: `StartPRBranch` resets the branch off base each run, so updating the remote branch requires a force-push — use `--force-with-lease` (fetch the remote branch first so the lease is real) so a concurrent edit to the same skill@version fails loudly instead of being clobbered, and split out a force variant so the empty-repo push stays non-forcing. Deferred for now.

## Known unfixed edges

Flagged in PR #167 review and deferred: the file lock is released between the three phases (very rare race — only bites if two mutating runs hit the same clone cache at once; the cheap guard is to re-check `GetCurrentBranch == prBranch` before pushing), and the compare-URL fallback is meaningless for non-GitHub remotes. (`--yes` auto-opening the PR is intended and now documented in rbac.md.)
