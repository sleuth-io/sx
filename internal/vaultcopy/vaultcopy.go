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

// Copy migrates the selected categories from src into dst. It returns a Report
// even on error so the caller can show partial progress.
func Copy(ctx context.Context, src, dst vault.Vault, opts Options) (*Report, error) {
	r := &Report{}
	if opts.Teams {
		if err := copyTeams(ctx, src, dst, opts, r); err != nil {
			return r, fmt.Errorf("copy teams: %w", err)
		}
	}
	if opts.Bots {
		if err := copyBots(ctx, src, dst, opts, r); err != nil {
			return r, fmt.Errorf("copy bots: %w", err)
		}
	}
	if opts.Assets {
		if err := copyAssets(ctx, src, dst, opts, r); err != nil {
			return r, fmt.Errorf("copy assets: %w", err)
		}
	}
	if opts.Audit {
		if err := copyAudit(ctx, src, dst, opts, r); err != nil {
			return r, fmt.Errorf("copy audit: %w", err)
		}
	}
	if opts.Usage {
		if err := copyUsage(ctx, src, dst, opts, r); err != nil {
			return r, fmt.Errorf("copy usage: %w", err)
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
	for _, b := range bots {
		r.Bots++
		if opts.DryRun {
			continue
		}
		// CreateBot may auto-issue a token (Sleuth) — it can't be copied, so we
		// drop it and note that bot keys must be regenerated.
		if _, err := dst.CreateBot(ctx, mgmt.Bot{Name: b.Name, Description: b.Description, Teams: b.Teams}); err != nil {
			r.warnf("create bot %q: %v", b.Name, err)
		}
	}
	if len(bots) > 0 && !opts.DryRun {
		r.warnf("bot API keys are not copyable; regenerate them on the destination")
	}
	return nil
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
			scopes, present, err := scopeReader.AssetInstallScopes(ctx, a.Name)
			if err != nil {
				r.warnf("read scopes for %q: %v", a.Name, err)
			} else {
				copyAssetScopes(ctx, dst, a.Name, scopes, present, opts.DryRun, r)
			}
		}
	}
	return nil
}

func copyAssetScopes(ctx context.Context, dst vault.Vault, name string, scopes []manifest.Scope, present, dryRun bool, r *Report) {
	if !present {
		// Asset exists only as uploaded files (never installed) — nothing to
		// register on the destination beyond the version content already copied.
		return
	}
	if len(scopes) == 0 {
		// Registered with no scopes == org-wide. Register it so the destination
		// records the global install rather than leaving an orphan file set.
		if !dryRun {
			if err := dst.SetAssetInstallation(ctx, name, vault.InstallTarget{Kind: vault.InstallKindOrg}); err != nil {
				r.warnf("set org-wide install on %q: %v", name, err)
				return
			}
		}
		r.Scopes++
		return
	}
	if len(scopes) > 1 {
		// SetAssetInstallation appends on file-backed vaults but replaces on
		// skills.new, where each call supersedes the previous one. Flag it so a
		// multi-scope asset's installs can be verified on a server destination.
		r.warnf("asset %q has %d scopes; verify all applied on the destination (server vaults replace per call)", name, len(scopes))
	}
	for _, sc := range scopes {
		target, ok := scopeToTarget(sc)
		if !ok {
			r.warnf("asset %q: unsupported scope kind %q; skipped", name, sc.Kind)
			continue
		}
		if !dryRun {
			if err := dst.SetAssetInstallation(ctx, name, target); err != nil {
				r.warnf("set scope %s on %q: %v", target.Describe(), name, err)
				continue
			}
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
