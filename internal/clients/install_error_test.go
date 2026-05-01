package clients

import (
	"errors"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/handlers/hook"
)

func TestTranslateInstallError(t *testing.T) {
	t.Run("nil error becomes success", func(t *testing.T) {
		status, msg, err := TranslateInstallError(nil, "/path/to/install")
		if status != StatusSuccess {
			t.Errorf("status = %q, want %q", status, StatusSuccess)
		}
		if msg != "/path/to/install" {
			t.Errorf("msg = %q, want %q", msg, "/path/to/install")
		}
		if err != nil {
			t.Errorf("err = %v, want nil", err)
		}
	})

	t.Run("unsupported event becomes skipped", func(t *testing.T) {
		input := hook.UnsupportedEventError("Gemini", "pre-compact")
		status, msg, err := TranslateInstallError(input, "ignored")
		if status != StatusSkipped {
			t.Errorf("status = %q, want %q", status, StatusSkipped)
		}
		if !strings.Contains(msg, "pre-compact") {
			t.Errorf("msg = %q, want it to mention the event", msg)
		}
		if err != nil {
			t.Errorf("err = %v, want nil (soft skip should not propagate)", err)
		}
	})

	t.Run("wrapped unsupported event becomes skipped", func(t *testing.T) {
		// Simulate a handler that wraps the sentinel with additional context.
		wrapped := errors.Join(hook.UnsupportedEventError("Gemini", "pre-compact"),
			errors.New("during settings.json update"))
		status, _, err := TranslateInstallError(wrapped, "ignored")
		if status != StatusSkipped {
			t.Errorf("status = %q, want %q", status, StatusSkipped)
		}
		if err != nil {
			t.Errorf("err = %v, want nil", err)
		}
	})

	t.Run("other errors become failure", func(t *testing.T) {
		input := errors.New("disk full")
		status, msg, err := TranslateInstallError(input, "ignored")
		if status != StatusFailed {
			t.Errorf("status = %q, want %q", status, StatusFailed)
		}
		if !strings.Contains(msg, "disk full") {
			t.Errorf("msg = %q, want it to include the error detail", msg)
		}
		if err == nil {
			t.Errorf("err = nil, want the original error preserved")
		}
	})
}
