# Permissions — github vault

This controls **who can set a skill's scope** (where it installs) and **who can edit it**. Scope is governed by the vault's **org-admins**; editing is governed by a skill's **team scope**. Org-admins can always do both. This is the model for the **github (git/path) vault**, enforced client-side; the Sleuth (skills.new) vault enforces the same model server-side.

## Setting scope — the two states

Scope rules depend on a single thing: **does the vault have org-admins?**

**One scope is always gated, regardless of state:** locking a skill to **team X** always requires being an **admin of team X** (or an org-admin) — teams own skills, so claiming a skill for a team is a team-management decision even in an ungoverned vault. Everything below is about the *other* scopes.

### State 1 — No org-admins (ungoverned, the default)

**Anyone can set any non-team scope.** A fresh vault starts here. Setting the first org-admin flips it to State 2 — and while the list is empty, *anyone* can do that, so the command warns you first.

### State 2 — Org-admins exist (governed)

| Scope                   | Who can set or remove it                |
|-------------------------|-----------------------------------------|
| org / repo / path / bot | **org-admins** only                     |
| team X                  | an **admin of team X**, or an org-admin (always, even ungoverned) |
| just-me (your own user) | **anyone**                              |

Org-admins can always act; they never take away a team admin's control over their own team's scope.

## Editing a skill

Who can edit/publish a skill depends only on whether it is scoped to a team — not on the org-admins state:

| Skill                | Who can edit / publish it                  |
|----------------------|--------------------------------------------|
| scoped to a team     | a **member** of that team, or an org-admin |
| not scoped to a team | **anyone**                                 |

A skill scoped to several teams is editable by a member of any of them. Org-admins can always edit anything.

These edit rules hold **regardless of governance state** — even in an ungoverned vault (no org-admins), a team-scoped skill is editable only by that team's members, and with no org-admins there is no one to override it. That's safe because scoping a skill to a team is itself always team-admin gated (above), so a skill can't be locked away from you by someone who doesn't run the team.

## Q & A — common flows

**How do I turn on governance?**
`sx org admin add a@x.com b@y.com` — add one or more org-admins. While the list is empty anyone can do this; it locks scope control to those people, so you're asked to confirm.

**How do I unset all org-admins (go back to ungoverned)?**
Remove every org-admin with `sx org admin remove <email>`. Only a current org-admin can do this. Once the list is empty, the vault is ungoverned again — anyone can set any scope.

**Who can scope a skill to team X?**
An admin of team X, or an org-admin — **always**, even in an ungoverned vault. Locking a skill to a team is a team-management action (teams own skills), so a non-admin can never do it.

**If I scope a skill to a team, who actually gets it installed, and where?**
Every **member** of the team — but *where* depends on the team's repositories. If the team owns repos, members get it **in those repos** (so a member only sees it when working in one of them). If the team owns **no** repositories, members get it **globally** (everywhere). Non-members never get it. So a team scope is "these people, in these repos" — or, with no repos, just "these people, everywhere."

**Why can't I set an org-wide / repo scope?**
The vault has org-admins and you're not one. Ask an org-admin to do it, or to add you.

**Who can edit a skill?**
If it's scoped to a team, only members of that team (plus org-admins, who can always edit anything). If it isn't scoped to any team, anyone can.

**A skill is scoped to a team *and* to specific users — who can edit it?**
The team scope still rules: only a member of (one of) those teams, or an org-admin. Being named in the user scope does **not** grant edit rights — any team scope means team members only.
