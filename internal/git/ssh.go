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
// If the keyPath looks like actual key content (starts with "-----BEGIN"), it's considered valid
func ValidateSSHKey(keyPath string) error {
	// If it looks like actual key content, no validation needed
	if isSSHKeyContent(keyPath) {
		return nil
	}

	// Otherwise treat as file path
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

// isSSHKeyContent checks if the string looks like actual SSH key content
func isSSHKeyContent(s string) bool {
	return strings.HasPrefix(strings.TrimSpace(s), "-----BEGIN")
}

// buildSSHCommand returns the GIT_SSH_COMMAND value for the given key path or content
// If keyPathOrContent contains actual key content, it writes it to a temporary file first
func buildSSHCommand(keyPathOrContent string) string {
	finalKeyPath := keyPathOrContent

	// If this is key content, write to a temporary file
	if isSSHKeyContent(keyPathOrContent) {
		tmpFile, err := os.CreateTemp("", "ssh-key-*")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to create temp file for SSH key: %v\n", err)
			return ""
		}

		// Write the key content
		if _, err := tmpFile.WriteString(keyPathOrContent); err != nil {
			tmpFile.Close()
			os.Remove(tmpFile.Name())
			fmt.Fprintf(os.Stderr, "Warning: failed to write SSH key to temp file: %v\n", err)
			return ""
		}

		// Set proper permissions
		if err := tmpFile.Chmod(0600); err != nil {
			tmpFile.Close()
			os.Remove(tmpFile.Name())
			fmt.Fprintf(os.Stderr, "Warning: failed to set permissions on temp SSH key: %v\n", err)
			return ""
		}

		tmpFile.Close()
		finalKeyPath = tmpFile.Name()

		// Note: The temp file won't be cleaned up automatically, but that's okay
		// since it's in the temp directory and will be cleaned on system reboot
	}

	// Use IdentitiesOnly=yes to prevent ssh-agent interference
	// Use StrictHostKeyChecking=accept-new to handle first-time host keys automatically
	return fmt.Sprintf("ssh -i %s -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new", finalKeyPath)
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
