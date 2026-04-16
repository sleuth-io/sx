package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sleuth-io/sx/internal/constants"
	"github.com/sleuth-io/sx/internal/logger"
	"github.com/sleuth-io/sx/internal/mgmt"
)

// runInVaultTx is the git vault's transactional wrapper for management
// mutations. Contract:
//
//  1. Acquires the vault's flock (blocks concurrent in-process writes).
//  2. Clones or syncs the vault repo.
//  3. Resolves the caller's actor via git config user.email.
//  4. Runs fn against the locked working tree. fn must only write to
//     .sx/ files (teams/installations/audit/usage) and sx.lock, and must
//     not commit or push; this wrapper handles that.
//  5. Stages .sx/ and sx.lock specifically (not the whole tree) so stale
//     install.sh/README.md or partial asset writes don't ride along.
//  6. Commits with commitMsg and pushes. On push rejection (another
//     process raced us), rebases local commits onto the new remote head
//     and retries once. Both errors are wrapped so troubleshooting
//     shows which leg failed.
func (g *GitVault) runInVaultTx(ctx context.Context, commitMsg string, fn func(vaultRoot string, actor mgmt.Actor) error) error {
	fileLock, err := g.acquireFileLock(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}
	defer func() { _ = fileLock.Unlock() }()

	if err := g.cloneOrUpdate(ctx); err != nil {
		return fmt.Errorf("failed to clone/update repository: %w", err)
	}
	if err := ensureSxDir(g.repoPath); err != nil {
		return err
	}

	actor, err := mgmt.CurrentGitActor(ctx, g.repoPath)
	if err != nil {
		return err
	}

	if err := fn(g.repoPath, actor); err != nil {
		return err
	}

	// Stage only the management files this function is allowed to touch.
	// Using explicit paths (not `git add .`) means a concurrent partial
	// asset write won't be swept into a management commit.
	for _, rel := range []string{constants.SkillLockFile, ".sx"} {
		if err := g.gitClient.Add(ctx, g.repoPath, rel); err != nil {
			return err
		}
	}

	hasChanges, err := g.gitClient.HasStagedChanges(ctx, g.repoPath)
	if err != nil {
		return err
	}
	if !hasChanges {
		return nil
	}
	if err := g.gitClient.Commit(ctx, g.repoPath, commitMsg); err != nil {
		return err
	}

	if pushErr := g.gitClient.Push(ctx, g.repoPath); pushErr != nil {
		if rebaseErr := g.gitClient.PullRebase(ctx, g.repoPath); rebaseErr != nil {
			return fmt.Errorf("push failed: %w; rebase also failed: %w", pushErr, rebaseErr)
		}
		if retryErr := g.gitClient.Push(ctx, g.repoPath); retryErr != nil {
			return fmt.Errorf("push failed after rebase: %w", retryErr)
		}
	}
	return nil
}

// CurrentActor resolves the caller's identity via git config.
func (g *GitVault) CurrentActor(ctx context.Context) (mgmt.Actor, error) {
	return mgmt.CurrentGitActor(ctx, g.repoPath)
}

func (g *GitVault) ListTeams(ctx context.Context) ([]mgmt.Team, error) {
	if err := g.cloneOrUpdate(ctx); err != nil {
		return nil, err
	}
	return commonListTeams(g.repoPath)
}

func (g *GitVault) GetTeam(ctx context.Context, name string) (*mgmt.Team, error) {
	if err := g.cloneOrUpdate(ctx); err != nil {
		return nil, err
	}
	return commonGetTeam(g.repoPath, name)
}

func (g *GitVault) CreateTeam(ctx context.Context, team mgmt.Team) error {
	return g.runInVaultTx(ctx, "Create team "+team.Name, func(root string, actor mgmt.Actor) error {
		return commonCreateTeam(root, actor, team)
	})
}

func (g *GitVault) UpdateTeam(ctx context.Context, team mgmt.Team) error {
	return g.runInVaultTx(ctx, "Update team "+team.Name, func(root string, actor mgmt.Actor) error {
		return commonUpdateTeam(root, actor, team)
	})
}

func (g *GitVault) DeleteTeam(ctx context.Context, name string) error {
	return g.runInVaultTx(ctx, "Delete team "+name, func(root string, actor mgmt.Actor) error {
		return commonDeleteTeam(root, actor, name)
	})
}

func (g *GitVault) AddTeamMember(ctx context.Context, team, email string, admin bool) error {
	msg := fmt.Sprintf("Add %s to team %s", email, team)
	return g.runInVaultTx(ctx, msg, func(root string, actor mgmt.Actor) error {
		return commonAddTeamMember(root, actor, team, email, admin)
	})
}

func (g *GitVault) RemoveTeamMember(ctx context.Context, team, email string) error {
	msg := fmt.Sprintf("Remove %s from team %s", email, team)
	return g.runInVaultTx(ctx, msg, func(root string, actor mgmt.Actor) error {
		return commonRemoveTeamMember(root, actor, team, email)
	})
}

func (g *GitVault) SetTeamAdmin(ctx context.Context, team, email string, admin bool) error {
	verb := "Grant"
	if !admin {
		verb = "Revoke"
	}
	msg := fmt.Sprintf("%s admin for %s on team %s", verb, email, team)
	return g.runInVaultTx(ctx, msg, func(root string, actor mgmt.Actor) error {
		return commonSetTeamAdmin(root, actor, team, email, admin)
	})
}

func (g *GitVault) AddTeamRepository(ctx context.Context, team, repoURL string) error {
	msg := "Add repo to team " + team
	return g.runInVaultTx(ctx, msg, func(root string, actor mgmt.Actor) error {
		return commonAddTeamRepository(root, actor, team, repoURL)
	})
}

func (g *GitVault) RemoveTeamRepository(ctx context.Context, team, repoURL string) error {
	msg := "Remove repo from team " + team
	return g.runInVaultTx(ctx, msg, func(root string, actor mgmt.Actor) error {
		return commonRemoveTeamRepository(root, actor, team, repoURL)
	})
}

func (g *GitVault) SetAssetInstallation(ctx context.Context, assetName string, target InstallTarget) error {
	msg := fmt.Sprintf("Install %s to %s", assetName, target.Describe())
	return g.runInVaultTx(ctx, msg, func(root string, actor mgmt.Actor) error {
		return commonSetAssetInstallation(root, actor, assetName, target)
	})
}

func (g *GitVault) ClearAssetInstallations(ctx context.Context, assetName string) error {
	msg := "Clear installations for " + assetName
	return g.runInVaultTx(ctx, msg, func(root string, actor mgmt.Actor) error {
		return commonClearAssetInstallations(root, actor, assetName)
	})
}

func (g *GitVault) RecordUsageEvents(ctx context.Context, events []mgmt.UsageEvent) error {
	if len(events) == 0 {
		return nil
	}
	msg := fmt.Sprintf("usage: %d events", len(events))
	return g.runInVaultTx(ctx, msg, func(root string, actor mgmt.Actor) error {
		return commonRecordUsageEvents(root, actor, events)
	})
}

func (g *GitVault) GetUsageStats(ctx context.Context, filter mgmt.UsageFilter) (*mgmt.UsageSummary, error) {
	if err := g.cloneOrUpdate(ctx); err != nil {
		return nil, err
	}
	return mgmt.SummarizeUsage(g.repoPath, filter)
}

func (g *GitVault) QueryAuditEvents(ctx context.Context, filter mgmt.AuditFilter) ([]mgmt.AuditEvent, error) {
	if err := g.cloneOrUpdate(ctx); err != nil {
		return nil, err
	}
	return mgmt.QueryAuditEvents(g.repoPath, filter)
}

// parseUsageJSONL converts a JSONL payload produced by stats.FlushQueue
// into the new mgmt.UsageEvent type.
func parseUsageJSONL(jsonlData string) ([]mgmt.UsageEvent, error) {
	var events []mgmt.UsageEvent
	for i, line := range strings.Split(jsonlData, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var raw struct {
			AssetName    string `json:"asset_name"`
			AssetVersion string `json:"asset_version"`
			AssetType    string `json:"asset_type"`
			Timestamp    string `json:"timestamp"`
			Actor        string `json:"actor"`
		}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			return nil, fmt.Errorf("malformed usage line %d: %w", i+1, err)
		}
		ev := mgmt.UsageEvent{
			Actor:        raw.Actor,
			AssetName:    raw.AssetName,
			AssetVersion: raw.AssetVersion,
			AssetType:    raw.AssetType,
		}
		if raw.Timestamp != "" {
			parsed, err := time.Parse(time.RFC3339, raw.Timestamp)
			if err != nil {
				logger.Get().Warn("skipping usage event with malformed timestamp",
					"timestamp", raw.Timestamp, "error", err)
				continue
			}
			ev.Timestamp = parsed
		}
		events = append(events, ev)
	}
	return events, nil
}
