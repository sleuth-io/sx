package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/zalando/go-keyring"

	"github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/logger"
)

// Extension secrets (API 1.4.0, docs/app-plugins-spec.md). API keys and
// tokens an extension needs must never land in its plugin-data JSON —
// that file syncs with backups and is world-readable next to every
// other extension's state. Secrets go to the OS keyring (macOS
// Keychain, Windows Credential Manager, Secret Service on Linux),
// scoped per profile + extension + name so no extension can read
// another's entries. When the keyring is unavailable (headless Linux,
// containers) they fall back to a 0600 file, same trade-off as
// internal/cloud's relay token.

// pluginKeyringService is the logical "app" key in the OS keyring.
// Distinct from the sx-cloud-relay service so extension entries are
// recognizable (and revocable) in the OS keychain UI.
const pluginKeyringService = "sx-app-plugins"

var pluginSecretNamePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)

// maxPluginSecretBytes bounds one secret's value; this is for API keys
// and tokens, not payload storage.
const maxPluginSecretBytes = 4096

// pluginSecretStore abstracts the OS keyring so tests can swap in an
// in-memory implementation (CI runners have no keyring).
type pluginSecretStore interface {
	Set(account, value string) error
	Get(account string) (string, bool, error)
	Delete(account string) error
}

type osPluginSecretStore struct{}

func (osPluginSecretStore) Set(account, value string) error {
	return keyring.Set(pluginKeyringService, account, value)
}

func (osPluginSecretStore) Get(account string) (string, bool, error) {
	v, err := keyring.Get(pluginKeyringService, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

func (osPluginSecretStore) Delete(account string) error {
	err := keyring.Delete(pluginKeyringService, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

var activePluginSecretStore pluginSecretStore = osPluginSecretStore{}

// setPluginSecretStore swaps the keyring backend for tests; returns a
// restore function.
func setPluginSecretStore(s pluginSecretStore) (restore func()) {
	prev := activePluginSecretStore
	activePluginSecretStore = s
	return func() { activePluginSecretStore = prev }
}

func validatePluginSecretName(name string) error {
	if !pluginSecretNamePattern.MatchString(name) {
		return fmt.Errorf("invalid secret name %q", name)
	}
	return nil
}

// pluginSecretAccount namespaces a secret in the keyring:
// <profile>/<extension-id>/<name>. Profile is part of the key so two
// libraries' extensions never share credentials.
func pluginSecretAccount(id, name string) (string, error) {
	cfg, err := config.Load()
	if err != nil {
		return "", errors.New("no library configured")
	}
	profile := cfg.ProfileName
	if profile == "" {
		profile = "default"
	}
	return profile + "/" + id + "/" + name, nil
}

// PluginSecretGet returns a stored secret ("" when unset). The keyring
// is preferred; the fallback file is only consulted when the keyring
// has no entry, so a healthy keyring can never be shadowed by a stale
// on-disk value.
func (a *App) PluginSecretGet(id, name string) (string, error) {
	if err := validatePluginID(id); err != nil {
		return "", err
	}
	if err := validatePluginSecretName(name); err != nil {
		return "", err
	}
	account, err := pluginSecretAccount(id, name)
	if err != nil {
		return "", err
	}
	v, ok, kerr := activePluginSecretStore.Get(account)
	if kerr == nil && ok {
		return v, nil
	}
	if kerr != nil {
		logger.Get().Warn("extension secrets: keyring read failed, trying file fallback",
			"extension", id, "error", kerr)
	}
	fallback, err := a.readSecretFallback(id)
	if err != nil {
		return "", err
	}
	return fallback[name], nil
}

// PluginSecretSet stores a secret; an empty value deletes it. On
// keyring failure the value falls back to a 0600 file in the plugin
// data dir, with a visible warning (backups of that dir will contain
// the secret).
func (a *App) PluginSecretSet(id, name, value string) error {
	if err := validatePluginID(id); err != nil {
		return err
	}
	if err := validatePluginSecretName(name); err != nil {
		return err
	}
	if len(value) > maxPluginSecretBytes {
		return fmt.Errorf("secret exceeds %d bytes", maxPluginSecretBytes)
	}
	account, err := pluginSecretAccount(id, name)
	if err != nil {
		return err
	}
	if value == "" {
		// Delete from both homes so no copy survives.
		if kerr := activePluginSecretStore.Delete(account); kerr != nil {
			logger.Get().Warn("extension secrets: keyring delete failed",
				"extension", id, "error", kerr)
		}
		return a.updateSecretFallback(id, name, "")
	}
	if kerr := activePluginSecretStore.Set(account, value); kerr != nil {
		logger.Get().Warn("extension secrets: OS keyring unavailable; falling back to on-disk storage",
			"extension", id, "error", kerr)
		return a.updateSecretFallback(id, name, value)
	}
	// Keyring write succeeded — drop any stale fallback copy of this
	// name so the file never holds a secret the keyring also has.
	return a.updateSecretFallback(id, name, "")
}

// secretFallbackPath is the 0600 file holding an extension's secrets
// when the OS keyring is unavailable.
func (a *App) secretFallbackPath(id string) (string, error) {
	dir, err := a.pluginDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, id+".secrets.json"), nil
}

func (a *App) readSecretFallback(id string) (map[string]string, error) {
	path, err := a.secretFallbackPath(id)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path) // #nosec G304 -- path is derived from the sx config dir and a validated id
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("corrupt secrets fallback file %s: %w", path, err)
	}
	return out, nil
}

// updateSecretFallback sets or (with value=="") removes one name in the
// fallback file, deleting the file entirely when it empties out.
func (a *App) updateSecretFallback(id, name, value string) error {
	path, err := a.secretFallbackPath(id)
	if err != nil {
		return err
	}
	existing, err := a.readSecretFallback(id)
	if err != nil {
		return err
	}
	if value == "" {
		if _, had := existing[name]; !had {
			return nil
		}
		delete(existing, name)
	} else {
		existing[name] = value
	}
	if len(existing) == 0 {
		if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
			return rmErr
		}
		return nil
	}
	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	// 0600 is applied to the temp file BEFORE the rename: the secrets
	// file must never exist world-readable, even for one scheduler tick.
	return atomicWriteFileMode(path, data, 0o600)
}
