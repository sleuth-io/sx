package commands

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/clients"
	"github.com/sleuth-io/sx/internal/handlers/hook"
	"github.com/sleuth-io/sx/internal/ui"
)

// processInstallationResults is unexported; test it through the package.
//
// Each subtest constructs an allResults map shaped like what the orchestrator
// returns, runs processInstallationResults with strict={false,true}, and
// asserts on whether the result counts as a failure.

func TestProcessInstallationResults_StrictMode(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	unsupported := hook.UnsupportedEventError("Gemini", "pre-compact")

	t.Run("non-strict: unsupported-event skip is informational", func(t *testing.T) {
		styledOut := ui.NewOutput(&bytes.Buffer{}, &bytes.Buffer{})
		results := map[string]clients.InstallResponse{
			"gemini": {Results: []clients.AssetResult{
				{
					AssetName: "log-pre-compact",
					Status:    clients.StatusSkipped,
					Message:   unsupported.Error(),
					Error:     unsupported,
				},
			}},
		}
		out := processInstallationResults(results, styledOut, false)
		if len(out.Failed) != 0 {
			t.Errorf("non-strict mode counted %d failures, want 0: %v", len(out.Failed), out.Failed)
		}
		if len(out.Errors) != 0 {
			t.Errorf("non-strict mode reported errors: %v", out.Errors)
		}
	})

	t.Run("strict: unsupported-event skip becomes failure", func(t *testing.T) {
		styledOut := ui.NewOutput(&bytes.Buffer{}, &bytes.Buffer{})
		results := map[string]clients.InstallResponse{
			"gemini": {Results: []clients.AssetResult{
				{
					AssetName: "log-pre-compact",
					Status:    clients.StatusSkipped,
					Message:   unsupported.Error(),
					Error:     unsupported,
				},
			}},
		}
		out := processInstallationResults(results, styledOut, true)
		if len(out.Failed) != 1 {
			t.Errorf("strict mode counted %d failures, want 1", len(out.Failed))
		}
		if !strings.Contains(out.Failed[0], "log-pre-compact") {
			t.Errorf("Failed[0] = %q, want it to mention the asset", out.Failed[0])
		}
		if len(out.Errors) == 0 {
			t.Errorf("strict mode produced no errors")
		}
	})

	t.Run("strict: unrelated skip stays informational", func(t *testing.T) {
		// Skips that aren't from unsupported events (e.g. "no compatible
		// assets", "unsupported asset type") should NOT be escalated by --strict.
		styledOut := ui.NewOutput(&bytes.Buffer{}, &bytes.Buffer{})
		results := map[string]clients.InstallResponse{
			"gemini": {Results: []clients.AssetResult{
				{
					Status:  clients.StatusSkipped,
					Message: "No compatible assets",
				},
			}},
		}
		out := processInstallationResults(results, styledOut, true)
		if len(out.Failed) != 0 {
			t.Errorf("strict mode escalated unrelated skip: %v", out.Failed)
		}
	})

	t.Run("strict: real failure stays a failure", func(t *testing.T) {
		styledOut := ui.NewOutput(&bytes.Buffer{}, &bytes.Buffer{})
		realErr := errors.New("disk full")
		results := map[string]clients.InstallResponse{
			"claude-code": {Results: []clients.AssetResult{
				{
					AssetName: "some-asset",
					Status:    clients.StatusFailed,
					Message:   realErr.Error(),
					Error:     realErr,
				},
			}},
		}
		out := processInstallationResults(results, styledOut, true)
		if len(out.Failed) != 1 {
			t.Errorf("strict mode lost a real failure: %v", out.Failed)
		}
	})

	t.Run("non-strict: real failure stays a failure", func(t *testing.T) {
		styledOut := ui.NewOutput(&bytes.Buffer{}, &bytes.Buffer{})
		realErr := errors.New("disk full")
		results := map[string]clients.InstallResponse{
			"claude-code": {Results: []clients.AssetResult{
				{
					AssetName: "some-asset",
					Status:    clients.StatusFailed,
					Message:   realErr.Error(),
					Error:     realErr,
				},
			}},
		}
		out := processInstallationResults(results, styledOut, false)
		if len(out.Failed) != 1 {
			t.Errorf("non-strict mode lost a real failure: %v", out.Failed)
		}
	})
}
