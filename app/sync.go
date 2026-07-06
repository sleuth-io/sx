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

// installSummary pulls the human outcome line out of `sx install` output —
// the last ✓/! line, stripped of its marker.
func installSummary(output string) string {
	for _, raw := range slices.Backward(strings.Split(output, "\n")) {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "✓") || strings.HasPrefix(line, "!") || strings.HasPrefix(line, "✗") {
			return strings.TrimSpace(strings.TrimLeft(line, "✓!✗ "))
		}
	}
	return ""
}
