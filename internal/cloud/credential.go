// Package cloud holds the client-side state for the "sx cloud" relay
// feature: credential storage, WebSocket dialing, and the MCP dispatcher
// loop.
//
// A "relay" is an sx process (this one) serving a public MCP endpoint
// hosted by skills.new. The credential persisted here is the machine
// token minted during “sx cloud connect“ — it authenticates this sx
// instance's long-lived WebSocket connection back to pulse.
//
// Storage layout:
//
//   - Non-secret metadata (relay base URL, GID) lives in a TOML file at
//     “<config-dir>/cloud.toml“ with 0600 permissions.
//   - The machine token itself is stored in the OS keyring (macOS
//     Keychain, Windows Credential Manager, freedesktop Secret Service
//     on Linux) so a full-disk backup or a misconfigured backup agent
//     can't leak it. If the keyring is unavailable (headless Linux
//     without a Secret Service, containerized envs, etc.) we fall back
//     to storing the token in the same TOML file with a visible
//     warning — better than refusing to work on CI runners.
package cloud

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/zalando/go-keyring"

	"github.com/sleuth-io/sx/internal/logger"
	"github.com/sleuth-io/sx/internal/utils"
)

// CredentialFileName is the TOML file that holds the active relay's
// metadata. Stored under the user's sx config dir with 0600 permissions
// so nothing else on the machine can read even the fallback-mode token.
const CredentialFileName = "cloud.toml"

// keyringService is the logical "app" key we use when talking to the
// OS keyring. “account“ is the relay GID so multiple relays on one
// machine coexist (only one is active, but revoking a stale entry via
// the OS keychain UI works even when that GID isn't the current one).
const keyringService = "sx-cloud-relay"

// ErrNoCredential is returned by Load when no cloud credential has been
// persisted yet. Callers check with errors.Is so "not attached" can be
// distinguished from genuine I/O failures.
var ErrNoCredential = errors.New("no sx cloud credential; run `sx cloud connect`")

// Credential is the full persisted state for an attached relay. The
// RelayGID is derived from RelayBaseURL (“.../relay/<gid>/“) and stored
// explicitly so we don't have to re-parse the URL every time.
type Credential struct {
	// RelayBaseURL is the base URL that includes the relay GID, e.g.
	// ``https://app.skills.new/relay/SR.../``. The trailing slash is
	// preserved when written but not required on read.
	RelayBaseURL string `toml:"relay_base_url"`

	// RelayGID is the URL-facing opaque id ("SR..."). Derived from
	// RelayBaseURL but stored for convenience.
	RelayGID string `toml:"relay_gid"`

	// MachineToken is the plaintext bearer token. NEVER serialized to
	// TOML in the normal path — it lives in the OS keyring. Only
	// written to the TOML file when the keyring is unavailable, and
	// even then the file is 0600.
	MachineToken string `toml:"machine_token,omitempty"`
}

// TokenStore abstracts the OS keyring so tests can swap in an
// in-memory implementation. Exported rather than unexported because
// the sibling test-helper package (“internal/cloud/cloudtest“) needs
// to implement it for tests in neighbouring packages (e.g.
// “internal/commands“). Production code has no reason to install a
// custom store; “SetTokenStore“ is the only hook.
type TokenStore interface {
	Set(account, token string) error
	Get(account string) (string, error)
	Delete(account string) error
}

// ErrTokenNotFound is returned by TokenStore.Get when no token is
// stored for the account. Separate from ErrNoCredential so Load can
// tell "TOML exists but keyring forgot the token" (bad state) from
// "user never attached" (clean state).
var ErrTokenNotFound = errors.New("token not found in keyring")

// osKeyring is the production TokenStore backed by go-keyring.
type osKeyring struct{}

func (osKeyring) Set(account, token string) error {
	return keyring.Set(keyringService, account, token)
}

func (osKeyring) Get(account string) (string, error) {
	v, err := keyring.Get(keyringService, account)
	if err == nil {
		return v, nil
	}
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrTokenNotFound
	}
	if isKeyringUnavailable(err) {
		// Headless Linux (no dbus, no org.freedesktop.secrets) returns a
		// raw ``exec`` / ``dbus`` error here rather than ``ErrNotFound``.
		// Treat that the same as "not found" so ``Load`` can fall back to
		// the inline TOML token written by ``Save``'s fallback path.
		// Without this, sx is unusable in containers / CI / minimal dev
		// environments even though ``Save`` already handles the write-side.
		return "", ErrTokenNotFound
	}
	return v, err
}

// isKeyringUnavailable returns true for the "no secret-service backend
// installed" class of errors. Kept conservative on purpose — real
// keyring failures (corrupt entry, permission denied on an existing
// entry) still bubble up so operators notice.
//
// String matching is unfortunate but unavoidable: ``go-keyring``'s Linux
// backend (``zalando/go-keyring``) doesn't expose a typed sentinel for "no
// backend available" — it just returns whatever D-Bus surfaces. A library
// or OS upgrade that reworded these messages would silently break the
// fallback to on-disk storage; the unit tests for this function pin each
// expected substring so we get a regression failure instead. If you add a
// new marker, add a matching test case in credential_test.go.
func isKeyringUnavailable(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	markers := []string{
		// Secret-service D-Bus name not registered (no gnome-keyring /
		// KWallet / secret-service daemon running).
		"org.freedesktop.secrets",
		// go-keyring's Linux backend shells out to ``dbus-launch`` when
		// no session bus is present. Missing binary → this error.
		"dbus-launch",
		// No session bus at all (container / minimal image).
		"DBUS_SESSION_BUS_ADDRESS",
	}
	for _, m := range markers {
		if strings.Contains(msg, m) {
			return true
		}
	}
	return false
}

func (osKeyring) Delete(account string) error {
	err := keyring.Delete(keyringService, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

// activeTokenStore is swapped out by tests via SetTokenStore.
// Production callers never touch it directly.
var activeTokenStore TokenStore = osKeyring{}

// SetTokenStore replaces the active keyring backend and returns a
// function that restores the previous one. Intended for test helpers
// in “internal/cloud/cloudtest“ — production code has no reason to
// call this.
func SetTokenStore(ts TokenStore) (restore func()) {
	prev := activeTokenStore
	activeTokenStore = ts
	return func() { activeTokenStore = prev }
}

// Path returns the absolute path to the credential metadata file,
// creating the config dir if it doesn't already exist.
func Path() (string, error) {
	dir, err := utils.GetConfigDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("failed to create sx config dir: %w", err)
	}
	return filepath.Join(dir, CredentialFileName), nil
}

// Load reads the stored credential, looking up the machine token in the
// OS keyring (or taking it from the TOML file when the keyring is
// unavailable). Returns ErrNoCredential if nothing has been persisted.
func Load() (*Credential, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path) // #nosec G304 -- path is derived from a fixed location under the sx config dir
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNoCredential
		}
		return nil, fmt.Errorf("failed to open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}

	var cred Credential
	if err := toml.Unmarshal(data, &cred); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", path, err)
	}

	// Prefer the keyring. If the keyring has the token, use it. If
	// it's specifically "not found" but the TOML has an inline token
	// (fallback-mode install), honor that. Anything else from the
	// keyring is a hard error so we don't accidentally authenticate
	// with a stale fallback token while the keyring is healthy.
	keyringToken, kerr := activeTokenStore.Get(cred.RelayGID)
	switch {
	case kerr == nil:
		cred.MachineToken = keyringToken
	case errors.Is(kerr, ErrTokenNotFound):
		if cred.MachineToken == "" {
			return nil, fmt.Errorf("credential metadata present at %s but token is missing from the keyring; "+
				"re-run `sx cloud connect`", path)
		}
		// fallback-mode: TOML inline token is authoritative.
	default:
		return nil, fmt.Errorf("failed to read token from keyring: %w", kerr)
	}
	return &cred, nil
}

// Save writes the credential atomically: the token goes to the OS
// keyring (with a TOML-file fallback on keyring failure), and the URL +
// GID land in the TOML file with 0600 permissions. If the TOML rename
// fails after the keyring write succeeded, we roll the keyring back so
// the caller is never left with a keyring entry that points at a relay
// the on-disk metadata doesn't know about.
func Save(cred *Credential) error {
	if cred == nil {
		return errors.New("nil credential")
	}
	if err := cred.Validate(); err != nil {
		return err
	}
	path, err := Path()
	if err != nil {
		return err
	}

	log := logger.Get()
	tomlCred := *cred
	savedToKeyring := false
	if kerr := activeTokenStore.Set(cred.RelayGID, cred.MachineToken); kerr != nil {
		// Keyring unavailable — fall back to storing the token inline
		// in the TOML file. 0600 still protects against other users on
		// the same box, but we warn prominently so operators running
		// on a headful laptop know something's off.
		log.Warn("sx cloud: OS keyring unavailable; falling back to on-disk token storage. "+
			"Backups of ~/.config/sx/cloud.toml will contain the token.",
			"error", kerr)
		// Fall through: tomlCred.MachineToken stays set, so the TOML
		// encoder writes it.
	} else {
		// Keyring succeeded — never put the token in the TOML file.
		tomlCred.MachineToken = ""
		savedToKeyring = true
	}

	// Roll the keyring back if the on-disk metadata write fails. The
	// defer fires only when ``returningErr`` is non-nil at the end of
	// the function — keeping the happy path allocation-free.
	var returningErr error
	defer func() {
		if returningErr != nil && savedToKeyring {
			if derr := activeTokenStore.Delete(cred.RelayGID); derr != nil {
				log.Warn("sx cloud: failed to roll back keyring after Save error",
					"error", derr, "gid", cred.RelayGID)
			}
		}
	}()

	buf, err := marshalToml(&tomlCred)
	if err != nil {
		returningErr = fmt.Errorf("failed to encode credential: %w", err)
		return returningErr
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".cloud.toml.tmp-*")
	if err != nil {
		returningErr = fmt.Errorf("failed to create temp file: %w", err)
		return returningErr
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if err := os.Chmod(tmpName, 0o600); err != nil {
		_ = tmp.Close()
		returningErr = fmt.Errorf("failed to chmod temp credential file: %w", err)
		return returningErr
	}
	if _, err := tmp.Write(buf); err != nil {
		_ = tmp.Close()
		returningErr = fmt.Errorf("failed to write temp credential file: %w", err)
		return returningErr
	}
	if err := tmp.Close(); err != nil {
		returningErr = fmt.Errorf("failed to close temp credential file: %w", err)
		return returningErr
	}
	if err := os.Rename(tmpName, path); err != nil {
		returningErr = fmt.Errorf("failed to move credential into place: %w", err)
		return returningErr
	}
	return nil
}

// RevokeKeyringEntry removes the keyring entry for a specific relay GID
// without touching the on-disk metadata. Used by the “--force“ swap
// path to clean up the old relay's token before saving the new one, so
// an orphan doesn't accumulate in the user's OS keychain.
//
// No-op if the keyring doesn't know this GID. Errors are returned so
// callers can log them, but the caller should continue regardless —
// a stale keyring entry is a secret-hygiene regression, not a
// correctness blocker.
func RevokeKeyringEntry(relayGID string) error {
	if relayGID == "" {
		return errors.New("relay_gid is required")
	}
	return activeTokenStore.Delete(relayGID)
}

// Delete removes the persisted credential: TOML metadata file AND the
// keyring entry for this relay's GID. Returns nil if nothing was stored.
func Delete() error {
	// Load first so we know the GID for the keyring entry; if the TOML
	// is missing, there's nothing to delete on either side.
	cred, err := Load()
	if err != nil {
		if errors.Is(err, ErrNoCredential) {
			return nil
		}
		// Couldn't load cleanly (e.g. the keyring is down). Still make
		// a best-effort attempt to delete the TOML so the next
		// ``connect`` starts fresh; an orphaned keyring entry is not a
		// correctness problem.
		logger.Get().Warn("sx cloud revoke: failed to load credential before delete", "error", err)
		return deleteMetadataFile()
	}

	// Delete keyring first so a crash after this line never leaves us
	// with keyring-present / file-absent (which Load would then treat
	// as "no credential" while the OS still shows a stale entry).
	if kerr := activeTokenStore.Delete(cred.RelayGID); kerr != nil {
		logger.Get().Warn("sx cloud revoke: failed to delete keyring entry", "error", kerr, "gid", cred.RelayGID)
	}
	return deleteMetadataFile()
}

func deleteMetadataFile() error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("failed to remove %s: %w", path, err)
	}
	return nil
}

// Validate returns an error if required fields are missing.
func (c *Credential) Validate() error {
	if c.RelayBaseURL == "" {
		return errors.New("relay_base_url is required")
	}
	if c.RelayGID == "" {
		return errors.New("relay_gid is required")
	}
	if c.MachineToken == "" {
		return errors.New("machine_token is required")
	}
	return nil
}

// WebSocketURL returns the URL sx dials to subscribe for inbound MCP
// requests. Derived from “RelayBaseURL“ by swapping “http(s)“ for
// “ws(s)“ and appending “ws/“.
func (c *Credential) WebSocketURL() (string, error) {
	u, err := url.Parse(c.RelayBaseURL)
	if err != nil {
		return "", fmt.Errorf("invalid relay_base_url: %w", err)
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	default:
		return "", fmt.Errorf("relay_base_url must be http(s): got scheme %q", u.Scheme)
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/ws/"
	return u.String(), nil
}

// ParseRelayURL takes a base URL like
// “https://app.skills.new/relay/SR.../“ and returns the cleaned URL
// and the relay GID segment. Used by “sx cloud attach“ to validate
// what the user pasted.
func ParseRelayURL(raw string) (baseURL string, relayGID string, err error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", "", fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", "", fmt.Errorf("URL scheme must be http or https, got %q", u.Scheme)
	}
	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	// Expect ``relay/<gid>`` possibly with extra trailing segments. We
	// accept the canonical ``/relay/<gid>/`` base as well as
	// ``/relay/<gid>/mcp/assets/`` (the URL shown on the success page).
	idx := -1
	for i, s := range segments {
		if s == "relay" && i+1 < len(segments) {
			idx = i + 1
			break
		}
	}
	if idx == -1 || segments[idx] == "" {
		return "", "", errors.New("URL does not contain a /relay/<gid>/ segment")
	}
	relayGID = segments[idx]
	u.Path = "/" + strings.Join(segments[:idx+1], "/") + "/"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), relayGID, nil
}

// marshalToml is separated so tests can exercise the full round-trip.
func marshalToml(cred *Credential) ([]byte, error) {
	var b strings.Builder
	enc := toml.NewEncoder(&b)
	if err := enc.Encode(cred); err != nil {
		return nil, err
	}
	return []byte(b.String()), nil
}
