package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sleuth-io/sx/internal/mgmt"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// Asset consolidation (API 1.9.0, docs/skill-dedupe-spec.md): collapse a
// duplicate cluster onto one surviving asset. The survivors' reach is
// the UNION of everyone's — each retired asset's install rows move onto
// the survivor before the retiree is removed — so nobody who had a copy
// loses the capability. Retirement is the soft kind: RemoveAsset with
// delete=false keeps the version archive, so a wrong consolidation is
// recoverable. This is the one extension-reachable mutation that removes
// assets, which is why it sits behind its own dangerous permission
// (assets:consolidate) and why the UI must confirm loudly. The vault's
// RBAC stays the real gate: rows the caller may not move come back in
// Skipped with the vault's reason, and a denied removal aborts.

// ConsolidateResult reports what a consolidation actually did.
type ConsolidateResult struct {
	// MovedInstallations counts install rows added to the survivor.
	MovedInstallations int `json:"movedInstallations"`
	// Retired lists the assets removed (recoverable from the archive).
	Retired []string `json:"retired"`
	// Skipped carries vault-refused install moves ("team X: not a
	// member"); the consolidation continues past them.
	Skipped []string `json:"skipped"`
}

// ConsolidateAssets moves every install row from each `from` asset onto
// `into`, then soft-retires the `from` assets. The id is the calling
// extension, for audit attribution.
func (a *App) ConsolidateAssets(id, into string, from []string) (ConsolidateResult, error) {
	var result ConsolidateResult
	if err := validatePluginID(id); err != nil {
		return result, err
	}
	if err := validateAssetRef(into, ""); err != nil {
		return result, err
	}
	sources := make([]string, 0, len(from))
	seen := map[string]bool{into: true}
	for _, name := range from {
		if err := validateAssetRef(name, ""); err != nil {
			return result, err
		}
		if seen[name] {
			continue // survivor in the from-list, or a duplicate entry
		}
		seen[name] = true
		sources = append(sources, name)
	}
	if len(sources) == 0 {
		return result, errors.New("nothing to consolidate: no source assets besides the survivor")
	}

	r, err := a.sharingVault()
	if err != nil {
		return result, err
	}
	v, err := a.currentVault()
	if err != nil {
		return result, err
	}
	bulk, ok := v.(bulkInstallTargetWriter)
	if !ok {
		return result, errors.New("this library doesn't support sharing controls")
	}

	// Read the survivor's reach first: if it is already org-wide there
	// is nothing to move, and if any source is org-wide the survivor
	// must become org-wide (an org row is exclusive on every backend).
	// `present` distinguishes "in the manifest with no rows" (org-wide)
	// from "no such asset" — a typo'd name must fail HERE, before
	// anything is retired, never read as "reaches everyone".
	intoTargets, intoPresent, err := r.CurrentInstallTargets(a.ctx, into)
	if err != nil {
		return result, friendlyVaultError(err)
	}
	if !intoPresent {
		return result, fmt.Errorf("survivor %q not found in this library", into)
	}
	intoEveryone := len(intoTargets) == 0

	var toAdd []vaultpkg.InstallTarget
	seenTarget := map[string]bool{}
	needsOrg := false
	for _, name := range sources {
		targets, present, terr := r.CurrentInstallTargets(a.ctx, name)
		if terr != nil {
			return result, friendlyVaultError(terr)
		}
		if !present {
			return result, fmt.Errorf("asset %q not found in this library", name)
		}
		if len(targets) == 0 {
			needsOrg = true
			continue
		}
		// Two sources sharing the same reach must count (and write) once.
		for _, t := range targets {
			key := fmt.Sprintf("%s|%s|%v|%s|%s|%s", t.Kind, t.Repo, t.Paths, t.Team, t.User, t.Bot)
			if seenTarget[key] {
				continue
			}
			seenTarget[key] = true
			toAdd = append(toAdd, t)
		}
	}

	switch {
	case intoEveryone:
		// Survivor already reaches everyone; nothing narrower to add.
	case needsOrg:
		// A source reached everyone, so the survivor must too. The org
		// target replaces the survivor's narrower rows by design.
		skipped, serr := bulk.SetAssetInstallations(a.ctx, into,
			[]vaultpkg.InstallTarget{{Kind: vaultpkg.InstallKindOrg}}, true)
		if serr != nil {
			return result, friendlyVaultError(serr)
		}
		for _, s := range skipped {
			result.Skipped = append(result.Skipped, s.Reason)
		}
		if len(skipped) == 0 {
			result.MovedInstallations++
		}
	case len(toAdd) > 0:
		skipped, serr := bulk.SetAssetInstallations(a.ctx, into, toAdd, true)
		if serr != nil {
			return result, friendlyVaultError(serr)
		}
		for _, s := range skipped {
			result.Skipped = append(result.Skipped, s.Reason)
		}
		result.MovedInstallations = len(toAdd) - len(skipped)
	}

	// Retire the sources only after their reach is safely on the
	// survivor. File vaults implement RetireAsset (manifest + root view
	// removed, version archive kept); skills.new vaults get the same
	// recoverable semantics from the server's non-delete removal.
	retire := func(name string) error {
		if retirer, ok := v.(interface {
			RetireAsset(ctx context.Context, name string) error
		}); ok {
			return retirer.RetireAsset(a.ctx, name)
		}
		return v.RemoveAsset(a.ctx, name, "", false)
	}
	actor := strings.TrimSpace(a.GetVaultInfo().Identity)
	for _, name := range sources {
		if rerr := retire(name); rerr != nil {
			done := "none yet"
			if len(result.Retired) > 0 {
				done = strings.Join(result.Retired, ", ")
			}
			return result, fmt.Errorf("installations moved and retired so far: %s — retiring %q failed: %w",
				done, name, friendlyVaultError(rerr))
		}
		result.Retired = append(result.Retired, name)
		go a.appendPluginAudit(mgmt.AuditEvent{
			Timestamp:  time.Now(),
			Actor:      actor,
			Event:      mgmt.EventAssetRemoved,
			TargetType: mgmt.TargetTypeAsset,
			Target:     name,
			Data: map[string]any{
				"reason":       "consolidated",
				"consolidated": map[string]any{"into": into, "by": id},
			},
		})
	}
	return result, nil
}
