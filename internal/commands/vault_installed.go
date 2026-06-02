package commands

import (
	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/assets"
	"github.com/sleuth-io/sx/internal/lockfile"
)

// filterInstalledForProfile keeps the tracker entries that belong to the
// current profile. An empty profile counts as the current profile, so
// untagged leftovers show under whatever profile you're using. When typeFilter
// is set, only that asset type is kept.
func filterInstalledForProfile(all []assets.InstalledAsset, currentProfile, typeFilter string) []assets.InstalledAsset {
	wantType := ""
	if typeFilter != "" {
		wantType = asset.FromString(typeFilter).Key
	}

	var out []assets.InstalledAsset
	for _, a := range all {
		if a.Profile != "" && a.Profile != currentProfile {
			continue
		}
		if wantType != "" && a.Type != wantType {
			continue
		}
		out = append(out, a)
	}
	return out
}

// installedToLockfileAssets adapts tracker entries to lockfile.Asset so the
// shared installed-list printers can render them. Each tracker entry is a
// single scope: global entries get no scopes, repo/path entries get one.
// Profile and Clients are intentionally not carried over: lockfile.Asset has
// no place for them and the installed-list printers don't display them. A
// future profile-scoped view would need to read those fields off the tracker
// entry directly rather than through this adapter.
func installedToLockfileAssets(in []assets.InstalledAsset) []lockfile.Asset {
	out := make([]lockfile.Asset, 0, len(in))
	for _, a := range in {
		la := lockfile.Asset{
			Name:    a.Name,
			Version: a.Version,
			Type:    asset.FromString(a.Type),
		}
		if a.Repository != "" {
			s := lockfile.Scope{Repo: a.Repository}
			if a.Path != "" {
				s.Paths = []string{a.Path}
			}
			la.Scopes = []lockfile.Scope{s}
		}
		out = append(out, la)
	}
	return out
}
