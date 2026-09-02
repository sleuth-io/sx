package git

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
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

// inlineKeyFiles caches the temp file written for each distinct inline key
// content so one process writes the key once (not once per git command) and
// can remove every copy on exit via CleanupTempSSHKeys.
var (
	inlineKeyMu    sync.Mutex
	inlineKeyFiles = map[string]string{} // key content -> temp file path
)

// buildSSHCommand returns the GIT_SSH_COMMAND value for the given key path or content.
// If keyPathOrContent contains actual key content, it is written to a 0600
// temp file (once per process — see inlineKeyFiles) that CleanupTempSSHKeys
// removes; callers that run many git commands (a vault clone plus fetches)
// therefore never leave more than one copy of the key on disk, and a process
// that exits cleanly leaves none.
func buildSSHCommand(keyPathOrContent string) string {
	finalKeyPath := keyPathOrContent

	// If this is key content, write to a temporary file
	if isSSHKeyContent(keyPathOrContent) {
		path, err := inlineKeyFile(keyPathOrContent)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
			return ""
		}
		finalKeyPath = path
	}

	// Use IdentitiesOnly=yes to prevent ssh-agent interference
	// Use StrictHostKeyChecking=accept-new to handle first-time host keys automatically
	return fmt.Sprintf("ssh -i %s -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new", finalKeyPath)
}

// inlineKeyFile returns the temp file holding keyContent, writing it on
// first use. Ensures the content ends with a newline (ssh requires it) and
// sets 0600 before the file is closed.
func inlineKeyFile(keyContent string) (string, error) {
	keyContent = strings.TrimSpace(keyContent) + "\n"

	inlineKeyMu.Lock()
	defer inlineKeyMu.Unlock()
	if path, ok := inlineKeyFiles[keyContent]; ok {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
		delete(inlineKeyFiles, keyContent)
	}

	tmpFile, err := os.CreateTemp("", "ssh-key-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file for SSH key: %w", err)
	}
	if _, err := tmpFile.WriteString(keyContent); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("failed to write SSH key to temp file: %w", err)
	}
	if err := tmpFile.Chmod(0600); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("failed to set permissions on temp SSH key: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("failed to close temp SSH key: %w", err)
	}
	inlineKeyFiles[keyContent] = tmpFile.Name()
	return tmpFile.Name(), nil
}

// CleanupTempSSHKeys removes every temp file buildSSHCommand wrote for inline
// key content. Call it once on process exit — an inline key typically comes
// from a CI secret, and later steps in the same job (an agent with shell
// access, say) should not find a private key lying in the temp directory.
func CleanupTempSSHKeys() {
	inlineKeyMu.Lock()
	defer inlineKeyMu.Unlock()
	for content, path := range inlineKeyFiles {
		os.Remove(path)
		delete(inlineKeyFiles, content)
	}
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
