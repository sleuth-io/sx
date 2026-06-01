// Package vaultcopy implements a backend-agnostic copy of a vault's contents
// (teams, bots, assets and their versions, installation scopes, audit history,
// and usage history) from one vault into another. It is the engine behind
// `sx vault copy`.
//
// Everything is read through the vault.Vault interface (plus a couple of
// optional capability interfaces), so any source/destination combination of
// path, git, and skills.new vaults works. The copy is best-effort at the item
// level: a failure copying one team/asset/etc. is recorded as a warning and the
// copy continues, so a single bad item never aborts a whole migration.
package vaultcopy

import (
	"context"
	"errors"
	"fmt"

	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/manifest"
	"github.com/sleuth-io/sx/internal/mgmt"
	"github.com/sleuth-io/sx/internal/vault"
	"github.com/sleuth-io/sx/internal/version"
)

// Options selects which categories to copy and whether to run read-only.
type Options struct {
	Teams  bool
	Bots   bool
	Assets bool
	Audit  bool
	Usage  bool
	DryRun bool
}

// DefaultOptions copies everything.
func DefaultOptions() Options {
	return Options{Teams: true, Bots: true, Assets: true, Audit: true, Usage: true}
}

// Report summarizes what a copy did (or would do, for a dry run).
type Report struct {
	Teams           int
	Bots            int
	Assets          int
	Versions        int
	SkippedVersions int
	Scopes          int
	AuditEvents     int
	UsageEvents     int
	Warnings        []string
}

func (r *Report) warnf(format string, args ...any) {
	r.Warnings = append(r.Warnings, fmt.Sprintf(format, args...))
}

// assetScopeReader reads an asset's installation scopes from a vault. All three
// backends implement it: file-backed vaults read sx.toml; the Sleuth vault reads
// the server's installation rows. `present` reports whether the asset is
// registered (an org-wide asset is present with no scopes).
type assetScopeReader interface {
	AssetInstallScopes(ctx context.Context, name string) (scopes []manifest.Scope, present bool, err error)
}

const assetListLimit = 10000

// sleuthBotListSoftCap mirrors the Sleuth backend's bot-list soft cap. Kept here
// only to warn when a copy may have been truncated; if the server cap changes
// this just affects when the heuristic warning fires, not correctness.
const sleuthBotListSoftCap = 1000

// Copy migrates the selected categories from src into dst. Each category is
// independent: a category-level failure is recorded as a warning and the copy
// moves on to the next, so one broken category (e.g. audit) never costs the
// others (e.g. usage). Always returns a Report for partial-progress reporting.
func Copy(ctx context.Context, src, dst vault.Vault, opts Options) (*Report, error) {
	r := &Report{}
	// Run in a fixed order (teams/bots before assets so scope targets exist).
	stages := []struct {
		name string
		on   bool
		fn   func(context.Context, vault.Vault, vault.Vault, Options, *Report) error
	}{
		{"teams", opts.Teams, copyTeams},
		{"bots", opts.Bots, copyBots},
		{"assets", opts.Assets, copyAssets},
		{"audit", opts.Audit, copyAudit},
		{"usage", opts.Usage, copyUsage},
	}
	for _, s := range stages {
		if !s.on {
			continue
		}
		if err := s.fn(ctx, src, dst, opts, r); err != nil {
			// Audit/usage import is additive, so a mid-stage failure that already
			// landed some chunks will double them on retry — say so in the moment.
			if s.name == "audit" || s.name == "usage" {
				r.warnf("copy %s failed: %v — re-running may duplicate already-imported events on the destination", s.name, err)
			} else {
				r.warnf("copy %s failed: %v", s.name, err)
			}
		}
	}
	return r, nil
}

func copyTeams(ctx context.Context, src, dst vault.Vault, opts Options, r *Report) error {
	res, err := src.ListTeams(ctx, vault.ListTeamsOptions{Limit: vault.DefaultTeamsLimit})
	if err != nil {
		return err
	}
	if res.HasMore {
		r.warnf("source has more than %d teams; only the first %d were copied", vault.DefaultTeamsLimit, vault.DefaultTeamsLimit)
	}
	for _, summary := range res.Teams {
		full, err := src.GetTeam(ctx, summary.Name)
		if err != nil {
			r.warnf("read team %q: %v", summary.Name, err)
			continue
		}
		r.Teams++
		if opts.DryRun {
			continue
		}
		team := mgmt.Team{
			Name:        full.Name,
			Description: full.Description,
			Members:     full.Members,
			Admins:      full.Admins,
		}
		if err := dst.CreateTeam(ctx, team); err != nil {
			// May already exist on a re-run; record and still try to sync repos.
			r.warnf("create team %q: %v", full.Name, err)
		}
		for _, repo := range full.Repositories {
			if err := dst.AddTeamRepository(ctx, full.Name, repo); err != nil {
				r.warnf("add repo %q to team %q: %v", repo, full.Name, err)
			}
		}
	}
	return nil
}

func copyBots(ctx context.Context, src, dst vault.Vault, opts Options, r *Report) error {
	bots, err := src.ListBots(ctx)
	if err != nil {
		return err
	}
	// The Sleuth backend lists bots under a soft cap (1000); ListBots exposes no
	// HasMore, so surface a heuristic warning when we hit it rather than only
	// emitting a structured log the operator won't see.
	if len(bots) >= sleuthBotListSoftCap {
		r.warnf("source returned %d bots (the server's list cap); any beyond that were not copied", len(bots))
	}
	for _, b := range bots {
		r.Bots++
		if opts.DryRun {
			continue
		}
		// CreateBot on Sleuth auto-issues a default API key (returned once and
		// otherwise unrecoverable). The copy can't carry keys, so the auto-issued
		// one would be an orphan — revoke it so the destination doesn't accumulate
		// dead keys. Operators regenerate keys as needed.
		rawToken, err := dst.CreateBot(ctx, mgmt.Bot{Name: b.Name, Description: b.Description, Teams: b.Teams})
		if err != nil {
			r.warnf("create bot %q: %v", b.Name, err)
			continue
		}
		if rawToken != "" {
			revokeAutoIssuedBotKeys(ctx, dst, b.Name, r)
		}
	}
	if len(bots) > 0 && !opts.DryRun {
		r.warnf("bot API keys are not copied; create fresh ones on the destination with 'sx bot key create'")
	}
	return nil
}

// botKeyManager is the slice of a vault needed to clean up an auto-issued bot
// key (Sleuth implements BotApiKeyManager; file-backed vaults don't issue keys).
type botKeyManager interface {
	ListBotApiKeys(ctx context.Context, botName string) ([]mgmt.BotApiKey, error)
	DeleteBotApiKey(ctx context.Context, botName, keyID string) error
}

// revokeAutoIssuedBotKeys deletes the key(s) a just-created bot was auto-issued,
// so a copy doesn't leave orphan, unusable keys on the destination.
func revokeAutoIssuedBotKeys(ctx context.Context, dst vault.Vault, botName string, r *Report) {
	km, ok := dst.(botKeyManager)
	if !ok {
		return
	}
	keys, err := km.ListBotApiKeys(ctx, botName)
	if err != nil {
		r.warnf("list auto-issued keys for bot %q: %v", botName, err)
		return
	}
	for _, k := range keys {
		if err := km.DeleteBotApiKey(ctx, botName, k.ID); err != nil {
			r.warnf("revoke auto-issued key for bot %q: %v", botName, err)
		}
	}
}

func copyAssets(ctx context.Context, src, dst vault.Vault, opts Options, r *Report) error {
	res, err := src.ListAssets(ctx, vault.ListAssetsOptions{Limit: assetListLimit})
	if err != nil {
		return err
	}
	scopeReader, canReadScopes := src.(assetScopeReader)
	if !canReadScopes && len(res.Assets) > 0 {
		r.warnf("source does not expose asset installation scopes; assets copied without scopes")
	}
	for _, a := range res.Assets {
		versions, err := src.GetVersionList(ctx, a.Name)
		if err != nil {
			r.warnf("list versions for %q: %v", a.Name, err)
			continue
		}

		// Read the source's scopes BEFORE uploading any versions. If the read
		// fails we skip the asset entirely rather than leaving stranded versions
		// on the destination (and, on Sleuth, an auto-applied org-wide install
		// with no way to know it should have been narrowed).
		var (
			scopes  []manifest.Scope
			present bool
		)
		if canReadScopes {
			scopes, present, err = scopeReader.AssetInstallScopes(ctx, a.Name)
			if err != nil {
				r.warnf("read scopes for %q: %v; skipping asset", a.Name, err)
				continue
			}
		}

		r.Assets++
		for _, v := range version.Sort(versions) {
			if opts.DryRun {
				r.Versions++
				continue
			}
			data, err := src.GetAssetByVersion(ctx, a.Name, v)
			if err != nil {
				r.warnf("download %q@%s: %v", a.Name, v, err)
				continue
			}
			la := &lockfile.Asset{Name: a.Name, Version: v, Type: a.Type}
			if err := dst.AddAsset(ctx, la, data); err != nil {
				var exists *vault.ErrVersionExists
				if errors.As(err, &exists) {
					r.SkippedVersions++
					continue
				}
				r.warnf("upload %q@%s: %v", a.Name, v, err)
				continue
			}
			r.Versions++
		}
		if canReadScopes {
			copyAssetScopes(ctx, dst, a.Name, scopes, present, opts.DryRun, r)
		}
	}
	return nil
}

// scopeInstaller is the slice of a vault the scope-copy step needs: set one
// installation target at a time, or clear an asset's installs entirely.
// Narrowed from vault.Vault so the dispatch logic is unit-testable with a fake.
type scopeInstaller interface {
	SetAssetInstallation(ctx context.Context, name string, target vault.InstallTarget) error
	ClearAssetInstallations(ctx context.Context, name string) error
}

// bulkInstaller is implemented by vaults that replace-on-set and so must set an
// asset's whole installation set in one call (the Sleuth vault): setting scopes
// one at a time would let each call clobber the last, leaving only the final
// scope. It returns the targets it couldn't resolve in the destination (skipped,
// not applied) so the caller can warn without the others being lost.
type bulkInstaller interface {
	SetAssetInstallations(ctx context.Context, name string, targets []vault.InstallTarget) (unresolved []vault.InstallTarget, err error)
}

// clearAutoInstall undoes any install a destination auto-applies on upload
// (skills.new publishes uploaded assets org-wide by default), so an asset that
// should end up with no — or no resolvable — scopes isn't silently widened.
// No-op on backends that don't auto-install.
func clearAutoInstall(ctx context.Context, dst scopeInstaller, name string, r *Report) {
	if err := dst.ClearAssetInstallations(ctx, name); err != nil {
		r.warnf("clear auto-applied install on %q: %v", name, err)
	}
}

func copyAssetScopes(ctx context.Context, dst scopeInstaller, name string, scopes []manifest.Scope, present, dryRun bool, r *Report) {
	if !present {
		// The asset has no installation in the source, so it should have none in
		// the destination. Clear the auto-applied install to match.
		if !dryRun {
			clearAutoInstall(ctx, dst, name, r)
		}
		return
	}

	var targets []vault.InstallTarget
	if len(scopes) == 0 {
		// Registered with no scopes == org-wide. Register it so the destination
		// records the global install rather than leaving an orphan file set.
		targets = []vault.InstallTarget{{Kind: vault.InstallKindOrg}}
	} else {
		for _, sc := range scopes {
			target, ok := scopeToTarget(sc)
			if !ok {
				r.warnf("asset %q: unsupported scope kind %q; skipped", name, sc.Kind)
				continue
			}
			targets = append(targets, target)
		}
	}
	if len(targets) == 0 {
		return
	}
	if dryRun {
		r.Scopes += len(targets)
		return
	}

	// On a replace-on-set backend (skills.new) we must set every scope in one
	// call — per-target calls would clobber each other. The bulk call is
	// best-effort: it applies all resolvable targets atomically and reports the
	// rest as unresolved (so we never fall back to clobbering per-target calls).
	if bulk, ok := dst.(bulkInstaller); ok {
		unresolved, err := bulk.SetAssetInstallations(ctx, name, targets)
		if err != nil {
			// The set failed, so the asset still carries any auto-applied install
			// (org-wide on skills.new). Clear it so a failed scope-set doesn't
			// leave the asset more permissive than the source intended.
			r.warnf("set scopes on %q: %v", name, err)
			clearAutoInstall(ctx, dst, name, r)
			return
		}
		for _, u := range unresolved {
			r.warnf("scope %s on %q skipped (not found in destination)", u.Describe(), name)
		}
		applied := len(targets) - len(unresolved)
		if applied == 0 {
			// Nothing resolved (every target absent from the destination). The
			// asset would otherwise keep its auto-applied org-wide install, which
			// is more permissive than the source — clear it instead.
			clearAutoInstall(ctx, dst, name, r)
			return
		}
		r.Scopes += applied
		return
	}
	// Append-on-set backends (file-backed): each target is independent, so a
	// per-target loop is correct and gives partial success.
	for _, target := range targets {
		if err := dst.SetAssetInstallation(ctx, name, target); err != nil {
			r.warnf("set scope %s on %q: %v", target.Describe(), name, err)
			continue
		}
		r.Scopes++
	}
}

func scopeToTarget(sc manifest.Scope) (vault.InstallTarget, bool) {
	switch sc.Kind {
	case manifest.ScopeKindOrg:
		return vault.InstallTarget{Kind: vault.InstallKindOrg}, true
	case manifest.ScopeKindRepo:
		return vault.InstallTarget{Kind: vault.InstallKindRepo, Repo: sc.Repo}, true
	case manifest.ScopeKindPath:
		return vault.InstallTarget{Kind: vault.InstallKindPath, Repo: sc.Repo, Paths: sc.Paths}, true
	case manifest.ScopeKindTeam:
		return vault.InstallTarget{Kind: vault.InstallKindTeam, Team: sc.Team}, true
	case manifest.ScopeKindUser:
		return vault.InstallTarget{Kind: vault.InstallKindUser, User: sc.User}, true
	case manifest.ScopeKindBot:
		return vault.InstallTarget{Kind: vault.InstallKindBot, Bot: sc.Bot}, true
	}
	return vault.InstallTarget{}, false
}

func copyAudit(ctx context.Context, src, dst vault.Vault, opts Options, r *Report) error {
	events, err := src.QueryAuditEvents(ctx, mgmt.AuditFilter{})
	if err != nil {
		return err
	}
	r.AuditEvents = len(events)
	if opts.DryRun || len(events) == 0 {
		return nil
	}
	return dst.ImportAuditEvents(ctx, events)
}

func copyUsage(ctx context.Context, src, dst vault.Vault, opts Options, r *Report) error {
	events, err := src.ReadUsageEvents(ctx, mgmt.UsageFilter{})
	if err != nil {
		return err
	}
	r.UsageEvents = len(events)
	if opts.DryRun || len(events) == 0 {
		return nil
	}
	return dst.RecordUsageEvents(ctx, events)
}
