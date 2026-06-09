package commands

import (
	"bufio"
	"io"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/ui/components"
)

// TestPromptForRepoURL_Normalization pins SD-10170: the server matches repos by
// exact URL string and stores them with a scheme (https://github.com/...), so
// any scheme-less input must be normalized before being sent. The regression
// was that "github.com/owner/repo" (host-prefixed but scheme-less — exactly the
// form the prompt hint invites) fell through untouched and was rejected by the
// server as "Repository '...' not found".
func TestPromptForRepoURL_Normalization(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"bare slug", "sleuth-io/pulse", "https://github.com/sleuth-io/pulse"},
		{"host-prefixed scheme-less", "github.com/sleuth-io/pulse", "https://github.com/sleuth-io/pulse"},
		{"full https url unchanged", "https://github.com/sleuth-io/pulse", "https://github.com/sleuth-io/pulse"},
		{"git ssh url unchanged", "git@github.com:sleuth-io/pulse", "git@github.com:sleuth-io/pulse"},
		{"non-github host scheme-less", "gitlab.com/acme/widgets", "https://gitlab.com/acme/widgets"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := bufio.NewReader(strings.NewReader(tc.input + "\n"))
			ioc := components.NewIOContext(in, io.Discard)

			got, err := promptForRepoURL(ioc)
			if err != nil {
				t.Fatalf("promptForRepoURL(%q): %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("promptForRepoURL(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
