package commands

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/clients"
	"github.com/sleuth-io/sx/internal/handlers/hook"
	"github.com/sleuth-io/sx/internal/ui"
)

// captureOutput renders processInstallationResults into a buffer so tests
// can assert on the user-visible install summary.
func captureOutput(t *testing.T, results map[string]clients.InstallResponse) string {
	t.Helper()
	var buf bytes.Buffer
	styledOut := ui.NewOutput(&buf, &bytes.Buffer{})
	processInstallationResults(results, styledOut)
	return buf.String()
}

func TestProcessInstallationResults_UnsupportedEventSummary(t *testing.T) {
	t.Run("aggregates multiple skips per client into one line", func(t *testing.T) {
		results := map[string]clients.InstallResponse{
			"gemini": {Results: []clients.AssetResult{
				skippedHook("log-pre-compact", "Gemini", "pre-compact"),
				skippedHook("log-subagent-start", "Gemini", "subagent-start"),
				skippedHook("log-subagent-stop", "Gemini", "subagent-stop"),
			}},
		}
		out := captureOutput(t, results)

		// One summary line per affected client.
		if !strings.Contains(out, "skipped 3 hooks") {
			t.Errorf("output did not aggregate three skips:\n%s", out)
		}
		if !strings.Contains(out, "pre-compact") || !strings.Contains(out, "subagent-start") || !strings.Contains(out, "subagent-stop") {
			t.Errorf("summary missing one of the events:\n%s", out)
		}
		// Per-asset ⊘ lines should be suppressed for unsupported-event skips.
		if strings.Contains(out, "log-pre-compact → Gemini") {
			t.Errorf("per-asset ⊘ line not suppressed:\n%s", out)
		}
	})

	t.Run("singular phrasing for one skip", func(t *testing.T) {
		results := map[string]clients.InstallResponse{
			"gemini": {Results: []clients.AssetResult{
				skippedHook("log-pre-compact", "Gemini", "pre-compact"),
			}},
		}
		out := captureOutput(t, results)
		if !strings.Contains(out, "skipped 1 hook (event not supported: pre-compact)") {
			t.Errorf("singular phrasing not used:\n%s", out)
		}
	})

	t.Run("non-unsupported skips keep per-asset ⊘ line", func(t *testing.T) {
		// "No compatible assets" / "Unsupported asset type" skips are NOT
		// rolled into the unsupported-event summary; they retain individual
		// rendering because the user may want to see them per-asset.
		results := map[string]clients.InstallResponse{
			"cursor": {Results: []clients.AssetResult{
				{
					AssetName: "some-skill",
					Status:    clients.StatusSkipped,
					Message:   "Unsupported asset type: foo",
					Error:     nil,
				},
			}},
		}
		out := captureOutput(t, results)
		if !strings.Contains(out, "some-skill → Cursor") {
			t.Errorf("non-unsupported skip should keep per-asset rendering:\n%s", out)
		}
	})

	t.Run("multiple clients each get their own summary", func(t *testing.T) {
		results := map[string]clients.InstallResponse{
			"gemini": {Results: []clients.AssetResult{
				skippedHook("log-pre-compact", "Gemini", "pre-compact"),
			}},
			"cline": {Results: []clients.AssetResult{
				skippedHook("log-stop", "Cline", "stop"),
			}},
		}
		out := captureOutput(t, results)
		if !strings.Contains(out, "Gemini") || !strings.Contains(out, "pre-compact") {
			t.Errorf("missing Gemini summary:\n%s", out)
		}
		if !strings.Contains(out, "Cline") || !strings.Contains(out, "stop") {
			t.Errorf("missing Cline summary:\n%s", out)
		}
	})

	t.Run("duplicate events deduplicated within a client", func(t *testing.T) {
		// Two assets fan out to the same unsupported event — the summary
		// should list the event once but count both skips.
		results := map[string]clients.InstallResponse{
			"gemini": {Results: []clients.AssetResult{
				skippedHook("log-pre-compact-a", "Gemini", "pre-compact"),
				skippedHook("log-pre-compact-b", "Gemini", "pre-compact"),
			}},
		}
		out := captureOutput(t, results)
		if !strings.Contains(out, "skipped 2 hooks") {
			t.Errorf("count should reflect 2 skips:\n%s", out)
		}
		// "pre-compact" should appear exactly once in events list.
		if strings.Count(out, "pre-compact") != 1 {
			t.Errorf("event list not deduplicated:\n%s", out)
		}
	})
}

func skippedHook(asset, client, event string) clients.AssetResult {
	err := hook.UnsupportedEventError(client, event)
	return clients.AssetResult{
		AssetName: asset,
		Status:    clients.StatusSkipped,
		Message:   err.Error(),
		Error:     err,
	}
}
