# Cloud relay (claude.ai and chatgpt.com)

Most AI clients sx supports — Claude Code, Cursor, Codex, Copilot,
Gemini, Kiro, Cline — are file-based: sx writes assets to a directory
the client reads on startup. Web clients like **claude.ai** and
**chatgpt.com** can't read your filesystem, so sx exposes the same vault
through a relay hosted at skills.new.

The vault content stays on your machine. The relay only forwards MCP
JSON-RPC requests over a WebSocket that your local sx process initiates.

## How the relay works

```
┌──────────────────────┐     HTTPS      ┌──────────────────┐    WebSocket    ┌────────────────┐
│  claude.ai /         │◄──────────────►│  skills.new      │◄───────────────►│  sx cloud      │
│  chatgpt.com         │   MCP request  │  relay endpoint  │   MCP forward   │  serve         │
└──────────────────────┘                └──────────────────┘                 └───────┬────────┘
                                                                                     │
                                                                                     ▼
                                                                              ┌──────────────┐
                                                                              │ local vault  │
                                                                              │ (path / git  │
                                                                              │  / sleuth)   │
                                                                              └──────────────┘
```

1. The user pastes a public MCP endpoint URL (printed by `sx cloud
   status`) into claude.ai or chatgpt.com as a custom MCP connector.
2. When the chat client invokes a tool, the relay forwards the
   JSON-RPC envelope down the persistent WebSocket your `sx cloud
   serve` process holds open.
3. sx runs the call against the local vault and returns the response
   back through the same WebSocket.

The relay never sees vault contents at rest. It only proxies live
request/response pairs while `sx cloud serve` is running.

## Quickstart

```bash
sx cloud connect       # opens skills.new, paste back the attach line
sx cloud serve         # keep this running — Ctrl+C exits
sx cloud status        # prints the MCP endpoint URL to paste into the chat client
```

Then in claude.ai or chatgpt.com, add a custom MCP connector pointing
at the URL printed by `sx cloud status` (the line labelled `MCP
endpoint`).

## Commands

### `sx cloud connect`

Opens the skills.new signup page (`https://app.skills.new/relay/connect`
by default; override with `SX_CLOUD_URL`). After you confirm your email
the page prints an `sx cloud attach --url=... --token=...` line; paste
it back into the terminal and `connect` parses it and persists the
credential.

| Flag           | Effect                                                                        |
|----------------|-------------------------------------------------------------------------------|
| `--no-browser` | Print the signup URL instead of launching a browser (useful over SSH).        |
| `--force`      | Allow swapping to a different relay when one is already attached.             |

### `sx cloud attach --url=... --token=...`

Persists the machine token directly without going through the browser
flow. Used by paste-back from the signup page, but also runnable on its
own (e.g. on a headless machine after copying the values from another
host).

Before saving, `attach` runs a 10-second probe against the relay and
fails with a clear message if the token is rejected — a typo otherwise
only surfaces later when `serve` can't connect.

### `sx cloud serve`

Holds the WebSocket open and dispatches inbound MCP calls against the
vault that `sx init` configured for this directory. Runs in the
foreground; Ctrl+C exits cleanly. Reconnects automatically with
exponential backoff; a connection that ran longer than 60 seconds
resets the backoff so a long-lived session that drops once doesn't
retry at the max delay forever.

`serve` requires a credential — run `sx cloud connect` (or `sx cloud
attach`) first.

### `sx cloud status`

Prints the attached relay's metadata, the WebSocket URL, a masked
preview of the machine token, the credential file location, and the
**MCP endpoint URL** to paste into claude.ai or chatgpt.com.

### `sx cloud revoke`

Removes the local credential. The relay is **not** revoked
server-side; skills.new still considers the token active until you
re-run `sx cloud connect` (which rotates it) or invalidate it from the
skills.new UI. Use `revoke` for hygiene; use the skills.new UI to kill
a leaked token.

## What the chat client sees

`sx cloud serve` builds a fresh MCP server per reconnect from the
local vault. The exposed tool set depends on the vault type:

| Vault type | Tools exposed                                                                  |
|------------|--------------------------------------------------------------------------------|
| Path       | `list_my_assets`, `load_my_asset`, `load_my_asset_file`                        |
| Git        | `list_my_assets`, `load_my_asset`, `load_my_asset_file`                        |
| Sleuth     | Vault-defined tool list (see [mcp-spec.md](mcp-spec.md))                       |

Path and git vaults use the same asset shim, so claude.ai and
chatgpt.com see identical surfaces regardless of how the vault is
backed.

## Credential storage

Non-secret metadata (relay base URL, relay GID) is written to a TOML
file at `<config-dir>/cloud.toml` with `0600` permissions. The machine
token itself is held in the OS keyring:

- macOS Keychain
- Windows Credential Manager
- freedesktop Secret Service (Linux desktops)

If no keyring is reachable (headless Linux, containerized CI runners,
etc.) sx falls back to writing the token into the same `0600` TOML
file and prints a warning. Better than refusing to work, but worth
knowing about for shared hosts.

The keyring entry is keyed by the relay GID, so multiple relays on one
machine coexist without collision (only one is active at a time, but
revoking a stale entry from the OS keychain UI works for any of them).

## Token rotation and multiple machines

Running `sx cloud connect` from a second machine mints a new token and
invalidates the previous one. The first sx instance stops serving on
its next frame. This is intentional: one relay = one active machine
token. To run sx from multiple machines simultaneously, attach each
machine to a different relay via separate skills.new signups.

Re-running `connect` against the **same** relay rotates the token in
place and is always allowed. Pointing sx at a **different** relay when
one is already attached requires `--force`.

## Troubleshooting

**"relay rejected the machine token"** — the token has been rotated or
revoked. Run `sx cloud connect` again to mint a fresh one.

**"could not reach relay to verify credential"** — DNS / TLS / proxy
issue talking to skills.new. The 10s probe timed out before the
handshake completed.

**`serve` keeps reconnecting** — check `sx cloud status` for the right
relay GID and confirm skills.new shows the relay as active. A
revoked-server-side token will refuse the WebSocket handshake on every
attempt.

**No tools visible in claude.ai/chatgpt.com** — confirm the MCP
endpoint URL was pasted into the chat client's custom connector slot
(not a generic web search/URL field), and that `sx cloud serve` is
running in a terminal somewhere.

## Limits

- The chat client must support custom MCP connectors. Both claude.ai
  and chatgpt.com do, with their respective UI flows.
- Per-call timeout is 25 seconds. Tool calls that take longer surface
  a timeout to the chat client rather than hanging indefinitely.
- `sx cloud serve` is a foreground process. Wrap it in your shell's
  preferred service runner (`launchd`, `systemd --user`, `pm2`,
  `tmux`) if you want it to survive reboots.
