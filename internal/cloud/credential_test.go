package cloud

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

// fakeKeyring is an in-memory tokenStore for tests. The real keyring
// is not reachable from CI; swapping this in via “withFakeKeyring“
// keeps every test hermetic and lets us also exercise the "keyring
// unavailable" fallback path without mucking with the OS keychain.
type fakeKeyring struct {
	mu      sync.Mutex
	entries map[string]string
	// setErr, if non-nil, is returned from set() — used to simulate
	// "keyring unavailable so the caller should fall back to inline
	// TOML storage."
	setErr error
}

func newFakeKeyring() *fakeKeyring {
	return &fakeKeyring{entries: make(map[string]string)}
}

func (f *fakeKeyring) Set(account, token string) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries[account] = token
	return nil
}

func (f *fakeKeyring) Get(account string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.entries[account]
	if !ok {
		return "", ErrTokenNotFound
	}
	return v, nil
}

func (f *fakeKeyring) Delete(account string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.entries, account)
	return nil
}

// withFakeKeyring swaps in a fresh in-memory tokenStore for the duration
// of one test, restoring the previous store (real OS keyring in prod,
// but in tests it can chain with another fake) on cleanup.
func withFakeKeyring(t *testing.T) *fakeKeyring {
	t.Helper()
	prev := activeTokenStore
	fake := newFakeKeyring()
	activeTokenStore = fake
	t.Cleanup(func() { activeTokenStore = prev })
	return fake
}

func TestParseRelayURL(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantURL string
		wantGID string
		wantErr bool
	}{
		{
			name:    "canonical base URL",
			input:   "https://app.skills.new/relay/SRabc123/",
			wantURL: "https://app.skills.new/relay/SRabc123/",
			wantGID: "SRabc123",
		},
		{
			name:    "trailing mcp path is trimmed to base",
			input:   "https://app.skills.new/relay/SRabc123/mcp/assets/",
			wantURL: "https://app.skills.new/relay/SRabc123/",
			wantGID: "SRabc123",
		},
		{
			name:    "http (dev) accepted",
			input:   "http://dev.pulse.sleuth.io/relay/SRdev/",
			wantURL: "http://dev.pulse.sleuth.io/relay/SRdev/",
			wantGID: "SRdev",
		},
		{
			name:    "query string stripped",
			input:   "https://app.skills.new/relay/SRabc/mcp/assets/?foo=bar",
			wantURL: "https://app.skills.new/relay/SRabc/",
			wantGID: "SRabc",
		},
		{
			name:    "missing relay segment",
			input:   "https://app.skills.new/some/other/path/",
			wantErr: true,
		},
		{
			name:    "non-http scheme",
			input:   "ftp://app.skills.new/relay/SRabc/",
			wantErr: true,
		},
		{
			name:    "empty GID",
			input:   "https://app.skills.new/relay//",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotURL, gotGID, err := ParseRelayURL(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got URL=%q GID=%q", gotURL, gotGID)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotURL != tc.wantURL {
				t.Errorf("URL mismatch: got %q want %q", gotURL, tc.wantURL)
			}
			if gotGID != tc.wantGID {
				t.Errorf("GID mismatch: got %q want %q", gotGID, tc.wantGID)
			}
		})
	}
}

func TestWebSocketURL(t *testing.T) {
	cases := []struct {
		base string
		want string
	}{
		{"https://app.skills.new/relay/SRabc/", "wss://app.skills.new/relay/SRabc/ws/"},
		{"http://dev.pulse.sleuth.io/relay/SRdev/", "ws://dev.pulse.sleuth.io/relay/SRdev/ws/"},
		// Trailing slash optional on input.
		{"https://app.skills.new/relay/SRxyz", "wss://app.skills.new/relay/SRxyz/ws/"},
	}
	for _, tc := range cases {
		t.Run(tc.base, func(t *testing.T) {
			c := &Credential{RelayBaseURL: tc.base}
			got, err := c.WebSocketURL()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestSaveAndLoadRoundTrip_UsesKeyring(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SX_CONFIG_DIR", tmp)
	fake := withFakeKeyring(t)

	cred := &Credential{
		RelayBaseURL: "https://app.skills.new/relay/SRabc/",
		RelayGID:     "SRabc",
		MachineToken: "machine-secret-xyz",
	}
	if err := Save(cred); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Token should be in the keyring, NOT in the TOML file on disk.
	path, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	diskBytes, err := os.ReadFile(path) // #nosec G304 -- path is the test-scoped config file
	if err != nil {
		t.Fatalf("read TOML: %v", err)
	}
	if contains(diskBytes, "machine-secret-xyz") {
		t.Errorf("TOML file leaked the token: %s", diskBytes)
	}
	if fake.entries["SRabc"] != "machine-secret-xyz" {
		t.Errorf("keyring missing token: %+v", fake.entries)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("expected 0600, got %o", info.Mode().Perm())
		}
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got == nil {
		t.Fatal("Load returned nil")
	}
	if *got != *cred {
		t.Errorf("round-trip mismatch: got %+v want %+v", got, cred)
	}
}

func TestSave_KeyringFailure_FallsBackToTOML(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SX_CONFIG_DIR", tmp)
	fake := withFakeKeyring(t)
	fake.setErr = errors.New("no secret service available")

	cred := &Credential{
		RelayBaseURL: "https://app.skills.new/relay/SRabc/",
		RelayGID:     "SRabc",
		MachineToken: "fallback-token",
	}
	if err := Save(cred); err != nil {
		t.Fatalf("Save should succeed even when keyring is down: %v", err)
	}

	// The TOML file should now contain the token (fallback mode).
	path, _ := Path()
	diskBytes, err := os.ReadFile(path) // #nosec G304 -- test-scoped config file
	if err != nil {
		t.Fatalf("read TOML: %v", err)
	}
	if !contains(diskBytes, "fallback-token") {
		t.Errorf("TOML file missing fallback token: %s", diskBytes)
	}

	// Load should return the inline token since the keyring still says
	// "not found".
	fake.setErr = nil
	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.MachineToken != "fallback-token" {
		t.Errorf("token: got %q want %q", got.MachineToken, "fallback-token")
	}
}

func TestLoad_InconsistentState_ReportsClearly(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SX_CONFIG_DIR", tmp)
	withFakeKeyring(t)

	// Simulate a corrupt state: metadata file exists (no inline token)
	// but the keyring knows nothing. Should surface a clear error.
	path, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	// #nosec G306 -- 0600 is the sane default for secret-adjacent files
	if err := os.WriteFile(path, []byte(
		"relay_base_url = \"https://x/relay/SRghost/\"\nrelay_gid = \"SRghost\"\n",
	), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err = Load()
	if err == nil {
		t.Fatal("expected error on metadata-without-keyring state")
	}
	if !contains([]byte(err.Error()), "missing from the keyring") {
		t.Errorf("expected keyring-specific error, got: %v", err)
	}
}

func TestLoadMissingReturnsSentinelError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SX_CONFIG_DIR", tmp)
	withFakeKeyring(t)
	got, err := Load()
	if !errors.Is(err, ErrNoCredential) {
		t.Fatalf("Load: want ErrNoCredential, got err=%v cred=%v", err, got)
	}
	if got != nil {
		t.Errorf("expected nil credential with ErrNoCredential, got %+v", got)
	}
}

func TestDeleteIsIdempotent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SX_CONFIG_DIR", tmp)
	fake := withFakeKeyring(t)
	if err := Delete(); err != nil {
		t.Fatalf("Delete missing: %v", err)
	}
	cred := &Credential{
		RelayBaseURL: "https://app.skills.new/relay/SRabc/",
		RelayGID:     "SRabc",
		MachineToken: "x",
	}
	if err := Save(cred); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := Delete(); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, still := fake.entries["SRabc"]; still {
		t.Errorf("keyring entry not cleaned up on delete: %+v", fake.entries)
	}
	got, err := Load()
	if !errors.Is(err, ErrNoCredential) {
		t.Fatalf("Load after delete: want ErrNoCredential, got err=%v cred=%+v", err, got)
	}
	if got != nil {
		t.Errorf("expected nil after delete, got %+v", got)
	}
}

// writeOnlyFailKeyring lets tests simulate "keyring accepts set, then
// we inspect whether rollback fired on a subsequent failure." The
// fallback-set path shouldn't trigger rollback, so we track calls
// instead of failing outright.
type countingKeyring struct {
	*fakeKeyring
	setCalls    int
	deleteCalls int
}

func (c *countingKeyring) Set(account, token string) error {
	c.setCalls++
	return c.fakeKeyring.Set(account, token)
}

func (c *countingKeyring) Delete(account string) error {
	c.deleteCalls++
	return c.fakeKeyring.Delete(account)
}

func TestSave_RollsBackKeyringOnDiskFailure(t *testing.T) {
	// Point SX_CONFIG_DIR at a path that doesn't exist AND can't be
	// created, so Path() → MkdirAll fails. The keyring write will
	// never be attempted here because Path() fails first — which is
	// not what we want to test. Instead, make the config dir a file
	// so ``os.CreateTemp`` fails when Save tries to stage the TOML.
	tmp := t.TempDir()
	configAsFile := filepath.Join(tmp, "config-not-a-dir")
	if err := os.WriteFile(configAsFile, []byte("nope"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// Inside the config dir, put another file with the exact name of
	// what Path() would create a subdir for, so MkdirAll succeeds but
	// CreateTemp fails because the parent isn't writable. The
	// straightforward path: use a read-only directory as the config
	// dir.
	readonlyDir := filepath.Join(tmp, "readonly")
	if err := os.Mkdir(readonlyDir, 0o500); err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(readonlyDir, 0o700) })
	t.Setenv("SX_CONFIG_DIR", readonlyDir)

	prev := activeTokenStore
	fake := newFakeKeyring()
	counting := &countingKeyring{fakeKeyring: fake}
	activeTokenStore = counting
	t.Cleanup(func() { activeTokenStore = prev })

	cred := &Credential{
		RelayBaseURL: "https://app.skills.new/relay/SRabc/",
		RelayGID:     "SRabc",
		MachineToken: "token-that-should-be-rolled-back",
	}
	err := Save(cred)
	if err == nil {
		t.Fatal("expected Save to fail on read-only config dir")
	}
	if counting.setCalls != 1 {
		t.Errorf("expected 1 keyring set call, got %d", counting.setCalls)
	}
	if counting.deleteCalls != 1 {
		t.Errorf("expected 1 keyring delete call (rollback), got %d", counting.deleteCalls)
	}
	if _, still := fake.entries["SRabc"]; still {
		t.Errorf("keyring not rolled back: %+v", fake.entries)
	}
}

func TestSave_SkipsRollbackWhenKeyringFallback(t *testing.T) {
	// When the keyring is unavailable, Save falls back to writing the
	// token inline in TOML. A subsequent disk failure must NOT
	// ``delete`` the keyring, because the set() never succeeded — and
	// calling delete on an unavailable keyring would either be a
	// no-op or noisy.
	tmp := t.TempDir()
	readonlyDir := filepath.Join(tmp, "readonly")
	if err := os.Mkdir(readonlyDir, 0o500); err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(readonlyDir, 0o700) })
	t.Setenv("SX_CONFIG_DIR", readonlyDir)

	prev := activeTokenStore
	fake := newFakeKeyring()
	fake.setErr = errors.New("no keyring")
	counting := &countingKeyring{fakeKeyring: fake}
	activeTokenStore = counting
	t.Cleanup(func() { activeTokenStore = prev })

	cred := &Credential{
		RelayBaseURL: "https://app.skills.new/relay/SRabc/",
		RelayGID:     "SRabc",
		MachineToken: "fallback-token",
	}
	_ = Save(cred) // expected to fail; we care about the side effects
	if counting.deleteCalls != 0 {
		t.Errorf("unexpected keyring delete when set() had failed: %d", counting.deleteCalls)
	}
}

func TestRevokeKeyringEntry(t *testing.T) {
	withFakeKeyring(t)

	if err := RevokeKeyringEntry(""); err == nil {
		t.Error("expected error for empty GID")
	}

	// Set one up, then revoke — should succeed and clear the entry.
	if err := activeTokenStore.Set("SRabc", "secret"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := RevokeKeyringEntry("SRabc"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := activeTokenStore.Get("SRabc"); !errors.Is(err, ErrTokenNotFound) {
		t.Errorf("expected entry gone, got err=%v", err)
	}

	// Revoking a non-existent GID is a no-op; fake keyring returns nil.
	if err := RevokeKeyringEntry("SRunknown"); err != nil {
		t.Errorf("revoke-missing should be no-op: %v", err)
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		cred    Credential
		wantErr bool
	}{
		{"all set", Credential{RelayBaseURL: "u", RelayGID: "g", MachineToken: "t"}, false},
		{"missing url", Credential{RelayGID: "g", MachineToken: "t"}, true},
		{"missing gid", Credential{RelayBaseURL: "u", MachineToken: "t"}, true},
		{"missing token", Credential{RelayBaseURL: "u", RelayGID: "g"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cred.Validate()
			if tc.wantErr && err == nil {
				t.Error("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// contains is a tiny helper to avoid importing bytes in test-only code.
// Using strings.Contains(string(b), ...) would do, but this keeps the
// byte domain and plays nicely with future binary assertions.
func contains(hay []byte, needle string) bool {
	n := []byte(needle)
	for i := 0; i+len(n) <= len(hay); i++ {
		match := true
		for j := range n {
			if hay[i+j] != n[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// mustPath is used inside tests that need the Path helper and know
// “SX_CONFIG_DIR“ is set. Not the usual "mustFoo" convenience — just
// keeps the test bodies short.
func mustPath(t *testing.T) string {
	t.Helper()
	path, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if dir := filepath.Dir(path); dir == "" {
		t.Fatalf("empty Path dir")
	}
	return path
}

// ensureTempConfig pre-flights the test-scoped config dir so higher-
// level tests don't have to do it themselves. Currently unused by the
// committed tests, but present so upstream refactors (adding a helper
// for new test cases) don't have to reinvent the bootstrap.
func ensureTempConfig(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("SX_CONFIG_DIR", tmp)
	return tmp
}

var (
	_ = mustPath
	_ = ensureTempConfig
)

func TestIsKeyringUnavailable(t *testing.T) {
	// Lock in every error-message marker the function recognizes. The
	// upstream library (``zalando/go-keyring``) doesn't expose typed
	// sentinels for "no backend available", so this matcher is the only
	// thing keeping the on-disk fallback path alive on headless Linux.
	// A library upgrade that reworded any of these would silently break
	// containerized cloud serve — these test cases turn that into a
	// loud regression instead.
	cases := []struct {
		name  string
		err   error
		match bool
	}{
		{name: "nil_error", err: nil, match: false},
		{
			name:  "secret_service_dbus_name_missing",
			err:   errors.New("The name org.freedesktop.secrets was not provided by any .service files"),
			match: true,
		},
		{
			name:  "dbus_launch_binary_missing",
			err:   errors.New("exec: \"dbus-launch\": executable file not found in $PATH"),
			match: true,
		},
		{
			name:  "no_session_bus",
			err:   errors.New("DBUS_SESSION_BUS_ADDRESS is not set"),
			match: true,
		},
		{
			name:  "real_failure_passes_through",
			err:   errors.New("permission denied"),
			match: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isKeyringUnavailable(tc.err); got != tc.match {
				t.Fatalf("isKeyringUnavailable(%v) = %v, want %v", tc.err, got, tc.match)
			}
		})
	}
}
