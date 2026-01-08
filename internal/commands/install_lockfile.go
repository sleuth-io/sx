package commands

import (
	"context"
	"fmt"

	"github.com/sleuth-io/sx/internal/cache"
	"github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/logger"
	"github.com/sleuth-io/sx/internal/ui/components"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// fetchLockFileWithCache fetches the lock file, using cache when possible.
// It handles ETag-based caching, parsing, and validation.
func fetchLockFileWithCache(ctx context.Context, vault vaultpkg.Vault, cfg *config.Config, status *components.Status) (*lockfile.LockFile, error) {
	status.Start("Fetching lock file")

	cachedETag, _ := cache.LoadETag(cfg.RepositoryURL)
	lockFileData, newETag, notModified, err := vault.GetLockFile(ctx, cachedETag)
	if err != nil {
		status.Fail("Failed to fetch lock file")
		return nil, fmt.Errorf("failed to fetch lock file: %w", err)
	}

	if notModified {
		lockFileData, err = cache.LoadLockFile(cfg.RepositoryURL)
		if err != nil {
			status.Fail("Failed to load cached lock file")
			return nil, fmt.Errorf("failed to load cached lock file: %w", err)
		}
	} else {
		saveLockFileToCache(cfg.RepositoryURL, newETag, lockFileData)
	}

	lf, err := parseLockFile(lockFileData, status)
	if err != nil {
		return nil, err
	}

	status.Clear()
	return lf, nil
}

// saveLockFileToCache saves the lock file and ETag to cache
func saveLockFileToCache(repoURL, etag string, data []byte) {
	log := logger.Get()
	if etag != "" {
		if err := cache.SaveETag(repoURL, etag); err != nil {
			log.Error("failed to save ETag", "error", err)
		}
	}
	if err := cache.SaveLockFile(repoURL, data); err != nil {
		log.Error("failed to cache lock file", "error", err)
	}
}

// parseLockFile parses and validates the lock file data
func parseLockFile(data []byte, status *components.Status) (*lockfile.LockFile, error) {
	lf, err := lockfile.Parse(data)
	if err != nil {
		status.Fail("Failed to parse lock file")
		return nil, fmt.Errorf("failed to parse lock file: %w", err)
	}

	if err := lf.Validate(); err != nil {
		status.Fail("Lock file validation failed")
		return nil, fmt.Errorf("lock file validation failed: %w", err)
	}

	return lf, nil
}
