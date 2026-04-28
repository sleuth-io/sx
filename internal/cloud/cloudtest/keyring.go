// Package cloudtest exposes test-only helpers for the “cloud“
// package. Lives in a sibling subpackage so the production-compiled
// “internal/cloud“ package has no “testing“ import and doesn't
// leak test flags (“-test.v“, “-test.run“, etc.) into the sx
// binary.
//
// Only call site is test files in neighbouring packages (e.g.
// “internal/commands“). Tests inside “internal/cloud“ itself use
// the package-scoped helpers in “credential_test.go“ directly.
package cloudtest

import (
	"maps"
	"sync"
	"testing"

	"github.com/sleuth-io/sx/internal/cloud"
)

// Keyring is an in-memory “cloud.TokenStore“ for use in tests that
// exercise the credential layer end-to-end. Methods mirror the
// “cloud.TokenStore“ interface; “Entries“ returns a snapshot of
// the current contents for assertions.
type Keyring struct {
	mu      sync.Mutex
	entries map[string]string
}

// Entries returns a snapshot of the keyring contents. Safe to call
// concurrently with the production code under test.
func (k *Keyring) Entries() map[string]string {
	k.mu.Lock()
	defer k.mu.Unlock()
	out := make(map[string]string, len(k.entries))
	maps.Copy(out, k.entries)
	return out
}

// Set implements “cloud.TokenStore“.
func (k *Keyring) Set(account, token string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.entries == nil {
		k.entries = make(map[string]string)
	}
	k.entries[account] = token
	return nil
}

// Get implements “cloud.TokenStore“.
func (k *Keyring) Get(account string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	v, ok := k.entries[account]
	if !ok {
		return "", cloud.ErrTokenNotFound
	}
	return v, nil
}

// Delete implements “cloud.TokenStore“.
func (k *Keyring) Delete(account string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.entries, account)
	return nil
}

// InstallKeyring replaces the cloud package's active token store with
// a fresh in-memory Keyring for the duration of one test, restoring
// the previous store on cleanup.
func InstallKeyring(t *testing.T) *Keyring {
	t.Helper()
	k := &Keyring{entries: make(map[string]string)}
	restore := cloud.SetTokenStore(k)
	t.Cleanup(restore)
	return k
}
