package commands

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/pkg/browser"
	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/cloud"
	"github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/logger"
	"github.com/sleuth-io/sx/internal/ui"
)

// DefaultCloudSignupURL is the skills.new page that walks a user
// through “sx cloud connect“ in a browser. Environment override:
// “SX_CLOUD_URL“.
const DefaultCloudSignupURL = "https://app.skills.new/relay/connect"

// NewCloudCommand creates the “sx cloud“ parent command. Subcommands
// are “connect“, “attach“, “serve“, “status“, and “revoke“.
func NewCloudCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cloud",
		Short: "Manage the skills.new relay that exposes this vault to chat clients",
		Long: `Manage the skills.new cloud relay.

A "relay" is a public MCP endpoint hosted on skills.new that forwards
requests from claude.ai or chatgpt.com to the sx process running on
your machine. The chat client reaches sx over a persistent WebSocket
anchored to skills.new; the vault content itself never leaves this
machine.

Typical flow:

  sx cloud connect       # open browser, confirm email, paste back the attach line
  sx cloud attach ...    # persist the machine token on disk
  sx cloud serve         # run the dispatcher loop (keep this running)
  sx cloud status        # inspect the currently-attached relay
  sx cloud revoke        # remove the local credential`,
	}
	cmd.AddCommand(newCloudConnectCommand())
	cmd.AddCommand(newCloudAttachCommand())
	cmd.AddCommand(newCloudServeCommand())
	cmd.AddCommand(newCloudStatusCommand())
	cmd.AddCommand(newCloudRevokeCommand())
	return cmd
}

// ---------------------------------------------------------------------------
// connect
// ---------------------------------------------------------------------------

func newCloudConnectCommand() *cobra.Command {
	var noBrowser bool
	var force bool
	cmd := &cobra.Command{
		Use:   "connect",
		Short: "Open the skills.new signup page and attach the returned credential",
		Long: "Open the skills.new magic-link signup page in your default browser,\n" +
			"wait for you to complete the flow, then paste the `sx cloud attach`\n" +
			"command shown on the success page back into this terminal.\n\n" +
			"The signup page mints a single machine token. Running `connect` again\n" +
			"(or clicking a new magic link) from a different machine rotates that\n" +
			"token — the previous sx instance stops serving on its next frame.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCloudConnect(cmd, noBrowser, force)
		},
	}
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false,
		"Print the signup URL instead of launching a browser")
	cmd.Flags().BoolVar(&force, "force", false,
		"Replace an existing credential even if it points at a different relay")
	return cmd
}

func runCloudConnect(cmd *cobra.Command, noBrowser, force bool) error {
	out := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())

	signupURL := DefaultCloudSignupURL
	if env := os.Getenv("SX_CLOUD_URL"); env != "" {
		signupURL = env
	}

	out.Info("Opening skills.new signup page...")
	out.Printf("%s", signupURL+"\n\n")
	if noBrowser {
		out.Printf("%s", "Open the link above in your browser, then paste the `sx cloud attach` command shown there:\n")
	} else {
		// Best effort — headless environments can use --no-browser.
		if err := browser.OpenURL(signupURL); err != nil {
			out.Warning(fmt.Sprintf("Failed to open browser automatically: %v", err))
			out.Printf("%s", "Open the link above in your browser, then paste the `sx cloud attach` command shown there:\n")
		} else {
			out.Printf("%s", "After confirming your email in the browser, paste the `sx cloud attach` command here:\n")
		}
	}

	line, err := readAttachCommand(cmd.InOrStdin())
	if err != nil {
		return err
	}
	relayURL, token, err := parseAttachCommandLine(line)
	if err != nil {
		return fmt.Errorf("could not parse pasted command: %w", err)
	}
	return persistCredential(cmd, relayURL, token, force)
}

// readAttachCommand pulls a single line from stdin, skipping blank
// lines so the user can paste at will.
func readAttachCommand(r interface{ Read(p []byte) (int, error) }) (string, error) {
	scanner := bufio.NewScanner(r)
	// sx cloud attach lines are short; give ourselves headroom anyway
	// so an unusually long URL doesn't silently truncate.
	scanner.Buffer(make([]byte, 0, 4096), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		return line, nil
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("reading input: %w", err)
	}
	return "", errors.New("no input received")
}

// parseAttachCommandLine accepts either
//   - a full "sx cloud attach --url=... --token=..." line (as printed on
//     the success page), OR
//   - a bare "--url=... --token=..." string.
//
// We deliberately don't re-execute the cobra command; we just extract
// the two values.
func parseAttachCommandLine(line string) (relayURL, token string, err error) {
	line = strings.TrimSpace(line)
	// Trim a leading ``sx cloud attach`` / ``./sx cloud attach`` /
	// ``bin/sx cloud attach`` etc. — anything before the first ``--``.
	if idx := strings.Index(line, "--"); idx > 0 {
		line = line[idx:]
	}
	// Tokenize on whitespace; each token is either ``--url=...``,
	// ``--token=...``, or (with a space separator) ``--url`` followed
	// by the value as the next token. Handle both shapes.
	fields := strings.Fields(line)
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		switch {
		case strings.HasPrefix(f, "--url="):
			relayURL = strings.TrimPrefix(f, "--url=")
		case f == "--url" && i+1 < len(fields):
			relayURL = fields[i+1]
			i++
		case strings.HasPrefix(f, "--token="):
			token = strings.TrimPrefix(f, "--token=")
		case f == "--token" && i+1 < len(fields):
			token = fields[i+1]
			i++
		}
	}
	if relayURL == "" || token == "" {
		return "", "", errors.New("missing --url or --token")
	}
	return relayURL, token, nil
}

// ---------------------------------------------------------------------------
// attach
// ---------------------------------------------------------------------------

func newCloudAttachCommand() *cobra.Command {
	var relayURL, token string
	var force bool
	cmd := &cobra.Command{
		Use:   "attach",
		Short: "Persist a machine token for a skills.new relay",
		Long: "Persist the machine token + relay URL produced by `sx cloud connect`.\n\n" +
			"The machine token is written to the OS keyring (macOS Keychain,\n" +
			"Windows Credential Manager, freedesktop Secret Service on Linux).\n" +
			"If the keyring is unavailable, sx falls back to an on-disk TOML\n" +
			"file with 0600 permissions and prints a warning.\n\n" +
			"Attaching when a credential already exists for a different relay\n" +
			"fails unless --force is set. Re-attaching the same relay (to rotate\n" +
			"its token) is always allowed.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if relayURL == "" || token == "" {
				return errors.New("--url and --token are required")
			}
			return persistCredential(cmd, relayURL, token, force)
		},
	}
	cmd.Flags().StringVar(&relayURL, "url", "",
		"Relay base URL, e.g. https://app.skills.new/relay/SR.../")
	cmd.Flags().StringVar(&token, "token", "",
		"Machine token shown once on the skills.new success page")
	cmd.Flags().BoolVar(&force, "force", false,
		"Replace an existing credential even if it points at a different relay")
	return cmd
}

func persistCredential(cmd *cobra.Command, relayURL, token string, force bool) error {
	out := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())

	cleanURL, relayGID, err := cloud.ParseRelayURL(relayURL)
	if err != nil {
		return fmt.Errorf("invalid --url: %w", err)
	}
	cred := &cloud.Credential{
		RelayBaseURL: cleanURL,
		RelayGID:     relayGID,
		MachineToken: token,
	}

	// Guard against a user pointing sx at a different relay than the
	// one currently attached. Same-GID re-attach (token rotation) is
	// common and proceeds; cross-relay swap requires --force.
	var supersededGID string
	if existing, loadErr := cloud.Load(); loadErr == nil && existing != nil && existing.RelayGID != relayGID {
		if !force {
			return fmt.Errorf(
				"refusing to overwrite existing credential for relay %s with a credential for relay %s; "+
					"re-run with --force to proceed",
				existing.RelayGID, relayGID,
			)
		}
		// Defer the actual keyring eviction until AFTER Probe + Save
		// succeed — if the probe rejects the new token or Save fails,
		// we want the old credential intact and usable.
		supersededGID = existing.RelayGID
	}

	// Verify the token actually authenticates before we write it to
	// disk / the keyring. A typo or wrong-relay paste otherwise
	// surfaces only when ``sx cloud serve`` fails to connect.
	probeCtx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
	defer cancel()
	if probeErr := cloud.Probe(probeCtx, cred, nil); probeErr != nil {
		if errors.Is(probeErr, cloud.ErrProbeUnauthorized) {
			return errors.New("relay rejected the machine token; re-run `sx cloud connect` to mint a fresh one")
		}
		return fmt.Errorf("could not reach relay to verify credential: %w", probeErr)
	}

	if err := cloud.Save(cred); err != nil {
		return err
	}

	// Save succeeded — now it's safe to evict the old keyring entry.
	// A failure here is non-fatal: the new credential is already
	// authoritative (TOML + keyring both reference the new GID);
	// a leaked old keyring entry is a secret-hygiene regression,
	// not a correctness one, and we surface it to the operator so
	// they can remove it manually if they care.
	if supersededGID != "" {
		out.Warning("Overwriting existing credential for relay " + supersededGID)
		if revErr := cloud.RevokeKeyringEntry(supersededGID); revErr != nil {
			out.Warning(fmt.Sprintf(
				"failed to remove keyring entry for old relay %s: %v (proceeding)",
				supersededGID, revErr,
			))
		}
	}
	path, _ := cloud.Path()
	out.Success("Saved machine token for relay " + relayGID)
	out.Printf("%s", fmt.Sprintf("  credential file: %s\n", path))
	out.Printf("%s", "  run `sx cloud serve` to start forwarding MCP requests from chat clients.\n")
	return nil
}

// ---------------------------------------------------------------------------
// serve
// ---------------------------------------------------------------------------

func newCloudServeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Dispatch MCP requests from the skills.new relay against the local vault",
		Long: "Keep a WebSocket open to the skills.new relay and run each inbound\n" +
			"MCP JSON-RPC request against the vault configured for this sx install.\n" +
			"The process runs in the foreground; Ctrl+C exits cleanly.\n\n" +
			"Reconnects automatically on transient network errors with exponential\n" +
			"backoff. Requires `sx cloud attach` (or `sx cloud connect`) to have\n" +
			"stored a machine token first.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCloudServe(cmd)
		},
	}
	return cmd
}

func runCloudServe(cmd *cobra.Command) error {
	out := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())
	log := logger.Get()

	cred, err := cloud.Load()
	if err != nil {
		if errors.Is(err, cloud.ErrNoCredential) {
			return errors.New("no relay credential found; run `sx cloud connect` first")
		}
		return err
	}

	// Load the local vault config so we can build the same MCP server
	// that ``sx serve`` (stdio) uses. The server factory runs per
	// reconnect to keep state fresh.
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load sx config (is this directory initialized?): %w", err)
	}
	log.Info("loaded sx config for cloud serve", "vault_type", cfg.Type)

	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigChan)
	go func() {
		select {
		case <-sigChan:
			out.Info("\nShutting down sx cloud serve...")
			cancel()
		case <-ctx.Done():
		}
	}()

	out.Info(fmt.Sprintf("Serving relay %s via %s", cred.RelayGID, cred.RelayBaseURL))
	out.Printf("%s", "Press Ctrl+C to stop.\n")

	err = cloud.Serve(ctx, cloud.ServeOptions{
		Credential:       cred,
		MCPServerFactory: buildCloudServeMCPServer,
	})
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// ---------------------------------------------------------------------------
// status
// ---------------------------------------------------------------------------

func newCloudStatusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the currently-attached skills.new relay",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCloudStatus(cmd)
		},
	}
	return cmd
}

func runCloudStatus(cmd *cobra.Command) error {
	out := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())
	cred, err := cloud.Load()
	if err != nil {
		if errors.Is(err, cloud.ErrNoCredential) {
			out.Printf("%s", "No relay attached. Run `sx cloud connect` to create one.\n")
			return nil
		}
		return err
	}
	path, _ := cloud.Path()
	wsURL, _ := cred.WebSocketURL()
	masked := maskToken(cred.MachineToken)
	out.Printf("%s", fmt.Sprintf("Relay GID:        %s\n", cred.RelayGID))
	out.Printf("%s", fmt.Sprintf("Relay base URL:   %s\n", cred.RelayBaseURL))
	out.Printf("%s", fmt.Sprintf("WebSocket URL:    %s\n", wsURL))
	out.Printf("%s", fmt.Sprintf("Machine token:    %s\n", masked))
	out.Printf("%s", fmt.Sprintf("Credential file:  %s\n", path))
	if mcpURL := mcpEndpointURL(cred.RelayBaseURL); mcpURL != "" {
		out.Printf("%s", fmt.Sprintf("MCP endpoint:     %s  (paste into claude.ai/chatgpt.com)\n", mcpURL))
	}
	return nil
}

// maskToken shows only the first 4 characters so “sx cloud status“ is
// copy-pasteable into support tickets without leaking the secret.
func maskToken(token string) string {
	if len(token) <= 4 {
		return "****"
	}
	return token[:4] + "..." + strings.Repeat("*", 8)
}

// mcpEndpointURL derives the /mcp/assets/ URL that the user pastes into
// claude.ai or chatgpt.com. Returns "" if the base URL doesn't parse.
func mcpEndpointURL(base string) string {
	u, err := url.Parse(base)
	if err != nil {
		return ""
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/mcp/assets/"
	return u.String()
}

// ---------------------------------------------------------------------------
// revoke
// ---------------------------------------------------------------------------

func newCloudRevokeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "revoke",
		Short: "Delete the local skills.new relay credential",
		Long: "Remove the locally-stored machine token. The relay itself is NOT\n" +
			"revoked on the server side — skills.new still considers it active\n" +
			"until someone re-runs `sx cloud connect` (which rotates the token)\n" +
			"or an admin explicitly revokes it. To regain access from this machine,\n" +
			"run `sx cloud connect` again.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())
			if err := cloud.Delete(); err != nil {
				return err
			}
			out.Success("Removed local relay credential")
			return nil
		},
	}
	return cmd
}
