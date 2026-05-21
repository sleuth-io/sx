package commands

import (
	"context"
	"errors"
	"fmt"

	"github.com/sleuth-io/sx/internal/cache"
	"github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/logger"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// fetchLockFile fetches and parses the lock file for a single profile,
// using the on-disk ETag cache when the vault reports notModified. It
// does not touch the shared status component, so callers running it
// from goroutines (see loadActiveLockFiles) don't fight for the
// spinner. Returns ErrLockFileNotFound for the pristine "new vault"
// case so callers can short-circuit without treating it as a hard
// failure.
func fetchLockFile(ctx context.Context, vault vaultpkg.Vault, cfg *config.Config) (*lockfile.LockFile, error) {
	// Key the cache by VaultIdentifier rather than raw RepositoryURL so
	// legacy Sleuth profiles (ServerURL set, RepositoryURL empty) get
	// distinct cache paths. Two such profiles in the same active set
	// would otherwise share cache entries and — under the parallel
	// fan-out in loadActiveLockFiles — race on the same cache file.
	cacheKey := cfg.VaultIdentifier()
	cachedETag, _ := cache.LoadETag(cacheKey)
	lockFileData, newETag, notModified, err := vault.GetLockFile(ctx, cachedETag)
	if err != nil {
		if errors.Is(err, vaultpkg.ErrLockFileNotFound) {
			return nil, vaultpkg.ErrLockFileNotFound
		}
		return nil, fmt.Errorf("failed to fetch lock file: %w", err)
	}

	if notModified {
		lockFileData, err = cache.LoadLockFile(cacheKey)
		if err != nil {
			return nil, fmt.Errorf("failed to load cached lock file: %w", err)
		}
	} else {
		saveLockFileToCache(cacheKey, newETag, lockFileData)
	}

	lf, err := lockfile.Parse(lockFileData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse lock file: %w", err)
	}
	if err := lf.Validate(); err != nil {
		return nil, fmt.Errorf("lock file validation failed: %w", err)
	}
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
