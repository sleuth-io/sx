package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/sleuth-io/sx/internal/utils"
)

// GetCacheDir returns the platform-specific cache directory for sx
func GetCacheDir() (string, error) {
	// Check for environment override (support both new and legacy)
	if cacheDir := os.Getenv("SX_CACHE_DIR"); cacheDir != "" {
		return cacheDir, nil
	}
	if cacheDir := os.Getenv("SKILLS_CACHE_DIR"); cacheDir != "" {
		return cacheDir, nil
	}

	// Use os.UserCacheDir() with platform-specific fallbacks
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		// Fallback to platform-specific defaults
		cacheDir, err = getFallbackCacheDir()
		if err != nil {
			return "", fmt.Errorf("failed to determine cache directory: %w", err)
		}
	}

	return filepath.Join(cacheDir, "sx"), nil
}

// getFallbackCacheDir returns platform-specific fallback cache directories
func getFallbackCacheDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(homeDir, "Library", "Caches"), nil
	case "linux":
		xdgCache := os.Getenv("XDG_CACHE_HOME")
		if xdgCache != "" {
			return xdgCache, nil
		}
		return filepath.Join(homeDir, ".cache"), nil
	case "windows":
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData != "" {
			return localAppData, nil
		}
		return filepath.Join(homeDir, "AppData", "Local"), nil
	default:
		return filepath.Join(homeDir, ".cache"), nil
	}
}

// GetAssetCacheDir returns the directory for caching assets
func GetAssetCacheDir() (string, error) {
	cacheDir, err := GetCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, "assets"), nil
}

// GetArtifactCacheDir is an alias for GetAssetCacheDir for backwards compatibility
// Deprecated: use GetAssetCacheDir instead
func GetArtifactCacheDir() (string, error) {
	return GetAssetCacheDir()
}

// GetGitReposCacheDir returns the directory for caching git repositories
func GetGitReposCacheDir() (string, error) {
	cacheDir, err := GetCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, "git-repos"), nil
}

// GetLockFileCacheDir returns the directory for caching lock files
func GetLockFileCacheDir() (string, error) {
	cacheDir, err := GetCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, "lockfiles"), nil
}

// EnsureCacheDirs creates all necessary cache directories
func EnsureCacheDirs() error {
	dirs := []func() (string, error){
		GetCacheDir,
		GetAssetCacheDir,
		GetGitReposCacheDir,
		GetLockFileCacheDir,
	}

	for _, dirFunc := range dirs {
		dir, err := dirFunc()
		if err != nil {
			return err
		}
		if err := utils.EnsureDir(dir); err != nil {
			return fmt.Errorf("failed to create cache directory %s: %w", dir, err)
		}
	}

	return nil
}

// GetAssetCachePath returns the cache path for a specific asset
func GetAssetCachePath(name, version string) (string, error) {
	assetCacheDir, err := GetAssetCacheDir()
	if err != nil {
		return "", err
	}
	// Use sanitized name for directory safety
	safeName := filepath.Base(filepath.Clean(name))
	return filepath.Join(assetCacheDir, safeName, version+".zip"), nil
}

// GetGitRepoCachePath returns the cache path for a git repository
func GetGitRepoCachePath(repoURL string) (string, error) {
	gitReposDir, err := GetGitReposCacheDir()
	if err != nil {
		return "", err
	}
	urlHash := utils.URLHash(repoURL)
	return filepath.Join(gitReposDir, urlHash), nil
}

// ClearAssetCache removes cached assets for cleanup
func ClearAssetCache() error {
	assetCacheDir, err := GetAssetCacheDir()
	if err != nil {
		return err
	}
	return os.RemoveAll(assetCacheDir)
}

// ETagCache stores ETags for lock files
type ETagCache struct {
	URL  string    `json:"url"`
	ETag string    `json:"etag"`
	Date time.Time `json:"date"`
}

// GetLockFileETagPath returns the path for storing lock file ETag
func GetLockFileETagPath(repoURL string) (string, error) {
	lockFileCacheDir, err := GetLockFileCacheDir()
	if err != nil {
		return "", err
	}

	// Hash the repo URL for filename
	urlHash := utils.URLHash(repoURL)
	return filepath.Join(lockFileCacheDir, urlHash+".etag.json"), nil
}

// LoadETag loads the cached ETag for a lock file
func LoadETag(repoURL string) (string, error) {
	etagPath, err := GetLockFileETagPath(repoURL)
	if err != nil {
		return "", err
	}

	if !utils.FileExists(etagPath) {
		return "", nil
	}

	data, err := os.ReadFile(etagPath)
	if err != nil {
		return "", err
	}

	var cache ETagCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return "", err
	}

	// Check if cache is stale (older than 7 days)
	if time.Since(cache.Date) > 7*24*time.Hour {
		return "", nil
	}

	return cache.ETag, nil
}

// SaveETag saves the ETag for a lock file
func SaveETag(repoURL, etag string) error {
	etagPath, err := GetLockFileETagPath(repoURL)
	if err != nil {
		return err
	}

	// Ensure directory exists
	if err := utils.EnsureDir(filepath.Dir(etagPath)); err != nil {
		return err
	}

	cache := ETagCache{
		URL:  repoURL,
		ETag: etag,
		Date: time.Now(),
	}

	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(etagPath, data, 0644)
}

// GetCachedLockFilePath returns path for cached lock file
func GetCachedLockFilePath(repoURL string) (string, error) {
	lockFileCacheDir, err := GetLockFileCacheDir()
	if err != nil {
		return "", err
	}
	urlHash := utils.URLHash(repoURL)
	return filepath.Join(lockFileCacheDir, urlHash+".lock"), nil
}

// SaveLockFile caches lock file to disk
func SaveLockFile(repoURL string, data []byte) error {
	path, err := GetCachedLockFilePath(repoURL)
	if err != nil {
		return err
	}
	if err := utils.EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// LoadLockFile loads cached lock file
func LoadLockFile(repoURL string) ([]byte, error) {
	path, err := GetCachedLockFilePath(repoURL)
	if err != nil {
		return nil, err
	}
	if !utils.FileExists(path) {
		return nil, os.ErrNotExist
	}
	return os.ReadFile(path)
}

// GetTrackerCacheDir returns the directory for tracking installed assets state
func GetTrackerCacheDir() (string, error) {
	cacheDir, err := GetCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, "installed-state"), nil
}

// GetTrackerCachePath returns the path for tracking installed assets
// scopeKey should be "global" or a hash/identifier for repo-scoped installs
func GetTrackerCachePath(scopeKey string) (string, error) {
	trackerDir, err := GetTrackerCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(trackerDir, scopeKey+".json"), nil
}

// SaveAssetToDisk caches an asset zip to disk
func SaveAssetToDisk(name, version string, data []byte) error {
	cachePath, err := GetAssetCachePath(name, version)
	if err != nil {
		return err
	}

	// Ensure directory exists
	if err := utils.EnsureDir(filepath.Dir(cachePath)); err != nil {
		return err
	}

	// Verify it's a valid zip before caching
	if !utils.IsZipFile(data) {
		return fmt.Errorf("not a valid zip file")
	}

	return os.WriteFile(cachePath, data, 0644)
}

// LoadAssetFromDisk loads a cached asset from disk
func LoadAssetFromDisk(name, version string) ([]byte, error) {
	cachePath, err := GetAssetCachePath(name, version)
	if err != nil {
		return nil, err
	}

	if !utils.FileExists(cachePath) {
		return nil, os.ErrNotExist
	}

	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, err
	}

	// Verify cached file is still a valid zip
	if !utils.IsZipFile(data) {
		// Corrupted cache, remove it
		os.Remove(cachePath)
		return nil, fmt.Errorf("cached file corrupted")
	}

	return data, nil
}

// InvalidateLockFileCache removes cached lock file and ETag for a repository URL
// This forces the next GetLockFile call to fetch fresh data from the backend
func InvalidateLockFileCache(repoURL string) error {
	// Remove lock file
	lockPath, err := GetCachedLockFilePath(repoURL)
	if err != nil {
		return err
	}
	if utils.FileExists(lockPath) {
		if err := os.Remove(lockPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove cached lock file: %w", err)
		}
	}

	// Remove ETag
	etagPath, err := GetLockFileETagPath(repoURL)
	if err != nil {
		return err
	}
	if utils.FileExists(etagPath) {
		if err := os.Remove(etagPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove cached etag: %w", err)
		}
	}

	return nil
}
