package hook

import (
	"archive/zip"
	"bytes"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/metadata"
)

func createTestZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatalf("Failed to create zip entry: %v", err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatalf("Failed to write zip content: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Failed to close zip: %v", err)
	}
	return buf.Bytes()
}

func TestContainsFile(t *testing.T) {
	files := []string{"metadata.toml", "scripts/hook.sh", "README.md"}

	tests := []struct {
		name     string
		filename string
		want     bool
	}{
		{"exact match", "metadata.toml", true},
		{"exact path match", "scripts/hook.sh", true},
		{"base name fallback", "hook.sh", true},
		{"not found", "missing.txt", false},
		{"empty name", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ContainsFile(files, tt.filename); got != tt.want {
				t.Errorf("ContainsFile(%v, %q) = %v, want %v", files, tt.filename, got, tt.want)
			}
		})
	}
}

func TestContainsFile_EmptyList(t *testing.T) {
	if ContainsFile(nil, "anything") {
		t.Error("ContainsFile(nil, ...) should return false")
	}
}

func TestIsZipFile(t *testing.T) {
	zipFiles := []string{"metadata.toml", "scripts/run.py", "config.json"}

	if !IsZipFile(zipFiles, "scripts/run.py") {
		t.Error("should find exact match")
	}
	if IsZipFile(zipFiles, "run.py") {
		t.Error("should not match base name (exact only)")
	}
	if IsZipFile(zipFiles, "missing.txt") {
		t.Error("should not find missing file")
	}
	if IsZipFile(nil, "anything") {
		t.Error("should return false for nil zipFiles")
	}
}

func TestHasExtractableFiles(t *testing.T) {
	metaOnly := createTestZip(t, map[string]string{
		"metadata.toml": "content",
	})
	if HasExtractableFiles(metaOnly) {
		t.Error("zip with only metadata.toml should not have extractable files")
	}

	withScript := createTestZip(t, map[string]string{
		"metadata.toml": "content",
		"hook.sh":       "#!/bin/bash",
	})
	if !HasExtractableFiles(withScript) {
		t.Error("zip with hook.sh should have extractable files")
	}

	if HasExtractableFiles([]byte("not a zip")) {
		t.Error("invalid zip should return false")
	}
}

func TestCacheZipFiles(t *testing.T) {
	zipData := createTestZip(t, map[string]string{
		"metadata.toml": "content",
		"hook.sh":       "script",
	})

	files := CacheZipFiles(zipData)
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}

	if CacheZipFiles([]byte("bad zip")) != nil {
		t.Error("invalid zip should return nil")
	}
}

func TestMapEvent(t *testing.T) {
	eventMap := map[string]string{
		"pre-tool-use":  "PreToolUse",
		"post-tool-use": "PostToolUse",
		"session-start": "SessionStart",
	}

	t.Run("standard mapping", func(t *testing.T) {
		native, ok := MapEvent("pre-tool-use", eventMap, nil)
		if !ok || native != "PreToolUse" {
			t.Errorf("got (%q, %v), want (PreToolUse, true)", native, ok)
		}
	})

	t.Run("unsupported event", func(t *testing.T) {
		_, ok := MapEvent("unknown-event", eventMap, nil)
		if ok {
			t.Error("unknown event should not be supported")
		}
	})

	t.Run("client override", func(t *testing.T) {
		overrides := map[string]any{"event": "CustomEvent"}
		native, ok := MapEvent("pre-tool-use", eventMap, overrides)
		if !ok || native != "CustomEvent" {
			t.Errorf("got (%q, %v), want (CustomEvent, true)", native, ok)
		}
	})

	t.Run("empty override falls through", func(t *testing.T) {
		overrides := map[string]any{"event": ""}
		native, ok := MapEvent("session-start", eventMap, overrides)
		if !ok || native != "SessionStart" {
			t.Errorf("got (%q, %v), want (SessionStart, true)", native, ok)
		}
	})

	t.Run("non-event override ignored", func(t *testing.T) {
		overrides := map[string]any{"timeout": 30}
		native, ok := MapEvent("post-tool-use", eventMap, overrides)
		if !ok || native != "PostToolUse" {
			t.Errorf("got (%q, %v), want (PostToolUse, true)", native, ok)
		}
	})
}

func TestResolveCommand_ScriptFile(t *testing.T) {
	cfg := &metadata.HookConfig{
		ScriptFile: "hook.sh",
	}
	result := ResolveCommand(cfg, "/install/dir", nil)
	if result.Command != "/install/dir/hook.sh" {
		t.Errorf("got %q, want /install/dir/hook.sh", result.Command)
	}
}

func TestResolveCommand_CommandOnly(t *testing.T) {
	cfg := &metadata.HookConfig{
		Command: "npx",
		Args:    []string{"lint-check", "--fix"},
	}
	result := ResolveCommand(cfg, "/install/dir", nil)
	if result.Command != "npx lint-check --fix" {
		t.Errorf("got %q, want \"npx lint-check --fix\"", result.Command)
	}
}

func TestResolveCommand_CommandWithBundledScript(t *testing.T) {
	cfg := &metadata.HookConfig{
		Command: "python",
		Args:    []string{"scripts/run.py", "--verbose"},
	}
	zipFiles := []string{"metadata.toml", "scripts/run.py"}
	result := ResolveCommand(cfg, "/install/dir", zipFiles)
	expected := "python /install/dir/scripts/run.py --verbose"
	if result.Command != expected {
		t.Errorf("got %q, want %q", result.Command, expected)
	}
}

func TestResolveCommand_CommandNoArgs(t *testing.T) {
	cfg := &metadata.HookConfig{
		Command: "echo",
	}
	result := ResolveCommand(cfg, "/install/dir", nil)
	if result.Command != "echo" {
		t.Errorf("got %q, want \"echo\"", result.Command)
	}
}

func TestValidateZipForHook(t *testing.T) {
	t.Run("valid script-file hook", func(t *testing.T) {
		zipData := createTestZip(t, map[string]string{
			"metadata.toml": `[asset]
name = "test-hook"
version = "1.0.0"
type = "hook"
description = "Test"

[hook]
event = "pre-tool-use"
script-file = "hook.sh"
`,
			"hook.sh": "#!/bin/bash",
		})
		if err := ValidateZipForHook(zipData); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("valid command-only hook", func(t *testing.T) {
		zipData := createTestZip(t, map[string]string{
			"metadata.toml": `[asset]
name = "test-hook"
version = "1.0.0"
type = "hook"
description = "Test"

[hook]
event = "post-tool-use"
command = "echo"
`,
		})
		if err := ValidateZipForHook(zipData); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("missing metadata.toml", func(t *testing.T) {
		zipData := createTestZip(t, map[string]string{
			"hook.sh": "#!/bin/bash",
		})
		if err := ValidateZipForHook(zipData); err == nil {
			t.Error("expected error for missing metadata.toml")
		}
	})

	t.Run("wrong asset type", func(t *testing.T) {
		zipData := createTestZip(t, map[string]string{
			"metadata.toml": `[asset]
name = "test"
version = "1.0.0"
type = "skill"
description = "Test"

[skill]
prompt-file = "SKILL.md"
`,
			"SKILL.md": "content",
		})
		err := ValidateZipForHook(zipData)
		if err == nil {
			t.Error("expected error for wrong asset type")
		}
	})

	t.Run("missing script file", func(t *testing.T) {
		zipData := createTestZip(t, map[string]string{
			"metadata.toml": `[asset]
name = "test-hook"
version = "1.0.0"
type = "hook"
description = "Test"

[hook]
event = "pre-tool-use"
script-file = "hook.sh"
`,
		})
		err := ValidateZipForHook(zipData)
		if err == nil {
			t.Error("expected error for missing script file")
		}
	})

	t.Run("invalid zip", func(t *testing.T) {
		if err := ValidateZipForHook([]byte("not a zip")); err == nil {
			t.Error("expected error for invalid zip")
		}
	})
}

func TestValidateZipForHook_MissingHookSection(t *testing.T) {
	zipData := createTestZip(t, map[string]string{
		"metadata.toml": `[asset]
name = "test"
version = "1.0.0"
type = "` + asset.TypeHook.Key + `"
description = "Test"
`,
	})
	err := ValidateZipForHook(zipData)
	if err == nil {
		t.Error("expected error for missing [hook] section")
	}
}
