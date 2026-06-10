package commands

import "testing"

// Behavioral guard-rail stubs for the interactive scope editor.
//
// These pin the end-to-end flows currently verified by hand so a future change
// can't silently break them. They are intentionally NOT implemented yet —
// each is a t.Skip with a precise description of the menu path to drive and the
// scopeResult / persisted targets to assert. Fill them in (drop the Skip) when
// wiring real coverage.
//
// Harness notes (see add_scope_keep_test.go for a worked example):
//   - Drive promptForRepositoriesWithUI with a bufio.Reader of "N\n" numbered
//     choices and io.Discard output (forces the non-TTY numbered menu).
//   - Top menu order (installed asset): 1=Keep, 2=Global, then any vault
//     ScopeOptionProvider extras (e.g. Sleuth's "Just for me"), then Edit
//     scopes, Remove.
//   - Editor menu order (Sleuth/identity vault): 1=Add repo, 2=Add path,
//     3=Add team, 4=Add user, 5=Add bot, 6=Remove, 7=Remove all, 8=Done.
//   - Flows that touch team/user/bot need a fake vault implementing
//     installSetter (plural SetAssetInstallations) so allowIdentity is true,
//     and CurrentActor so the "me" alias resolves. "me" resolution and the
//     append/replace decision land in updateLockFile -> bulkSetInstallTargets,
//     so asserting the *sent* targets means driving persistence with a fake
//     setter that records its (targets, appendMode) arguments.

// TestScopeEditor_KeepUnchanged pins: picking "Keep current settings" makes no
// changes — scopeResult.Inherit==true, no Targets, Remove==false — so the
// downstream no-mutation branch runs and server-side identity scopes the client
// never saw are preserved. (Overlaps TestPromptKeepCurrentSettingsSetsInherit;
// keep here as the editor-level guard.)
func TestScopeEditor_KeepUnchanged(t *testing.T) {
	t.Skip("TODO(SD-10170): assert choosing 'Keep current settings' returns Inherit=true with no Targets and triggers no installation mutation")
}

// TestScopeEditor_MakeGloballyAvailable pins: picking "Make it available
// globally" returns scopeResult.Scopes==[] (empty, non-nil => global), no
// Targets, Append==false, and persists as an org-wide install.
func TestScopeEditor_MakeGloballyAvailable(t *testing.T) {
	t.Skip("TODO(SD-10170): assert choosing 'Make it available globally' yields an empty (global) scope set and an org-wide install")
}

// TestScopeEditor_AddMeAndAnotherUser pins: Edit scopes -> Add a user scope ->
// enter "me, other@example.com" -> Done. The result must carry two user targets
// ("me" plus the other email); on persistence "me" resolves to CurrentActor's
// email (with the "Assigned to you (<email>)" line), the other user is sent
// verbatim, and duplicates are de-duped. Needs a fake installSetter+CurrentActor
// vault; assert the targets handed to SetAssetInstallations.
func TestScopeEditor_AddMeAndAnotherUser(t *testing.T) {
	t.Skip("TODO(SD-10170): assert 'me, other@example.com' produces two user targets, with 'me' resolved to the caller's email on persistence")
}

// TestScopeEditor_MeThenOrgWide_Replace pins: starting from a "me" user scope,
// the user then makes the asset org-wide and chooses Replace. Replace (not
// append) must win, and org-wide is exclusive — so the final installation is a
// single global/org install and the prior "me" scope is dropped, not merged.
// Needs a fake installSetter; assert appendMode==false and that the sent set is
// org-wide only.
func TestScopeEditor_MeThenOrgWide_Replace(t *testing.T) {
	t.Skip("TODO(SD-10170): assert adding 'me' then going org-wide with Replace drops the user scope and persists a single org-wide install (appendMode=false)")
}

// --- Top-menu options ---

// TestScopeEditor_RemoveFromInstallation pins: picking "Remove from
// installation" returns scopeResult.Remove==true (uninstall, asset stays in the
// vault) and not Inherit.
func TestScopeEditor_RemoveFromInstallation(t *testing.T) {
	t.Skip("TODO(SD-10170): assert choosing 'Remove from installation' returns Remove=true and uninstalls (asset remains in vault)")
}

// TestScopeEditor_JustForMe_Personal pins the Sleuth-only convenience option:
// picking "Just for me" returns scopeResult.ScopeEntity=="personal" (no Targets,
// no Scopes), which persists via the server's personalOnly path. Needs a fake
// vault implementing ScopeOptionProvider.
func TestScopeEditor_JustForMe_Personal(t *testing.T) {
	t.Skip("TODO(SD-10170): assert the vault's 'Just for me' option returns ScopeEntity=personal and uses the personalOnly install path")
}

// TestScopeEditor_CancelTopMenu pins the cancel contract: ESC at the top menu
// returns Inherit==true when the asset is installed (currentRepos != nil) and
// Remove==true when it isn't (currentRepos == nil) — never a silent overwrite.
func TestScopeEditor_CancelTopMenu(t *testing.T) {
	t.Skip("TODO(SD-10170): assert ESC at the top menu yields Inherit when installed and Remove when not installed")
}

// --- Editor actions ---

// TestScopeEditor_AddRepoScope_NonIdentityPath pins the common repo/path edit:
// Edit scopes -> Add a repo scope -> Done with no identity scopes routes through
// the lock-file SetInstallations path (NOT the bulk setter), since
// hasIdentityScope is false and Append is false.
func TestScopeEditor_AddRepoScope_NonIdentityPath(t *testing.T) {
	t.Skip("TODO(SD-10170): assert a repo-only edit persists via SetInstallations (lock-file path), not the bulk installer")
}

// TestScopeEditor_UserList_CreatesMultipleTargets pins comma-separated input:
// "Add a user scope" with "a@x.com, b@x.com" yields two distinct user targets.
// (Same behavior expected for team and bot lists.)
func TestScopeEditor_UserList_CreatesMultipleTargets(t *testing.T) {
	t.Skip("TODO(SD-10170): assert a comma-separated user/team/bot list creates one target per entry")
}

// TestScopeEditor_RemoveAllScopes_NoAppendPrompt pins: "Remove all scopes" then
// Done clears the working set and is treated as an unambiguous replace — the
// append/replace prompt is skipped and the persisted set is empty (global).
func TestScopeEditor_RemoveAllScopes_NoAppendPrompt(t *testing.T) {
	t.Skip("TODO(SD-10170): assert 'Remove all scopes' clears the set, skips the append/replace prompt, and persists as a replace")
}

// TestScopeEditor_CancelInEditor_KeepsOriginal pins: ESC inside the editor (or
// declining the final 'Continue with these changes?') returns the original
// scope set unchanged — added/removed entries in the working set are discarded.
func TestScopeEditor_CancelInEditor_KeepsOriginal(t *testing.T) {
	t.Skip("TODO(SD-10170): assert cancelling inside the editor discards working changes and returns the original scopes")
}

// --- "me" alias ---

// TestScopeEditor_MePrefill_EnterResolvesToCaller pins the prefill ergonomics:
// pressing Enter at the user-scope prompt (default "me") yields the "me" target,
// which resolves to CurrentActor's email on persistence.
func TestScopeEditor_MePrefill_EnterResolvesToCaller(t *testing.T) {
	t.Skip("TODO(SD-10170): assert pressing Enter at the user prompt uses the 'me' default and resolves to the caller's email")
}

// TestScopeEditor_MeAndOwnEmail_Deduped pins de-duplication: entering both "me"
// and the caller's own email collapses to a single user target after "me"
// resolves (no duplicate install).
func TestScopeEditor_MeAndOwnEmail_Deduped(t *testing.T) {
	t.Skip("TODO(SD-10170): assert 'me' plus the caller's own email de-dupes to one user target on persistence")
}

// --- Append / replace ---

// TestScopeEditor_Append_SendsAppendModeTrue pins: choosing Append after editing
// calls SetAssetInstallations with appendMode==true and the editor's targets
// only (the server does the merge — no client-side union).
func TestScopeEditor_Append_SendsAppendModeTrue(t *testing.T) {
	t.Skip("TODO(SD-10170): assert choosing Append calls SetAssetInstallations with appendMode=true and only the newly chosen targets")
}

// TestScopeEditor_Replace_SendsAppendModeFalse pins: choosing Replace calls
// SetAssetInstallations with appendMode==false (existing server scopes are
// overwritten).
func TestScopeEditor_Replace_SendsAppendModeFalse(t *testing.T) {
	t.Skip("TODO(SD-10170): assert choosing Replace calls SetAssetInstallations with appendMode=false")
}

// --- Vault gating & error reporting ---

// TestScopeEditor_NonSleuthVault_NoIdentityNoAppend pins the file/git gating: a
// vault that does NOT implement installSetter offers only repo/path actions in
// the editor, never shows team/user/bot, never asks append/replace, and
// persists via SetInstallations.
func TestScopeEditor_NonSleuthVault_NoIdentityNoAppend(t *testing.T) {
	t.Skip("TODO(SD-10170): assert a non-Sleuth vault hides team/user/bot actions, skips the append/replace prompt, and uses SetInstallations")
}

// TestScopeEditor_UnresolvedTargets_Reported pins best-effort resolution: when a
// team/user/bot can't be resolved on the server, it's reported to the user
// (the "⚠ Could not resolve …" line) while the resolvable targets still apply.
func TestScopeEditor_UnresolvedTargets_Reported(t *testing.T) {
	t.Skip("TODO(SD-10170): assert unresolved team/user/bot targets are reported while the resolvable ones are still applied")
}
