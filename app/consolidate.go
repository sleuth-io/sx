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
// duplicate cluster onto one surviving asset. The survivor's reach is
// the UNION of everyone's — each retired asset's install rows move onto
// the survivor before the retiree is removed — so nobody who had a copy
// loses the capability. Retirement is the soft kind (version archive
// kept), so a wrong consolidation is recoverable. This is the one
// extension-reachable mutation that removes assets, which is why it
// sits behind its own dangerous permission (assets:consolidate) and why
// the UI must confirm loudly. The vault's RBAC stays the real gate: a
// source whose rows the caller may not move is KEPT, never retired —
// reach must never shrink.

// ConsolidateResult reports what a consolidation actually did.
type ConsolidateResult struct {
	// MovedInstallations counts install rows added to the survivor.
	MovedInstallations int `json:"movedInstallations"`
	// Retired lists the assets removed (recoverable from the archive).
	Retired []string `json:"retired"`
	// Kept lists sources NOT retired because part of their reach was
	// refused (see Skipped) — retiring them would shrink someone's
	// access. They stay in the library for a retry with more rights.
	Kept []string `json:"kept"`
	// Skipped carries vault-refused install moves ("team X: not a
	// member"); the consolidation continues past them.
	Skipped []string `json:"skipped"`
}

func consolidateTargetKey(t vaultpkg.InstallTarget) string {
	return fmt.Sprintf("%s|%s|%v|%s|%s|%s", t.Kind, t.Repo, t.Paths, t.Team, t.User, t.Bot)
}

// consolidateSources validates and dedupes the from-list.
func consolidateSources(into string, from []string) ([]string, error) {
	if err := validateAssetRef(into, ""); err != nil {
		return nil, err
	}
	sources := make([]string, 0, len(from))
	seen := map[string]bool{into: true}
	for _, name := range from {
		if err := validateAssetRef(name, ""); err != nil {
			return nil, err
		}
		if seen[name] {
			continue // survivor in the from-list, or a duplicate entry
		}
		seen[name] = true
		sources = append(sources, name)
	}
	if len(sources) == 0 {
		return nil, errors.New("nothing to consolidate: no source assets besides the survivor")
	}
	return sources, nil
}

// reachPlan is the read phase's output: what has to move where.
type reachPlan struct {
	intoEveryone bool
	needsOrg     bool // some source reaches everyone
	toAdd        []vaultpkg.InstallTarget
	sourceKeys   map[string][]string // source -> its reach's target keys
}

// planReach reads the survivor's and every source's install rows.
// `present` distinguishes "in the manifest with no rows" (org-wide)
// from "no such asset" — a typo'd name must fail HERE, before anything
// is retired, never read as "reaches everyone".
func (a *App) planReach(r installTargetReader, into string, sources []string) (*reachPlan, error) {
	intoTargets, intoPresent, err := r.CurrentInstallTargets(a.ctx, into)
	if err != nil {
		return nil, friendlyVaultError(err)
	}
	if !intoPresent {
		return nil, fmt.Errorf("survivor %q not found in this library", into)
	}
	plan := &reachPlan{
		intoEveryone: len(intoTargets) == 0,
		sourceKeys:   map[string][]string{},
	}
	// Rows the survivor already has are covered without a write —
	// seeding here both avoids redundant writes and keeps
	// MovedInstallations honest.
	seenTarget := map[string]bool{}
	for _, t := range intoTargets {
		seenTarget[consolidateTargetKey(t)] = true
	}
	for _, name := range sources {
		targets, present, terr := r.CurrentInstallTargets(a.ctx, name)
		if terr != nil {
			return nil, friendlyVaultError(terr)
		}
		if !present {
			return nil, fmt.Errorf("asset %q not found in this library", name)
		}
		if len(targets) == 0 {
			plan.needsOrg = true
			continue
		}
		for _, t := range targets {
			key := consolidateTargetKey(t)
			plan.sourceKeys[name] = append(plan.sourceKeys[name], key)
			// Two sources sharing the same reach write (and count) once.
			if seenTarget[key] {
				continue
			}
			seenTarget[key] = true
			plan.toAdd = append(plan.toAdd, t)
		}
	}
	return plan, nil
}

// applyReach writes the moves onto the survivor. Returns the keys the
// vault REFUSED (RBAC) and whether an org-wide move failed outright —
// a source whose reach didn't fully land must NOT be retired.
func (a *App) applyReach(bulk bulkInstallTargetWriter, into string, plan *reachPlan, result *ConsolidateResult) (skippedKeys map[string]bool, orgMoveFailed bool, err error) {
	skippedKeys = map[string]bool{}
	switch {
	case plan.intoEveryone:
		// Survivor already reaches everyone; nothing narrower to add.
	case plan.needsOrg:
		// A source reached everyone, so the survivor must too. The org
		// target replaces the survivor's narrower rows by design.
		skipped, serr := bulk.SetAssetInstallations(a.ctx, into,
			[]vaultpkg.InstallTarget{{Kind: vaultpkg.InstallKindOrg}}, true)
		if serr != nil {
			return nil, false, friendlyVaultError(serr)
		}
		if len(skipped) > 0 {
			// The survivor could not go org-wide: NOTHING is covered
			// (the narrower rows were never attempted in this branch),
			// so no source may retire.
			orgMoveFailed = true
			for _, s := range skipped {
				result.Skipped = append(result.Skipped, s.Reason)
			}
		} else {
			result.MovedInstallations++
		}
	case len(plan.toAdd) > 0:
		skipped, serr := bulk.SetAssetInstallations(a.ctx, into, plan.toAdd, true)
		if serr != nil {
			return nil, false, friendlyVaultError(serr)
		}
		for _, s := range skipped {
			result.Skipped = append(result.Skipped, s.Reason)
			skippedKeys[consolidateTargetKey(s.Target)] = true
		}
		result.MovedInstallations = len(plan.toAdd) - len(skipped)
	}
	return skippedKeys, orgMoveFailed, nil
}

// retireSources removes each retirable source (version archive kept)
// and appends audit events through one sequential writer.
func (a *App) retireSources(v vaultpkg.Vault, id, into string, sources []string, retirable func(string) bool, result *ConsolidateResult) error {
	// File vaults implement RetireAsset (manifest + root view removed,
	// version archive kept); skills.new vaults get the same recoverable
	// semantics from the server's non-delete removal.
	retire := func(name string) error {
		if retirer, ok := v.(interface {
			RetireAsset(ctx context.Context, name string) error
		}); ok {
			return retirer.RetireAsset(a.ctx, name)
		}
		return v.RemoveAsset(a.ctx, name, "", false)
	}
	actor := strings.TrimSpace(a.GetVaultInfo().Identity)
	var audits []mgmt.AuditEvent
	var retireErr error
	for _, name := range sources {
		if !retirable(name) {
			result.Kept = append(result.Kept, name)
			continue
		}
		if rerr := retire(name); rerr != nil {
			done := "none yet"
			if len(result.Retired) > 0 {
				done = strings.Join(result.Retired, ", ")
			}
			retireErr = fmt.Errorf("installations moved and retired so far: %s — retiring %q failed: %w",
				done, name, friendlyVaultError(rerr))
			break
		}
		result.Retired = append(result.Retired, name)
		audits = append(audits, mgmt.AuditEvent{
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
	// One fire-and-forget writer for all events, not one goroutine per
	// retire — sequential appends can't interleave in the audit log.
	a.goEvent(func() {
		for _, event := range audits {
			a.appendPluginAudit(event)
		}
	})
	return retireErr
}

// ConsolidateAssets moves every install row from each `from` asset onto
// `into`, then soft-retires the `from` assets. The id is the calling
// extension, for audit attribution.
func (a *App) ConsolidateAssets(id, into string, from []string) (ConsolidateResult, error) {
	var result ConsolidateResult
	if err := validatePluginID(id); err != nil {
		return result, err
	}
	sources, err := consolidateSources(into, from)
	if err != nil {
		return result, err
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

	plan, err := a.planReach(r, into, sources)
	if err != nil {
		return result, err
	}
	skippedKeys, orgMoveFailed, err := a.applyReach(bulk, into, plan, &result)
	if err != nil {
		return result, err
	}

	// A source retires only when its whole reach is safely on the
	// survivor: the survivor reaches everyone, or none of the source's
	// rows were refused.
	retirable := func(name string) bool {
		if orgMoveFailed {
			return false
		}
		if plan.intoEveryone || plan.needsOrg {
			return true
		}
		for _, key := range plan.sourceKeys[name] {
			if skippedKeys[key] {
				return false
			}
		}
		return true
	}
	if err := a.retireSources(v, id, into, sources, retirable, &result); err != nil {
		return result, err
	}
	return result, nil
}
