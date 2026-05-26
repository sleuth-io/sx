package git

import (
	"net/url"
	"strings"
)

// RemoteAuthInfo describes the host and transport parsed from a git remote URL.
type RemoteAuthInfo struct {
	Host   string
	Scheme string
	HTTP   bool
	SSH    bool
}

// ParseRemoteAuthInfo extracts the auth-relevant host and transport from a
// git remote URL. It supports HTTP(S) URLs, ssh:// URLs, and scp-like SSH
// remotes such as git@github.com:owner/repo.git.
func ParseRemoteAuthInfo(repoURL string) RemoteAuthInfo {
	repoURL = strings.TrimSpace(repoURL)
	u, err := url.Parse(repoURL)
	if err == nil && u.Host != "" {
		scheme := strings.ToLower(u.Scheme)
		switch scheme {
		case "http", "https":
			return RemoteAuthInfo{Host: u.Host, Scheme: scheme, HTTP: true}
		case "ssh", "git+ssh":
			return RemoteAuthInfo{Host: u.Host, Scheme: scheme, SSH: true}
		}
	}
	if host := scpLikeSSHHost(repoURL); host != "" {
		return RemoteAuthInfo{Host: host, SSH: true}
	}
	return RemoteAuthInfo{}
}

// LooksLikeHTTPRemote reports whether repoURL uses an HTTP(S) scheme even when
// the URL is malformed enough that ParseRemoteAuthInfo cannot extract a host.
func LooksLikeHTTPRemote(repoURL string) bool {
	repoURL = strings.ToLower(strings.TrimSpace(repoURL))
	return strings.HasPrefix(repoURL, "https://") || strings.HasPrefix(repoURL, "http://")
}

// DefaultHTTPSAuthUsername returns a reasonable basic-auth username for a git
// HTTPS token when the caller did not configure one explicitly.
func DefaultHTTPSAuthUsername(host, explicit string) string {
	if explicit = strings.TrimSpace(explicit); explicit != "" {
		return explicit
	}
	host = strings.ToLower(strings.TrimSpace(host))
	switch {
	case host == "gitlab.com" || strings.HasSuffix(host, ".gitlab.com"):
		return "oauth2"
	default:
		return "x-access-token"
	}
}

func scpLikeSSHHost(repoURL string) string {
	if strings.Contains(repoURL, "://") {
		return ""
	}
	at := strings.IndexByte(repoURL, '@')
	colon := strings.IndexByte(repoURL, ':')
	if at <= 0 || colon <= at+1 {
		return ""
	}
	return strings.TrimSpace(repoURL[at+1 : colon])
}
