package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/sleuth-io/sx/internal/buildinfo"
	"github.com/sleuth-io/sx/internal/version"
)

// UpdateInfo tells the frontend whether a newer app release exists.
// Zero-valued when the check is inconclusive — the app never nags on
// network failures or dev builds.
type UpdateInfo struct {
	Available bool   `json:"available"`
	Version   string `json:"version"`
	URL       string `json:"url"`
}

const releasesURL = "https://api.github.com/repos/sleuth-io/sx/releases/latest"

// CheckForUpdate compares this build against the latest GitHub release.
// Notify-only: the frontend shows a quiet banner linking to the download.
func (a *App) CheckForUpdate() UpdateInfo {
	current := strings.TrimPrefix(buildinfo.Version, "v")
	if current == "" || current == "dev" {
		return UpdateInfo{}
	}

	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(a.ctx, http.MethodGet, releasesURL, nil)
	if err != nil {
		return UpdateInfo{}
	}
	req.Header.Set("User-Agent", buildinfo.GetUserAgent())
	resp, err := client.Do(req)
	if err != nil {
		return UpdateInfo{}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return UpdateInfo{}
	}

	var release struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return UpdateInfo{}
	}
	latest := strings.TrimPrefix(release.TagName, "v")
	if latest == "" {
		return UpdateInfo{}
	}

	currentVer, err := version.Parse(current)
	if err != nil {
		return UpdateInfo{}
	}
	latestVer, err := version.Parse(latest)
	if err != nil {
		return UpdateInfo{}
	}
	if latestVer.Compare(currentVer) <= 0 {
		return UpdateInfo{}
	}
	return UpdateInfo{Available: true, Version: latest, URL: release.HTMLURL}
}
