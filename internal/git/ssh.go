package git

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// ConvertToSSH converts HTTPS git URLs to SSH format
// Example: https://github.com/owner/repo.git → git@github.com:owner/repo.git
func ConvertToSSH(httpsURL string) (string, error) {
	if !IsHTTPSURL(httpsURL) {
		return "", fmt.Errorf("not an HTTPS URL: %s", httpsURL)
	}

	// Remove https:// prefix
	url := strings.TrimPrefix(httpsURL, "https://")

	// Extract host and path
	// Expected format: host/owner/repo.git or host/owner/repo
	parts := strings.SplitN(url, "/", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid URL format: %s", httpsURL)
	}

	host := parts[0]
	path := parts[1]

	// Check if this is a known git service
	if !isKnownGitService(host) {
		return "", fmt.Errorf("unsupported git host: %s", host)
	}

	// Build SSH URL: git@host:path
	sshURL := fmt.Sprintf("git@%s:%s", host, path)

	return sshURL, nil
}

// IsSSHURL checks if URL is in SSH format (git@host:path)
func IsSSHURL(url string) bool {
	// SSH URLs have format: git@host:path or user@host:path
	return strings.Contains(url, "@") && strings.Contains(url, ":") && !strings.Contains(url, "://")
}

// IsHTTPSURL checks if URL is in HTTPS format
func IsHTTPSURL(url string) bool {
	return strings.HasPrefix(url, "https://")
}

// ValidateSSHKey validates SSH key file exists, readable, and has proper permissions
func ValidateSSHKey(keyPath string) error {
	// Check if file exists
	info, err := os.Stat(keyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("SSH key file not found: %s", keyPath)
		}
		return fmt.Errorf("SSH key file not readable: %w", err)
	}

	// Check if it's a regular file
	if !info.Mode().IsRegular() {
		return fmt.Errorf("SSH key path is not a regular file: %s", keyPath)
	}

	// Check permissions (warn if too permissive)
	perm := info.Mode().Perm()
	if perm&0077 != 0 {
		// Permission bits for group/other are set, which is too permissive
		fmt.Fprintf(os.Stderr, "Warning: SSH key has permissive permissions (%04o), recommend 0600 for %s\n", perm, keyPath)
	}

	return nil
}

// buildSSHCommand returns the GIT_SSH_COMMAND value for the given key path
func buildSSHCommand(keyPath string) string {
	// Use IdentitiesOnly=yes to prevent ssh-agent interference
	// Use StrictHostKeyChecking=accept-new to handle first-time host keys automatically
	return fmt.Sprintf("ssh -i %s -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new", keyPath)
}

// isKnownGitService checks if the host is a known git service
func isKnownGitService(host string) bool {
	knownHosts := []string{
		"github.com",
		"gitlab.com",
		"bitbucket.org",
		"codeberg.org",
	}

	// Also support self-hosted instances with these domains
	for _, known := range knownHosts {
		if host == known || strings.HasSuffix(host, "."+known) {
			return true
		}
	}

	// Support generic git hosting patterns
	// Match hosts like git.company.com, gitlab.company.com, etc.
	gitHostPattern := regexp.MustCompile(`^(git|gitlab|github|bitbucket)\.`)
	return gitHostPattern.MatchString(host)
}
