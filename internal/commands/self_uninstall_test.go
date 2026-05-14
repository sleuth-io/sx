package commands

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// stubAssetCleanup swaps assetCleanupFn for the duration of a test so we can
// assert it was invoked and control whether it succeeds or fails.
func stubAssetCleanup(t *testing.T, returnErr error) *int {
	t.Helper()
	calls := 0
	prev := assetCleanupFn
	assetCleanupFn = func(_ *cobra.Command, _ []string, opts UninstallOptions) error {
		calls++
		if !opts.All || !opts.Yes {
			t.Errorf("expected asset cleanup to be invoked with All=true, Yes=true; got %+v", opts)
		}
		return returnErr
	}
	t.Cleanup(func() { assetCleanupFn = prev })
	return &calls
}

// newSelfUninstallTestCmd wires up a cobra.Command with buffered output and a
// stub executableFn pointing at a temp file so the test can observe binary
// removal without touching the actual test binary.
func newSelfUninstallTestCmd(t *testing.T, stubBinary string) (*cobra.Command, *bytes.Buffer) {
	t.Helper()
	cmd := &cobra.Command{}
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)

	prev := executableFn
	executableFn = func() (string, error) { return stubBinary, nil }
	t.Cleanup(func() { executableFn = prev })

	return cmd, out
}

func TestSelfUninstall_DryRunMakesNoChanges(t *testing.T) {
	configDir := t.TempDir()
	cacheDir := t.TempDir()
	t.Setenv("SX_CONFIG_DIR", configDir)
	t.Setenv("SX_CACHE_DIR", cacheDir)

	// Drop a marker file into each dir so we can prove dry-run preserved them.
	configMarker := filepath.Join(configDir, "config.json")
	cacheMarker := filepath.Join(cacheDir, "marker")
	if err := os.WriteFile(configMarker, []byte("{}"), 0600); err != nil {
		t.Fatalf("setup config marker: %v", err)
	}
	if err := os.WriteFile(cacheMarker, []byte("x"), 0600); err != nil {
		t.Fatalf("setup cache marker: %v", err)
	}

	stubBinary := filepath.Join(t.TempDir(), "sx")
	if err := os.WriteFile(stubBinary, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("setup stub binary: %v", err)
	}

	cmd, out := newSelfUninstallTestCmd(t, stubBinary)
	if err := runSelfUninstall(cmd, SelfUninstallOptions{DryRun: true, KeepAssets: true}); err != nil {
		t.Fatalf("runSelfUninstall returned error: %v", err)
	}

	output := out.String()
	for _, want := range []string{configDir, cacheDir, stubBinary, "dry run"} {
		if !strings.Contains(output, want) {
			t.Errorf("expected output to contain %q\noutput:\n%s", want, output)
		}
	}

	// Files must still exist.
	for _, p := range []string{configMarker, cacheMarker, stubBinary} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("dry-run deleted %s: %v", p, err)
		}
	}
}

func TestSelfUninstall_KeepAssetsRemovesConfigCacheAndBinary(t *testing.T) {
	configDir := t.TempDir()
	cacheDir := t.TempDir()
	t.Setenv("SX_CONFIG_DIR", configDir)
	t.Setenv("SX_CACHE_DIR", cacheDir)

	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte("{}"), 0600); err != nil {
		t.Fatalf("setup config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "marker"), []byte("x"), 0600); err != nil {
		t.Fatalf("setup cache: %v", err)
	}

	stubBinary := filepath.Join(t.TempDir(), "sx")
	if err := os.WriteFile(stubBinary, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("setup stub binary: %v", err)
	}

	cmd, out := newSelfUninstallTestCmd(t, stubBinary)
	opts := SelfUninstallOptions{
		Yes:         true,
		KeepAssets:  true,
		skipConfirm: true,
	}
	if err := runSelfUninstall(cmd, opts); err != nil {
		t.Fatalf("runSelfUninstall returned error: %v", err)
	}

	if _, err := os.Stat(configDir); !os.IsNotExist(err) {
		t.Errorf("config dir not removed: stat err = %v", err)
	}
	if _, err := os.Stat(cacheDir); !os.IsNotExist(err) {
		t.Errorf("cache dir not removed: stat err = %v", err)
	}
	if _, err := os.Stat(stubBinary); !os.IsNotExist(err) {
		t.Errorf("binary not removed: stat err = %v", err)
	}

	if !strings.Contains(out.String(), "sx has been removed") {
		t.Errorf("expected success message in output:\n%s", out.String())
	}
}

func TestSelfUninstall_KeepBinaryPreservesIt(t *testing.T) {
	t.Setenv("SX_CONFIG_DIR", t.TempDir())
	t.Setenv("SX_CACHE_DIR", t.TempDir())

	stubBinary := filepath.Join(t.TempDir(), "sx")
	if err := os.WriteFile(stubBinary, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("setup stub binary: %v", err)
	}

	cmd, _ := newSelfUninstallTestCmd(t, stubBinary)
	opts := SelfUninstallOptions{
		Yes:         true,
		KeepAssets:  true,
		keepBinary:  true,
		skipConfirm: true,
	}
	if err := runSelfUninstall(cmd, opts); err != nil {
		t.Fatalf("runSelfUninstall returned error: %v", err)
	}

	if _, err := os.Stat(stubBinary); err != nil {
		t.Errorf("binary removed even though keepBinary=true: %v", err)
	}
}

func TestSelfUninstall_HandlesMissingDirsGracefully(t *testing.T) {
	// Point to dirs that have never existed.
	tmp := t.TempDir()
	missingConfig := filepath.Join(tmp, "never-created-config")
	missingCache := filepath.Join(tmp, "never-created-cache")
	t.Setenv("SX_CONFIG_DIR", missingConfig)
	t.Setenv("SX_CACHE_DIR", missingCache)

	stubBinary := filepath.Join(t.TempDir(), "sx")
	if err := os.WriteFile(stubBinary, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("setup stub binary: %v", err)
	}

	cmd, out := newSelfUninstallTestCmd(t, stubBinary)
	opts := SelfUninstallOptions{
		Yes:         true,
		KeepAssets:  true,
		keepBinary:  true,
		skipConfirm: true,
	}
	if err := runSelfUninstall(cmd, opts); err != nil {
		t.Fatalf("runSelfUninstall returned error on missing dirs: %v", err)
	}

	output := out.String()
	if strings.Contains(strings.ToLower(output), "failed to remove") {
		t.Errorf("missing dirs should not produce failure warnings:\n%s", output)
	}
}

func TestSelfUninstall_AssetCleanupSuccessProceeds(t *testing.T) {
	configDir := t.TempDir()
	cacheDir := t.TempDir()
	t.Setenv("SX_CONFIG_DIR", configDir)
	t.Setenv("SX_CACHE_DIR", cacheDir)

	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte("{}"), 0600); err != nil {
		t.Fatalf("setup config: %v", err)
	}

	stubBinary := filepath.Join(t.TempDir(), "sx")
	if err := os.WriteFile(stubBinary, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("setup stub binary: %v", err)
	}

	calls := stubAssetCleanup(t, nil)

	cmd, _ := newSelfUninstallTestCmd(t, stubBinary)
	opts := SelfUninstallOptions{
		Yes:         true,
		skipConfirm: true,
	}
	if err := runSelfUninstall(cmd, opts); err != nil {
		t.Fatalf("runSelfUninstall returned error: %v", err)
	}

	if *calls != 1 {
		t.Errorf("expected asset cleanup to be called exactly once, got %d", *calls)
	}
	if _, err := os.Stat(configDir); !os.IsNotExist(err) {
		t.Errorf("config dir not removed after successful asset cleanup: %v", err)
	}
	if _, err := os.Stat(stubBinary); !os.IsNotExist(err) {
		t.Errorf("binary not removed after successful asset cleanup: %v", err)
	}
}

func TestSelfUninstall_AssetCleanupFailureAbortsWithoutForce(t *testing.T) {
	configDir := t.TempDir()
	cacheDir := t.TempDir()
	t.Setenv("SX_CONFIG_DIR", configDir)
	t.Setenv("SX_CACHE_DIR", cacheDir)

	configFile := filepath.Join(configDir, "config.json")
	if err := os.WriteFile(configFile, []byte("{}"), 0600); err != nil {
		t.Fatalf("setup config: %v", err)
	}

	stubBinary := filepath.Join(t.TempDir(), "sx")
	if err := os.WriteFile(stubBinary, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("setup stub binary: %v", err)
	}

	stubAssetCleanup(t, errors.New("simulated client failure"))

	cmd, out := newSelfUninstallTestCmd(t, stubBinary)
	opts := SelfUninstallOptions{
		Yes:         true,
		skipConfirm: true,
	}
	err := runSelfUninstall(cmd, opts)
	if err == nil {
		t.Fatal("expected runSelfUninstall to return an error when asset cleanup fails without --force")
	}
	if !strings.Contains(err.Error(), "asset cleanup failed") {
		t.Errorf("expected error to mention asset cleanup, got %v", err)
	}

	// Nothing else should have been touched.
	if _, err := os.Stat(configFile); err != nil {
		t.Errorf("config file deleted despite asset cleanup failure: %v", err)
	}
	if _, err := os.Stat(stubBinary); err != nil {
		t.Errorf("binary deleted despite asset cleanup failure: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "Asset cleanup failed") {
		t.Errorf("expected user-facing error message; got:\n%s", output)
	}
	if !strings.Contains(output, "--force") {
		t.Errorf("expected output to mention --force escape hatch; got:\n%s", output)
	}
}

func TestSelfUninstall_AssetCleanupFailureWithForceProceeds(t *testing.T) {
	configDir := t.TempDir()
	cacheDir := t.TempDir()
	t.Setenv("SX_CONFIG_DIR", configDir)
	t.Setenv("SX_CACHE_DIR", cacheDir)

	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte("{}"), 0600); err != nil {
		t.Fatalf("setup config: %v", err)
	}

	stubBinary := filepath.Join(t.TempDir(), "sx")
	if err := os.WriteFile(stubBinary, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("setup stub binary: %v", err)
	}

	stubAssetCleanup(t, errors.New("simulated client failure"))

	cmd, _ := newSelfUninstallTestCmd(t, stubBinary)
	opts := SelfUninstallOptions{
		Yes:         true,
		Force:       true,
		skipConfirm: true,
	}
	if err := runSelfUninstall(cmd, opts); err != nil {
		t.Fatalf("expected --force to override failure, got error: %v", err)
	}

	if _, err := os.Stat(configDir); !os.IsNotExist(err) {
		t.Errorf("config dir not removed with --force: %v", err)
	}
	if _, err := os.Stat(stubBinary); !os.IsNotExist(err) {
		t.Errorf("binary not removed with --force: %v", err)
	}
}

func TestRemoveDirIfExists(t *testing.T) {
	t.Run("ignores missing", func(t *testing.T) {
		if err := removeDirIfExists(filepath.Join(t.TempDir(), "nope")); err != nil {
			t.Errorf("expected nil for missing path, got %v", err)
		}
	})
	t.Run("removes existing", func(t *testing.T) {
		dir := t.TempDir()
		nested := filepath.Join(dir, "child")
		if err := os.MkdirAll(nested, 0755); err != nil {
			t.Fatalf("setup: %v", err)
		}
		if err := os.WriteFile(filepath.Join(nested, "f"), []byte("x"), 0600); err != nil {
			t.Fatalf("setup: %v", err)
		}
		if err := removeDirIfExists(dir); err != nil {
			t.Errorf("remove failed: %v", err)
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("dir not removed: %v", err)
		}
	})
	t.Run("empty path is a no-op", func(t *testing.T) {
		if err := removeDirIfExists(""); err != nil {
			t.Errorf("expected nil for empty path, got %v", err)
		}
	})
}
