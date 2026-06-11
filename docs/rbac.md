# Permissions — github vault

This controls **who can set a skill's scope** (where it installs) and **who can edit it**. Scope is governed by the vault's **org-admins**; editing is governed by a skill's **team scope**. Org-admins can always do both.

## Setting scope — the two states

Scope rules depend on a single thing: **does the vault have org-admins?**

### State 1 — No org-admins (ungoverned, the default)

**Anyone can set any scope.** A fresh vault starts here. Setting the first org-admin flips it to State 2 — and while the list is empty, *anyone* can do that, so the command warns you first.

### State 2 — Org-admins exist (governed)

| Scope                   | Who can set or remove it                |
|-------------------------|-----------------------------------------|
| org / repo / path / bot | **org-admins** only                     |
| team X                  | an **admin of team X**, or an org-admin |
| just-me (your own user) | **anyone**                              |

Org-admins can always act; they never take away a team admin's control over their own team's scope.

## Editing a skill

Who can edit/publish a skill depends only on whether it is scoped to a team — not on the org-admins state:

| Skill                | Who can edit / publish it                  |
|----------------------|--------------------------------------------|
| scoped to a team     | a **member** of that team, or an org-admin |
| not scoped to a team | **anyone**                                 |

A skill scoped to several teams is editable by a member of any of them. Org-admins can always edit anything.

## Q & A — common flows

**How do I turn on governance?**
`sx org admin add a@x.com b@y.com` — add one or more org-admins. While the list is empty anyone can do this; it locks scope control to those people, so you're asked to confirm.

**How do I unset all org-admins (go back to ungoverned)?**
Remove every org-admin with `sx org admin remove <email>`. Only a current org-admin can do this. Once the list is empty, the vault is ungoverned again — anyone can set any scope.

**Who can scope a skill to team X?**
An admin of team X, or an org-admin. (If the vault has no org-admins, anyone can.)

**Why can't I set an org-wide / repo scope?**
The vault has org-admins and you're not one. Ask an org-admin to do it, or to add you.

**Who can edit a skill?**
If it's scoped to a team, only members of that team (plus org-admins, who can always edit anything). If it isn't scoped to any team, anyone can.
