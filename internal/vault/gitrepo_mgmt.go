package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sleuth-io/sx/internal/cache"
	"github.com/sleuth-io/sx/internal/logger"
	"github.com/sleuth-io/sx/internal/manifest"
	"github.com/sleuth-io/sx/internal/mgmt"
)

// usagePushInterval is the minimum gap between successive
// commit-and-push cycles for usage events. Picked to balance
// freshness against git history volume — at one hour, even an
// always-on user produces at most ~24 usage commits per day.
var usagePushInterval = time.Hour

// runInVaultTx is the git vault's transactional wrapper for management
// mutations. Contract:
//
//  1. Acquires the vault's flock (blocks concurrent in-process writes).
//  2. Clones or syncs the vault repo.
//  3. Resolves the caller's actor via git config user.email.
//  4. Runs fn against the locked working tree. fn must only write to
//     sx.toml and .sx/ files (audit/usage JSONL streams), and must not
//     commit or push; this wrapper handles that.
//  5. Stages sx.toml and .sx/ specifically (not the whole tree) so stale
//     install.sh/README.md or partial asset writes don't ride along.
//  6. Commits with commitMsg and pushes. On push rejection (another
//     process raced us), rebases local commits onto the new remote head
//     and retries once. Both errors are wrapped so troubleshooting
//     shows which leg failed.
//
// Any path in the staging list that doesn't exist yet is skipped —
// critical for empty vaults, where the very first `sx team create`
// runs before sx.toml has been written.
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

	for _, rel := range []string{manifest.FileName, ".sx"} {
		if _, statErr := os.Stat(filepath.Join(g.repoPath, rel)); os.IsNotExist(statErr) {
			continue
		}
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

	return g.pushWithRebaseRetry(ctx)
}

// pushWithRebaseRetry pushes the staged commit, rebasing and retrying on
// non-fast-forward rejection. Each iteration is: push; on failure, rebase
// onto the remote head and try again. Under high concurrency a single
// retry isn't enough — between our rebase and our next push, a third
// process can push again and reject us — so the loop runs up to
// maxAttempts full push attempts with maxAttempts-1 rebases between
// them. Anything beyond that is probably a genuine conflict or a broken
// remote and surfaces to the caller.
func (g *GitVault) pushWithRebaseRetry(ctx context.Context) error {
	const maxAttempts = 3
	var lastPushErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		pushErr := g.gitClient.Push(ctx, g.repoPath)
		if pushErr == nil {
			return nil
		}
		lastPushErr = pushErr
		if attempt == maxAttempts {
			break
		}
		if err := g.gitClient.PullRebase(ctx, g.repoPath); err != nil {
			return fmt.Errorf("push failed: %w; rebase also failed: %w", pushErr, err)
		}
		// Jittered backoff: 50–250ms after attempt 1, 100–500ms after
		// attempt 2. The jitter desynchronizes retry storms from other
		// writers that lost the same race. Returns immediately if the
		// context is done.
		backoff := time.Duration(attempt) * (50*time.Millisecond + time.Duration(rand.Intn(200))*time.Millisecond)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
	}
	return fmt.Errorf("push failed after %d attempts: %w", maxAttempts, lastPushErr)
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

func (g *GitVault) ListBots(ctx context.Context) ([]mgmt.Bot, error) {
	if err := g.cloneOrUpdate(ctx); err != nil {
		return nil, err
	}
	return commonListBots(g.repoPath)
}

func (g *GitVault) GetBot(ctx context.Context, name string) (*mgmt.Bot, error) {
	if err := g.cloneOrUpdate(ctx); err != nil {
		return nil, err
	}
	return commonGetBot(g.repoPath, name)
}

// CreateBot on a git vault returns ("", err) — file-based vaults are
// identity-only for bots and never issue API keys.
func (g *GitVault) CreateBot(ctx context.Context, bot mgmt.Bot) (string, error) {
	return "", g.runInVaultTx(ctx, "Create bot "+bot.Name, func(root string, actor mgmt.Actor) error {
		return commonCreateBot(root, actor, bot)
	})
}

func (g *GitVault) UpdateBot(ctx context.Context, bot mgmt.Bot) error {
	return g.runInVaultTx(ctx, "Update bot "+bot.Name, func(root string, actor mgmt.Actor) error {
		return commonUpdateBot(root, actor, bot)
	})
}

func (g *GitVault) DeleteBot(ctx context.Context, name string) error {
	return g.runInVaultTx(ctx, "Delete bot "+name, func(root string, actor mgmt.Actor) error {
		return commonDeleteBot(root, actor, name)
	})
}

func (g *GitVault) AddBotTeam(ctx context.Context, bot, team string) error {
	msg := fmt.Sprintf("Add bot %s to team %s", bot, team)
	return g.runInVaultTx(ctx, msg, func(root string, actor mgmt.Actor) error {
		return commonAddBotTeam(root, actor, bot, team)
	})
}

func (g *GitVault) RemoveBotTeam(ctx context.Context, bot, team string) error {
	msg := fmt.Sprintf("Remove bot %s from team %s", bot, team)
	return g.runInVaultTx(ctx, msg, func(root string, actor mgmt.Actor) error {
		return commonRemoveBotTeam(root, actor, bot, team)
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

// RecordUsageEvents appends usage events to the local JSONL log and,
// at most once per usagePushInterval, commits and pushes the queued
// .sx/usage files to the remote.
//
// Under active use this can run hundreds of times per hour, and a
// commit-per-call would spam the git history and turn a background
// observation channel into a noisy multi-writer workload. Originally
// the design relied on the next management mutation (team, install,
// etc.) running runInVaultTx to sweep .sx/ into its commit, but
// users who only consume assets never trigger such a mutation, so
// their usage events would accumulate locally and never reach the
// remote. Throttled push closes that gap without per-call spam.
//
// Queries on the same machine see events immediately because
// GetUsageStats reads the local working tree regardless of push
// state.
func (g *GitVault) RecordUsageEvents(ctx context.Context, events []mgmt.UsageEvent) error {
	if len(events) == 0 {
		return nil
	}
	fileLock, err := g.acquireFileLock(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}
	defer func() { _ = fileLock.Unlock() }()

	// Sync to remote BEFORE dirtying the working tree. If we appended
	// first and pulled later, a remote commit touching the same monthly
	// JSONL would make `git pull` refuse the merge ("local changes
	// would be overwritten") and pushes would stall — under multi-
	// writer contention this is the common case, not the edge case.
	// This matches runInVaultTx's clone-then-mutate ordering.
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
	if err := commonRecordUsageEvents(g.repoPath, actor, events); err != nil {
		return err
	}

	if err := g.maybePushUsage(ctx); err != nil {
		// Push failures shouldn't drop the events — they remain on
		// disk and will be retried on the next call. Log and return
		// nil so the hook caller doesn't error out.
		logger.Get().Warn("usage push failed; events remain queued locally",
			"error", err,
			"repo_path", g.repoPath)
	}
	return nil
}

// usagePushSentinelPath returns the path of the per-clone marker
// whose mtime records the last successful usage push. The sentinel
// lives under the cache dir (not the working tree) so it isn't
// accidentally swept into a runInVaultTx commit and propagated to
// other clones.
func (g *GitVault) usagePushSentinelPath() (string, error) {
	cacheDir, err := cache.GetCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, "git-repos", filepath.Base(g.repoPath)+".usage-pushed"), nil
}

// maybePushUsage commits any pending .sx/usage changes and, when
// more than usagePushInterval has elapsed since the last successful
// push, pushes the local branch to the remote. The caller must
// already hold the vault file lock and have already synced the
// working tree via cloneOrUpdate.
//
// Committing is unconditional even inside the throttle window:
// leaving uncommitted appends in the working tree across fresh
// `report-usage` processes would wedge the next pull as soon as a
// concurrent writer touched the same monthly file. Throttling only
// the push keeps the tree clean while still bounding remote-history
// volume.
func (g *GitVault) maybePushUsage(ctx context.Context) error {
	sentinel, err := g.usagePushSentinelPath()
	if err != nil {
		return err
	}

	// Stage only .sx/usage so a stale or partial write elsewhere in
	// the working tree doesn't ride along with the usage commit.
	usageDir := filepath.Join(g.repoPath, mgmt.UsageDirName)
	if _, statErr := os.Stat(usageDir); !os.IsNotExist(statErr) {
		if err := g.gitClient.Add(ctx, g.repoPath, mgmt.UsageDirName); err != nil {
			return err
		}
		hasChanges, err := g.gitClient.HasStagedChanges(ctx, g.repoPath)
		if err != nil {
			return err
		}
		if hasChanges {
			if err := g.gitClient.Commit(ctx, g.repoPath, "Record usage events"); err != nil {
				return err
			}
		}
	}

	// Throttle only the push. Any local commits made above stay in
	// place; pushWithRebaseRetry will batch them on the next call
	// past the throttle window.
	if info, statErr := os.Stat(sentinel); statErr == nil {
		if time.Since(info.ModTime()) < usagePushInterval {
			return nil
		}
	} else if !os.IsNotExist(statErr) {
		return statErr
	}

	if err := g.pushWithRebaseRetry(ctx); err != nil {
		return err
	}
	return touchSentinel(sentinel)
}

// touchSentinel updates (or creates) the sentinel file with the
// current mtime. Always creates-then-Chtimes so a transient Chtimes
// failure on an existing file isn't masked by a successful OpenFile —
// a stale mtime would expire the throttle on every subsequent call.
// Errors are returned to the caller.
func touchSentinel(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	now := time.Now()
	return os.Chtimes(path, now, now)
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
// into the new mgmt.UsageEvent type. A malformed line is logged and
// skipped; the rest of the batch still flushes so one bad event never
// drops a whole queue of good ones.
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
			logger.Get().Warn("usage line malformed; dropping",
				"line_number", i+1,
				"error", err,
				"line", truncateForLog(line, 200))
			continue
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
				// Keep the event but stamp it with the Unix epoch so it
				// doesn't fall inside any `--since Nd` window and skew
				// recent-usage totals. `time.Now()` here would make a
				// month-old malformed event show up as "today"; the
				// sentinel makes these events visible to stats queries
				// that request all-time data and invisible to narrower
				// windows.
				logger.Get().Warn("usage event timestamp unparseable; stamping with epoch sentinel",
					"timestamp", raw.Timestamp,
					"asset_name", raw.AssetName,
					"asset_version", raw.AssetVersion,
					"actor", raw.Actor,
					"error", err)
				ev.Timestamp = time.Unix(0, 0).UTC()
			} else {
				ev.Timestamp = parsed
			}
		}
		events = append(events, ev)
	}
	return events, nil
}

// truncateForLog bounds the amount of potentially-untrusted payload that
// ends up in a log line.
func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "…"
}
