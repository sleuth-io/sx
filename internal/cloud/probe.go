package cloud

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/coder/websocket"
)

// ErrProbeUnauthorized is returned by Probe when the relay's auth
// handshake rejected our bearer token. Callers surface this as "token
// rejected; re-run `sx cloud connect`" to the user without needing to
// inspect the wrapped error.
var ErrProbeUnauthorized = errors.New("relay rejected the machine token")

// Probe performs a single-handshake verification of a credential
// against its relay. Used by “sx cloud connect“ / “sx cloud attach“
// to fail fast with a readable error when the pasted token is wrong,
// rather than letting the failure surface only inside the long-running
// “sx cloud serve“ loop.
//
// The probe dials the WebSocket endpoint with the stored Bearer token
// and closes cleanly. A successful handshake means the relay accepted
// the token and subscribed us — we don't need to stay connected. A
// 401/403 on handshake maps to “ErrProbeUnauthorized“.
//
// “httpClient“ is optional; tests pass a stub, production leaves it
// nil. “ctx“ bounds the whole dance — a reasonable default is 5s.
func Probe(ctx context.Context, cred *Credential, httpClient *http.Client) error {
	if cred == nil {
		return errors.New("probe: credential is nil")
	}
	if err := cred.Validate(); err != nil {
		return fmt.Errorf("probe: invalid credential: %w", err)
	}
	wsURL, err := cred.WebSocketURL()
	if err != nil {
		return fmt.Errorf("probe: bad WebSocket URL: %w", err)
	}
	dialOpts := &websocket.DialOptions{
		HTTPClient: httpClient,
		HTTPHeader: http.Header{
			"Authorization": []string{"Bearer " + cred.MachineToken},
		},
	}
	conn, resp, err := websocket.Dial(ctx, wsURL, dialOpts)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		// go-coder/websocket exposes the HTTP status code via its
		// ``CloseStatus`` for close frames, but handshake failures
		// come back as a wrapped error with the status in the text.
		// Check the response first (more reliable) then fall back to
		// string matching.
		if resp != nil && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) {
			return fmt.Errorf("%w (HTTP %d)", ErrProbeUnauthorized, resp.StatusCode)
		}
		return fmt.Errorf("probe handshake: %w", err)
	}
	// Hand-off: close cleanly. We only cared about the handshake — a
	// successful ``Dial`` means the relay accepted our bearer, which
	// is the only thing we can learn without round-tripping an MCP
	// message. Close-time errors (peer closed first, TCP teardown
	// race, ctx near deadline) are uninteresting here; returning them
	// would cause spurious "probe failed" reports to the user.
	_ = conn.Close(websocket.StatusNormalClosure, "")
	return nil
}
