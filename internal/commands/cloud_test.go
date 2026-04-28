package commands

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/cloud"
	"github.com/sleuth-io/sx/internal/cloud/cloudtest"
)

func TestParseAttachCommandLine(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantURL string
		wantTok string
		wantErr bool
	}{
		{
			name:    "canonical paste from success page",
			input:   "sx cloud attach --url=https://app.skills.new/relay/SRabc/ --token=tok-xyz",
			wantURL: "https://app.skills.new/relay/SRabc/",
			wantTok: "tok-xyz",
		},
		{
			name:    "space-separated flags",
			input:   "sx cloud attach --url https://app.skills.new/relay/SRabc/ --token tok-xyz",
			wantURL: "https://app.skills.new/relay/SRabc/",
			wantTok: "tok-xyz",
		},
		{
			name:    "bare flags without sx cloud attach prefix",
			input:   "--url=https://app.skills.new/relay/SRabc/ --token=tok-xyz",
			wantURL: "https://app.skills.new/relay/SRabc/",
			wantTok: "tok-xyz",
		},
		{
			name:    "swapped order",
			input:   "sx cloud attach --token=tok-xyz --url=https://app.skills.new/relay/SRabc/",
			wantURL: "https://app.skills.new/relay/SRabc/",
			wantTok: "tok-xyz",
		},
		{
			name:    "leading binary path",
			input:   "/usr/local/bin/sx cloud attach --url=https://app.skills.new/relay/SRabc/ --token=tok-xyz",
			wantURL: "https://app.skills.new/relay/SRabc/",
			wantTok: "tok-xyz",
		},
		{
			name:    "whitespace variations",
			input:   "  sx cloud attach --url=https://app.skills.new/relay/SRabc/   --token=tok-xyz  ",
			wantURL: "https://app.skills.new/relay/SRabc/",
			wantTok: "tok-xyz",
		},
		{
			name:    "missing token",
			input:   "sx cloud attach --url=https://app.skills.new/relay/SRabc/",
			wantErr: true,
		},
		{
			name:    "missing url",
			input:   "sx cloud attach --token=tok-xyz",
			wantErr: true,
		},
		{
			name:    "unrelated text",
			input:   "hello world",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotURL, gotTok, err := parseAttachCommandLine(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got URL=%q token=%q", gotURL, gotTok)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotURL != tc.wantURL {
				t.Errorf("URL: got %q want %q", gotURL, tc.wantURL)
			}
			if gotTok != tc.wantTok {
				t.Errorf("token: got %q want %q", gotTok, tc.wantTok)
			}
		})
	}
}

func TestReadAttachCommandSkipsBlankLines(t *testing.T) {
	input := "\n\n   \nsx cloud attach --url=https://x/relay/SR/ --token=t\n"
	got, err := readAttachCommand(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "sx cloud attach --url=https://x/relay/SR/ --token=t"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestReadAttachCommandEmptyInputErrors(t *testing.T) {
	if _, err := readAttachCommand(strings.NewReader("")); err == nil {
		t.Error("expected error for empty input")
	}
}

func TestMaskToken(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", "****"},
		{"ab", "****"},
		{"abcd", "****"},
		{"abcdefghij", "abcd...********"},
	}
	for _, tc := range cases {
		got := maskToken(tc.in)
		if got != tc.want {
			t.Errorf("maskToken(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestMCPEndpointURL(t *testing.T) {
	got := mcpEndpointURL("https://app.skills.new/relay/SRabc/")
	want := "https://app.skills.new/relay/SRabc/mcp/assets/"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// TestPersistCredential_FailedProbeKeepsOldCredentialIntact guards
// against a subtle order-of-operations regression: when a user runs
// “sx cloud attach --force“ for a different relay and the probe
// rejects the new token, the OLD credential must still be loadable.
// The fix is to defer the old-keyring eviction until after probe + save
// succeed.
func TestPersistCredential_FailedProbeKeepsOldCredentialIntact(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SX_CONFIG_DIR", tmp)
	fake := cloudtest.InstallKeyring(t)

	// Seed a working credential for the OLD relay.
	old := &cloud.Credential{
		RelayBaseURL: "https://app.skills.new/relay/SRold/",
		RelayGID:     "SRold",
		MachineToken: "old-token",
	}
	if err := cloud.Save(old); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	// Stand up a relay server that rejects the NEW token.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	newURL := strings.TrimSuffix(srv.URL, "/") + "/relay/SRnew/"

	// Fire persistCredential with --force + a bad token. Should fail.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := &cobra.Command{}
	cmd.SetContext(ctx)
	cmd.SetOut(os.Stderr)
	cmd.SetErr(os.Stderr)

	err := persistCredential(cmd, newURL, "bad-new-token", true)
	if err == nil {
		t.Fatal("expected persistCredential to fail when probe rejects the token")
	}

	// OLD credential must still be loadable — that's the core of this
	// test. If the eviction fired too early, Load() now trips the
	// "metadata present but token missing" branch in credential.go.
	got, loadErr := cloud.Load()
	if loadErr != nil {
		t.Fatalf("old credential should still load after failed swap: %v", loadErr)
	}
	if got.RelayGID != "SRold" || got.MachineToken != "old-token" {
		t.Errorf("old credential altered: got %+v", got)
	}
	// Keyring should still hold the old token, NOT the new one.
	if _, has := fake.Entries()["SRnew"]; has {
		t.Errorf("keyring should not contain the failed new token")
	}
	if v := fake.Entries()["SRold"]; v != "old-token" {
		t.Errorf("keyring lost old token: got %q", v)
	}
}
