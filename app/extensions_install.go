package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/logger"
	"github.com/sleuth-io/sx/internal/manifest"
	"github.com/sleuth-io/sx/internal/mgmt"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// Extension installs are scoped: "me" adds a personal user scope so the
// extension reaches only the installing user; "org" leaves it library-wide.
// The scope lives in the vault manifest — the same rows `sx add --user me`
// writes — so the Extensions screen, the CLI, and every teammate's app agree
// on who receives what.
const (
	// ExtensionScopeMe installs for the caller only.
	ExtensionScopeMe = "me"
	// ExtensionScopeOrg installs for the whole library.
	ExtensionScopeOrg = "org"
)

// extensionScopeAuditName maps an install scope to its audit-log name.
func extensionScopeAuditName(scope string) string {
	if scope == ExtensionScopeMe {
		return "personal"
	}
	return "library"
}

// ExtensionScope describes who receives an extension, for scope chips and
// the remove/share actions. Shared means the extension reaches the caller
// through library-wide or team sharing; Personal means the caller's own
// user scope is present. Label is short chip text ("" hides the chip).
type ExtensionScope struct {
	Shared   bool   `json:"shared"`
	Personal bool   `json:"personal"`
	Label    string `json:"label"`
}

// CanInstallForEveryone reports whether the caller may install an extension
// library-wide. Mirrors docs/rbac.md: an ungoverned vault (no org-admins)
// lets anyone set an org scope; a governed one restricts it to org-admins.
// Vaults that don't expose the admin list (skills.new enforces server-side)
// answer true and let the write be the gate.
func (a *App) CanInstallForEveryone() bool {
	v, err := a.currentVault()
	if err != nil {
		return false
	}
	lister, ok := v.(interface {
		ListOrgAdmins(ctx context.Context) ([]string, error)
	})
	if !ok {
		return true
	}
	admins, err := lister.ListOrgAdmins(a.ctx)
	if err != nil || len(admins) == 0 {
		return true
	}
	self := manifest.NormalizeEmail(strings.TrimSpace(a.GetVaultInfo().Identity))
	if self == "" {
		return false
	}
	for _, admin := range admins {
		if manifest.NormalizeEmail(admin) == self {
			return true
		}
	}
	return false
}

// installExtensionScoped publishes an already-validated extension folder and
// applies the requested scope. A fresh publish lands with no scopes (=
// library-wide), so a personal install adds the caller's user scope right
// after — and rolls the publish back if that fails, because a failed
// personal install must not strand the extension installed for everyone.
// Republishing an extension the vault already has is an update: its
// existing scopes are inherited and left alone, whatever scope was asked.
func (a *App) installExtensionScoped(dir, id, version, scope, source string) (string, error) {
	if scope != ExtensionScopeMe && scope != ExtensionScopeOrg {
		return "", fmt.Errorf("unknown install scope %q", scope)
	}
	var self string
	var sharing installTargetReader
	if scope == ExtensionScopeMe {
		self = manifest.NormalizeEmail(strings.TrimSpace(a.GetVaultInfo().Identity))
		if self == "" {
			return "", errors.New("set your email in Settings first — personal installs are scoped to you")
		}
		var err error
		sharing, err = a.sharingVault()
		if err != nil {
			return "", errors.New("this library doesn't support personal installs — install for everyone instead")
		}
	}
	wasUpdate := a.extensionPresent(id)

	name, publishedVersion, err := a.addExtensionFrom(dir)
	if err != nil {
		return "", err
	}
	if publishedVersion != "" {
		version = publishedVersion
	}

	if !wasUpdate && scope == ExtensionScopeMe {
		target := vaultpkg.InstallTarget{Kind: vaultpkg.InstallKindUser, User: self}
		if err := sharing.SetAssetInstallation(a.ctx, name, target); err != nil {
			// Roll the publish back: without the user scope the fresh
			// asset would sit library-wide, the opposite of what the
			// caller asked for.
			if v, verr := a.currentVault(); verr == nil {
				if rerr := v.RemoveAsset(a.ctx, name, "", true); rerr != nil {
					logger.Get().Warn("could not roll back failed personal install", "asset", name, "error", rerr)
				}
			}
			return "", friendlyVaultError(err)
		}
	}
	a.emitExtensionInstall(name, version, scope, source, wasUpdate)
	return name, nil
}

// extensionPresent reports whether the current vault already has an
// app-plugin asset under this plugin id.
func (a *App) extensionPresent(id string) bool {
	if r, err := a.sharingVault(); err == nil {
		if _, present, err := r.CurrentInstallTargets(a.ctx, id); err == nil {
			return present
		}
	}
	v, err := a.currentVault()
	if err != nil {
		return false
	}
	res, err := v.ListAssets(a.ctx, vaultpkg.ListAssetsOptions{Type: asset.TypeAppPlugin.Key, Limit: 200})
	if err != nil {
		return false
	}
	for _, s := range res.Assets {
		if s.Name == id {
			return true
		}
	}
	return false
}

// RemoveExtensionAsset removes an extension from the vault the way the
// caller means it: a personal install loses only the caller's user scope
// (full delete when no one else receives it), a shared one is deleted for
// everyone. Returns a short summary of what actually happened.
//
// The full delete for a recipient WITHOUT a personal scope is deliberate:
// a team/org-shared extension would come right back on the next sync if
// we only touched their view, "I don't want it running" is the enable
// toggle's job, and the vault's edit gate (docs/rbac.md — team-scoped
// assets are editable by team members only) plus the confirm dialog's
// "anyone it's shared with loses it too" bound the blast radius.
func (a *App) RemoveExtensionAsset(id string) (string, error) {
	if err := validateAssetRef(id, ""); err != nil {
		return "", err
	}
	v, err := a.currentVault()
	if err != nil {
		return "", err
	}
	self := manifest.NormalizeEmail(strings.TrimSpace(a.GetVaultInfo().Identity))

	var targets []vaultpkg.InstallTarget
	var sharing installTargetReader
	if r, rerr := a.sharingVault(); rerr == nil {
		sharing = r
		if t, _, terr := r.CurrentInstallTargets(a.ctx, id); terr == nil {
			targets = t
		}
	}
	shared, mine := false, false
	if self != "" {
		shared, mine = a.assetReachesUser(targets, self)
	}

	if mine && sharing != nil {
		onlyMine := true
		for _, t := range targets {
			if t.Kind != vaultpkg.InstallKindUser || manifest.NormalizeEmail(t.User) != self {
				onlyMine = false
				break
			}
		}
		if !onlyMine {
			// Others still receive it — drop only the caller's personal
			// scope and leave the asset in place for them.
			target := vaultpkg.InstallTarget{Kind: vaultpkg.InstallKindUser, User: self}
			if err := sharing.RemoveAssetInstallation(a.ctx, id, target); err != nil {
				return "", friendlyVaultError(err)
			}
			a.emitExtensionRemove(id, "personal")
			if shared {
				return "Removed your personal install — the library still shares this with you", nil
			}
			return "Removed your personal install — it stays for the others it's shared with", nil
		}
		if err := v.RemoveAsset(a.ctx, id, "", true); err != nil {
			return "", friendlyVaultError(err)
		}
		a.purgeSearchCache(id)
		a.emitExtensionRemove(id, "personal")
		return "Removed — it was only installed for you", nil
	}

	if err := v.RemoveAsset(a.ctx, id, "", true); err != nil {
		return "", friendlyVaultError(err)
	}
	a.purgeSearchCache(id)
	a.emitExtensionRemove(id, "library")
	return "Removed for everyone in the library", nil
}

// ShareExtensionWithLibrary promotes a personally-installed extension to
// the whole library. An org install clears every scope, so the personal
// user scope goes away with it. The vault's RBAC gate decides whether the
// caller may do this (org-admins only, on governed vaults).
func (a *App) ShareExtensionWithLibrary(id string) error {
	if err := validateAssetRef(id, ""); err != nil {
		return err
	}
	sharing, err := a.sharingVault()
	if err != nil {
		return err
	}
	target := vaultpkg.InstallTarget{Kind: vaultpkg.InstallKindOrg}
	if err := sharing.SetAssetInstallation(a.ctx, id, target); err != nil {
		return friendlyVaultError(err)
	}
	event := mgmt.AuditEvent{
		Timestamp:  time.Now(),
		Actor:      strings.TrimSpace(a.GetVaultInfo().Identity),
		Event:      mgmt.EventPluginShared,
		TargetType: mgmt.TargetTypePlugin,
		Target:     id,
	}
	go a.appendPluginAudit(event)
	return nil
}

// extensionScope computes who receives the named extension relative to
// the caller. teams is a memoized fetch (teamsOnce) so a loop over many
// assets resolves team membership from one read. A nil reader or unknown
// identity can't tell, so the extension stays visible with no chip —
// filtering must fail open, never hide.
func (a *App) extensionScope(name, self string, r installTargetReader, teams func() []TeamInfo) ExtensionScope {
	if r == nil || self == "" {
		return ExtensionScope{Shared: true}
	}
	targets, _, err := r.CurrentInstallTargets(a.ctx, name)
	if err != nil {
		return ExtensionScope{Shared: true}
	}
	shared, mine := assetReachesUserVia(targets, self, teams)
	return ExtensionScope{
		Shared:   shared,
		Personal: mine,
		Label:    extensionScopeLabel(targets, mine),
	}
}

// extensionScopeLabel renders a short chip for an extension's install
// targets. Empty targets (or an explicit org row) mean everyone; repo,
// path, and bot scopes never reach app users, so they contribute nothing.
func extensionScopeLabel(targets []vaultpkg.InstallTarget, mine bool) string {
	if len(targets) == 0 {
		return "Everyone"
	}
	var teams []string
	users := 0
	for _, t := range targets {
		switch t.Kind {
		case vaultpkg.InstallKindOrg:
			return "Everyone"
		case vaultpkg.InstallKindTeam:
			if !slices.Contains(teams, t.Team) {
				teams = append(teams, t.Team)
			}
		case vaultpkg.InstallKindUser:
			users++
		case vaultpkg.InstallKindRepo, vaultpkg.InstallKindPath, vaultpkg.InstallKindBot:
		}
	}
	var parts []string
	if len(teams) > 0 {
		parts = append(parts, "Team "+strings.Join(teams, ", "))
	}
	switch {
	case mine && users == 1 && len(teams) == 0:
		return "Just you"
	case mine && users > 1:
		parts = append(parts, fmt.Sprintf("you + %d more", users-1))
	case mine:
		parts = append(parts, "you")
	case users > 0:
		parts = append(parts, fmt.Sprintf("%d people", users))
	}
	if len(parts) == 0 {
		return "Everyone"
	}
	return strings.Join(parts, " · ")
}

// emitExtensionInstall appends the install/update audit event and the
// usage event that feeds `sx stats`. Both are fire-and-forget: on a git
// vault each append is a pull+commit+push and must not block the UI —
// the published asset is the durable state.
func (a *App) emitExtensionInstall(id, version, scope, source string, wasUpdate bool) {
	actor := strings.TrimSpace(a.GetVaultInfo().Identity)
	event := mgmt.AuditEvent{
		Timestamp:  time.Now(),
		Actor:      actor,
		Event:      mgmt.EventPluginInstalled,
		TargetType: mgmt.TargetTypePlugin,
		Target:     id,
		Data: map[string]any{
			"version": version,
			"scope":   extensionScopeAuditName(scope),
			"source":  source,
		},
	}
	if wasUpdate {
		event.Event = mgmt.EventPluginUpdated
	}
	go a.appendPluginAudit(event)
	usage := mgmt.UsageEvent{
		Timestamp:    event.Timestamp,
		Actor:        actor,
		AssetName:    id,
		AssetVersion: version,
		AssetType:    asset.TypeAppPlugin.Key,
	}
	go a.recordPluginUsage(usage)
}

// emitExtensionRemove appends the uninstall audit event (fire-and-forget,
// same reasoning as emitExtensionInstall).
func (a *App) emitExtensionRemove(id, scope string) {
	event := mgmt.AuditEvent{
		Timestamp:  time.Now(),
		Actor:      strings.TrimSpace(a.GetVaultInfo().Identity),
		Event:      mgmt.EventPluginUninstalled,
		TargetType: mgmt.TargetTypePlugin,
		Target:     id,
		Data:       map[string]any{"scope": scope},
	}
	go a.appendPluginAudit(event)
}

// recordPluginUsage writes one usage event to the vault's usage stream,
// mirroring appendPluginAudit's context handling.
func (a *App) recordPluginUsage(e mgmt.UsageEvent) {
	v, err := a.currentVault()
	if err != nil {
		return
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if err := v.RecordUsageEvents(ctx, []mgmt.UsageEvent{e}); err != nil {
		logger.Get().Warn("extension usage append failed", "error", err)
	}
}
