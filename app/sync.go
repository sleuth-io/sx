package main

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/sleuth-io/sx/internal/commands"
)

// syncMu serializes SyncAITools runs — a second click while one is in
// flight would race the same client directories.
var syncMu sync.Mutex

// SyncAITools runs the real `sx install` pipeline: every active profile's
// lock file, scope resolution for this machine and user, delivery to every
// detected AI tool, and stale-asset cleanup. It executes the CLI's install
// command in-process so the app and the terminal can never disagree about
// what "installed" means.
func (a *App) SyncAITools() (string, error) {
	syncMu.Lock()
	defer syncMu.Unlock()

	var out bytes.Buffer
	cmd := commands.NewInstallCommand()
	cmd.SetArgs([]string{})
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	if err := cmd.Execute(); err != nil {
		if summary := installSummary(out.String()); summary != "" {
			return "", fmt.Errorf("%s: %w", summary, err)
		}
		return "", err
	}
	if summary := installSummary(out.String()); summary != "" {
		return summary, nil
	}
	return "Your AI tools are up to date", nil
}

// installSummary pulls the human outcome line out of `sx install` output.
// Summary lines ("✓ Installed 3 assets") start at column 0; per-asset item
// lines are indented — prefer the last top-level summary so a multi-asset
// sync doesn't report the final asset's bare name.
func installSummary(output string) string {
	lines := strings.Split(output, "\n")
	for _, raw := range slices.Backward(lines) {
		if strings.HasPrefix(raw, "✓") || strings.HasPrefix(raw, "!") || strings.HasPrefix(raw, "✗") {
			return strings.TrimSpace(strings.TrimLeft(raw, "✓!✗ "))
		}
	}
	// No top-level summary — fall back to any marked line.
	for _, raw := range slices.Backward(lines) {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "✓") || strings.HasPrefix(line, "!") || strings.HasPrefix(line, "✗") {
			return strings.TrimSpace(strings.TrimLeft(line, "✓!✗ "))
		}
	}
	return ""
}
