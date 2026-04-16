package vault

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"

	"github.com/sleuth-io/sx/internal/mgmt"
)

// acquirePathLock takes a file lock scoped to the PathVault's repo path so
// multiple sx processes writing to the same local vault don't corrupt the
// .sx/ files. Read-only operations skip the lock.
func (p *PathVault) acquirePathLock(ctx context.Context) (*flock.Flock, error) {
	lockPath := filepath.Join(p.repoPath, ".sx", ".lock")
	fl := flock.New(lockPath)
	locked, err := fl.TryLockContext(ctx, 100*time.Millisecond)
	if err != nil {
		return nil, err
	}
	if !locked {
		return nil, errors.New("could not acquire path vault lock (timeout)")
	}
	return fl, nil
}

// currentActor reads the caller's git identity, falling back to $USER@host.
// Cached by mgmt.CurrentGitActor for the CLI's lifetime.
func (p *PathVault) CurrentActor(ctx context.Context) (mgmt.Actor, error) {
	return mgmt.CurrentGitActor(ctx, p.repoPath)
}

func (p *PathVault) ListTeams(ctx context.Context) ([]mgmt.Team, error) {
	return commonListTeams(p.repoPath)
}

func (p *PathVault) GetTeam(ctx context.Context, name string) (*mgmt.Team, error) {
	return commonGetTeam(p.repoPath, name)
}

func (p *PathVault) CreateTeam(ctx context.Context, team mgmt.Team) error {
	return p.withLock(ctx, func(actor mgmt.Actor) error {
		return commonCreateTeam(p.repoPath, actor, team)
	})
}

func (p *PathVault) UpdateTeam(ctx context.Context, team mgmt.Team) error {
	return p.withLock(ctx, func(actor mgmt.Actor) error {
		return commonUpdateTeam(p.repoPath, actor, team)
	})
}

func (p *PathVault) DeleteTeam(ctx context.Context, name string) error {
	return p.withLock(ctx, func(actor mgmt.Actor) error {
		return commonDeleteTeam(p.repoPath, actor, name)
	})
}

func (p *PathVault) AddTeamMember(ctx context.Context, team, email string, admin bool) error {
	return p.withLock(ctx, func(actor mgmt.Actor) error {
		return commonAddTeamMember(p.repoPath, actor, team, email, admin)
	})
}

func (p *PathVault) RemoveTeamMember(ctx context.Context, team, email string) error {
	return p.withLock(ctx, func(actor mgmt.Actor) error {
		return commonRemoveTeamMember(p.repoPath, actor, team, email)
	})
}

func (p *PathVault) SetTeamAdmin(ctx context.Context, team, email string, admin bool) error {
	return p.withLock(ctx, func(actor mgmt.Actor) error {
		return commonSetTeamAdmin(p.repoPath, actor, team, email, admin)
	})
}

func (p *PathVault) AddTeamRepository(ctx context.Context, team, repoURL string) error {
	return p.withLock(ctx, func(actor mgmt.Actor) error {
		return commonAddTeamRepository(p.repoPath, actor, team, repoURL)
	})
}

func (p *PathVault) RemoveTeamRepository(ctx context.Context, team, repoURL string) error {
	return p.withLock(ctx, func(actor mgmt.Actor) error {
		return commonRemoveTeamRepository(p.repoPath, actor, team, repoURL)
	})
}

func (p *PathVault) SetAssetInstallation(ctx context.Context, assetName string, target InstallTarget) error {
	return p.withLock(ctx, func(actor mgmt.Actor) error {
		return commonSetAssetInstallation(p.repoPath, actor, assetName, target)
	})
}

func (p *PathVault) ClearAssetInstallations(ctx context.Context, assetName string) error {
	return p.withLock(ctx, func(actor mgmt.Actor) error {
		return commonClearAssetInstallations(p.repoPath, actor, assetName)
	})
}

func (p *PathVault) RecordUsageEvents(ctx context.Context, events []mgmt.UsageEvent) error {
	return p.withLock(ctx, func(actor mgmt.Actor) error {
		return commonRecordUsageEvents(p.repoPath, actor, events)
	})
}

func (p *PathVault) GetUsageStats(ctx context.Context, filter mgmt.UsageFilter) (*mgmt.UsageSummary, error) {
	return mgmt.SummarizeUsage(p.repoPath, filter)
}

func (p *PathVault) QueryAuditEvents(ctx context.Context, filter mgmt.AuditFilter) ([]mgmt.AuditEvent, error) {
	return mgmt.QueryAuditEvents(p.repoPath, filter)
}

// withLock wraps a mutating op: acquire flock, resolve actor, run fn.
func (p *PathVault) withLock(ctx context.Context, fn func(actor mgmt.Actor) error) error {
	if err := ensureSxDir(p.repoPath); err != nil {
		return err
	}
	fl, err := p.acquirePathLock(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = fl.Unlock() }()

	actor, err := p.CurrentActor(ctx)
	if err != nil {
		return err
	}
	return fn(actor)
}
