package hook

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
)

// ResolvedCommand holds the result of resolving a hook's command configuration
// to an absolute command string ready for installation.
type ResolvedCommand struct {
	Command string
}

// ContainsFile checks if a filename exists in the file list.
// It checks both exact match and filepath.Base fallback.
func ContainsFile(files []string, name string) bool {
	for _, f := range files {
		if f == name || filepath.Base(f) == name {
			return true
		}
	}
	return false
}

// IsZipFile returns true if the given arg matches a file path in the cached zip file list.
func IsZipFile(zipFiles []string, arg string) bool {
	return slices.Contains(zipFiles, arg)
}

// HasExtractableFiles returns true if the zip contains files beyond metadata.toml.
func HasExtractableFiles(zipData []byte) bool {
	files, err := utils.ListZipFiles(zipData)
	if err != nil {
		return false
	}
	for _, f := range files {
		if f != "metadata.toml" {
			return true
		}
	}
	return false
}

// CacheZipFiles returns the list of file paths in the zip for later path resolution.
// Returns nil on error.
func CacheZipFiles(zipData []byte) []string {
	files, err := utils.ListZipFiles(zipData)
	if err != nil {
		return nil
	}
	return files
}

// ErrUnsupportedEvent is returned when a hook asset's canonical event is not
// supported by the target client. Callers (typically a client's InstallAssets
// implementation) use errors.Is to distinguish this from real install
// failures and surface the result as StatusSkipped rather than StatusFailed.
//
// This is not a user error: the asset is well-formed and the user's
// configuration is correct; the target client simply has no lifecycle event
// to fire it from. For example, Gemini Code Assist does not implement
// `pre-compact` or `subagent-start`, so log hooks for those events are
// soft-skipped on Gemini and installed as usual on Claude Code.
var ErrUnsupportedEvent = errors.New("unsupported hook event")

// unsupportedEventErr is the concrete type returned by UnsupportedEventError.
// It carries Client and Event as structured fields so callers can build
// per-client summaries without parsing the message string. Callers should
// match it via errors.As, or detect the wrapped sentinel via errors.Is.
type unsupportedEventErr struct {
	Client string
	Event  string
}

func (e *unsupportedEventErr) Error() string {
	return fmt.Sprintf("%s %q on %s", ErrUnsupportedEvent.Error(), e.Event, e.Client)
}

func (e *unsupportedEventErr) Unwrap() error { return ErrUnsupportedEvent }

// UnsupportedEventError builds an error that wraps ErrUnsupportedEvent with
// the client and event captured as structured fields. Callers detect it via
// errors.Is(err, ErrUnsupportedEvent), and extract the fields via:
//
//	var ue *UnsupportedEventDetails
//	if errors.As(err, &ue) { ... ue.Client, ue.Event ... }
func UnsupportedEventError(clientName, event string) error {
	return &unsupportedEventErr{Client: clientName, Event: event}
}

// UnsupportedEventDetails is the public alias for the concrete type used with
// errors.As to recover the (Client, Event) pair from an UnsupportedEventError.
type UnsupportedEventDetails = unsupportedEventErr

// MapEvent maps a canonical hook event name to a client-native event name.
// It first checks the client-specific override map, then falls back to the
// standard eventMap. Returns the native event name and whether the event
// is supported.
func MapEvent(event string, eventMap map[string]string, clientOverrides map[string]any) (string, bool) {
	if clientOverrides != nil {
		if eventOverride, ok := clientOverrides["event"].(string); ok && eventOverride != "" {
			return eventOverride, true
		}
	}

	if nativeEvent, ok := eventMap[event]; ok {
		return nativeEvent, true
	}

	return "", false
}

// ResolveCommand resolves a hook's script-file or command+args configuration
// into an absolute command string. For script-file mode, it returns the
// absolute path to the script. For command mode, it joins the command with
// args, resolving any args that match zip files to absolute paths.
func ResolveCommand(hookCfg *metadata.HookConfig, installDir string, zipFiles []string) ResolvedCommand {
	if hookCfg.ScriptFile != "" {
		return ResolvedCommand{
			Command: filepath.Join(installDir, hookCfg.ScriptFile),
		}
	}

	cmd := hookCfg.Command
	if len(hookCfg.Args) > 0 {
		resolvedArgs := make([]string, len(hookCfg.Args))
		for i, arg := range hookCfg.Args {
			if IsZipFile(zipFiles, arg) {
				resolvedArgs[i] = filepath.Join(installDir, arg)
			} else {
				resolvedArgs[i] = arg
			}
		}
		cmd = cmd + " " + strings.Join(resolvedArgs, " ")
	}
	return ResolvedCommand{Command: cmd}
}

// ValidateZipForHook performs full validation of a zip archive for a hook asset.
// It checks that metadata.toml exists, parses it, validates the asset type,
// verifies the hook section, and checks that any referenced script file exists.
func ValidateZipForHook(zipData []byte) error {
	files, err := utils.ListZipFiles(zipData)
	if err != nil {
		return fmt.Errorf("failed to list zip files: %w", err)
	}

	if !ContainsFile(files, "metadata.toml") {
		return errors.New("metadata.toml not found in zip")
	}

	metadataBytes, err := utils.ReadZipFile(zipData, "metadata.toml")
	if err != nil {
		return fmt.Errorf("failed to read metadata.toml: %w", err)
	}

	meta, err := metadata.Parse(metadataBytes)
	if err != nil {
		return fmt.Errorf("failed to parse metadata: %w", err)
	}

	if err := meta.ValidateWithFiles(files); err != nil {
		return fmt.Errorf("metadata validation failed: %w", err)
	}

	if meta.Asset.Type != asset.TypeHook {
		return fmt.Errorf("asset type mismatch: expected hook, got %s", meta.Asset.Type)
	}

	if meta.Hook == nil {
		return errors.New("[hook] section missing in metadata")
	}

	if meta.Hook.ScriptFile != "" {
		if !ContainsFile(files, meta.Hook.ScriptFile) {
			return fmt.Errorf("script file not found in zip: %s", meta.Hook.ScriptFile)
		}
	}

	return nil
}
