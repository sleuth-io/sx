#!/usr/bin/env bash
# Package the built sx.app for macOS distribution: a zip (the auto-update
# feed the app itself downloads) and a DMG (the first-install artifact).
#
# The artifact suffix tracks what a downloader will experience:
#   (clean name)   signed + notarized + stapled — opens with no warning
#   -unnotarized   Developer ID cert but no notary key — Gatekeeper warns
#   -unsigned      no credentials (forks, pre-enrollment) — ad-hoc signed
# The auto-updater matches "sx-app-macos-<arch>*.zip", so all three
# names feed it.
#
# Inputs (env):
#   ARCH                        arm64 | amd64 — artifact naming only
#   APP_PATH                    built bundle (default app/build/bin/sx.app)
#   MACOS_CERTIFICATE_P12       base64-encoded Developer ID .p12   (optional)
#   MACOS_CERTIFICATE_PASSWORD  password for the .p12              (optional)
#   NOTARY_KEY_ID               App Store Connect API key ID       (optional)
#   NOTARY_ISSUER_ID            App Store Connect issuer ID        (optional)
#   NOTARY_KEY                  App Store Connect .p8 key contents (optional)
#   NOTARY_TIMEOUT_SECS         max wait per notarization (default 10800)
#
# Outputs: sx-app-macos-<arch>[-unsigned|-unnotarized].{zip,dmg} next to
# the bundle; names are appended to $GITHUB_ENV (ZIP_NAME/DMG_NAME) when
# set.
set -euo pipefail

ARCH="${ARCH:?set ARCH to arm64 or amd64}"
APP_PATH="${APP_PATH:-app/build/bin/sx.app}"
OUT_DIR="$(dirname "$APP_PATH")"
APP_NAME="$(basename "$APP_PATH")"
# Set by GitHub Actions; default for local runs of the unsigned path.
RUNNER_TEMP="${RUNNER_TEMP:-$(mktemp -d)}"

SIGNED=false
KEYCHAIN=""

# Run a command with a hard cap and retries. codesign (--timestamp) and
# stapler both call out to Apple servers with no timeout of their own; a
# stall (or a locked keychain prompt) otherwise hangs the job until the
# runner's 6h limit. perl ships with macOS; alarm+exec kills on deadline.
bounded() {
  local attempt
  for attempt in 1 2 3; do
    if perl -e 'alarm shift; exec @ARGV' 180 "$@"; then
      return 0
    fi
    echo "WARNING: '$1' attempt $attempt failed or timed out" >&2
    [ "$attempt" -lt 3 ] && sleep 15
  done
  echo "ERROR: '$*' failed after 3 attempts" >&2
  return 1
}
cleanup() {
  if [ -n "$KEYCHAIN" ]; then
    security delete-keychain "$KEYCHAIN" 2>/dev/null || true
  fi
}
trap cleanup EXIT

if [ -n "${MACOS_CERTIFICATE_P12:-}" ] && [ -n "${MACOS_CERTIFICATE_PASSWORD:-}" ]; then
  echo "==> Importing Developer ID certificate into a temporary keychain"
  KEYCHAIN="$RUNNER_TEMP/sx-signing.keychain-db"
  KEYCHAIN_PASSWORD="$(uuidgen)"
  CERT_PATH="$RUNNER_TEMP/certificate.p12"
  echo "$MACOS_CERTIFICATE_P12" | base64 --decode > "$CERT_PATH"
  security create-keychain -p "$KEYCHAIN_PASSWORD" "$KEYCHAIN"
  # 6h lock timeout (matches the job's max lifetime), NOT the usual 1800:
  # notarization can hold the job for hours between signing operations,
  # and a keychain that auto-locks meanwhile makes the next codesign hang
  # forever waiting for an unlock prompt no headless runner will answer.
  security set-keychain-settings -lut 21600 "$KEYCHAIN"
  security unlock-keychain -p "$KEYCHAIN_PASSWORD" "$KEYCHAIN"
  security import "$CERT_PATH" -P "$MACOS_CERTIFICATE_PASSWORD" \
    -A -t cert -f pkcs12 -k "$KEYCHAIN"
  rm -f "$CERT_PATH"
  security set-key-partition-list -S apple-tool:,apple: -k "$KEYCHAIN_PASSWORD" "$KEYCHAIN" > /dev/null
  security list-keychains -d user -s "$KEYCHAIN" login.keychain

  IDENTITY="$(security find-identity -v -p codesigning "$KEYCHAIN" \
    | awk -F '"' '/Developer ID Application/ {print $2; exit}')"
  if [ -z "$IDENTITY" ]; then
    echo "ERROR: certificate imported but no 'Developer ID Application' identity found" >&2
    exit 1
  fi

  echo "==> Signing $APP_NAME with '$IDENTITY' (hardened runtime)"
  bounded codesign --force --options runtime --timestamp \
    --sign "$IDENTITY" "$APP_PATH"
  codesign --verify --strict --verbose=2 "$APP_PATH"
  SIGNED=true
else
  echo "==> No signing certificate configured — ad-hoc signing (artifacts stay -unsigned)"
  codesign --force --deep -s - "$APP_PATH"
fi

NOTARIZED=false

notarize() {
  # Submit one artifact and wait for the verdict. A missing App Store
  # Connect key trio downgrades to "signed but not notarized" with a loud
  # warning (return 1 → callers skip stapling); an actual rejection or
  # submission failure kills the release — a silently-unnotarized signed
  # build would look done but still trip Gatekeeper.
  #
  # Deliberately NOT `notarytool submit --wait`: that holds one connection
  # open for the whole (sometimes hours-long) review, and a single network
  # blip on the runner either errors out or wedges silently while the
  # submission continues fine server-side. Instead submit once to get a
  # submission id, then poll `notarytool info` — each poll is a fresh
  # connection, so transient failures just mean try again.
  local artifact="$1"
  if [ -z "${NOTARY_KEY_ID:-}" ] || [ -z "${NOTARY_ISSUER_ID:-}" ] || [ -z "${NOTARY_KEY:-}" ]; then
    echo "WARNING: signed but NOT notarized — Gatekeeper will still warn. Add the NOTARY_* secrets." >&2
    return 1
  fi
  local key_path="$RUNNER_TEMP/notary-key.p8"
  echo "$NOTARY_KEY" > "$key_path"

  echo "==> Notarizing $(basename "$artifact")"
  local submit_out submission_id attempt
  for attempt in 1 2 3; do
    if submit_out="$(xcrun notarytool submit "$artifact" \
      --key "$key_path" --key-id "$NOTARY_KEY_ID" --issuer "$NOTARY_ISSUER_ID" \
      --output-format json 2>&1)"; then
      break
    fi
    echo "WARNING: notarytool submit attempt $attempt failed: $submit_out" >&2
    if [ "$attempt" -eq 3 ]; then
      echo "ERROR: could not submit $(basename "$artifact") for notarization" >&2
      rm -f "$key_path"
      exit 1
    fi
    sleep 30
  done
  submission_id="$(printf '%s' "$submit_out" | sed -n 's/.*"id" *: *"\([^"]*\)".*/\1/p' | head -n 1)"
  if [ -z "$submission_id" ]; then
    echo "ERROR: could not parse submission id from notarytool output: $submit_out" >&2
    rm -f "$key_path"
    exit 1
  fi
  echo "==> Submission id: $submission_id — polling for the verdict"

  # Apple's first submissions from a new team can take hours; poll with a
  # generous cap rather than trusting one long-lived connection.
  local deadline=$(( $(date +%s) + ${NOTARY_TIMEOUT_SECS:-10800} ))
  local info_out status
  while :; do
    if info_out="$(xcrun notarytool info "$submission_id" \
      --key "$key_path" --key-id "$NOTARY_KEY_ID" --issuer "$NOTARY_ISSUER_ID" \
      --output-format json 2>&1)"; then
      status="$(printf '%s' "$info_out" | sed -n 's/.*"status" *: *"\([^"]*\)".*/\1/p' | head -n 1)"
      case "$status" in
        Accepted)
          echo "==> Notarization accepted for $(basename "$artifact")"
          rm -f "$key_path"
          return 0
          ;;
        "In Progress")
          echo "    still in progress ($(date -u +%H:%M:%S) UTC)"
          ;;
        *)
          # Invalid/Rejected — surface Apple's reasons before dying.
          echo "ERROR: notarization $status for $(basename "$artifact")" >&2
          xcrun notarytool log "$submission_id" \
            --key "$key_path" --key-id "$NOTARY_KEY_ID" --issuer "$NOTARY_ISSUER_ID" >&2 || true
          rm -f "$key_path"
          exit 1
          ;;
      esac
    else
      echo "WARNING: notary status check failed (transient network?): $info_out" >&2
    fi
    if [ "$(date +%s)" -ge "$deadline" ]; then
      echo "ERROR: notarization still not finished after ${NOTARY_TIMEOUT_SECS:-10800}s for $(basename "$artifact")" >&2
      rm -f "$key_path"
      exit 1
    fi
    sleep 30
  done
}

STAGING_ZIP="$OUT_DIR/.staging-update.zip"
echo "==> Building update zip (auto-update feed)"
ditto -c -k --sequesterRsrc --keepParent "$APP_PATH" "$STAGING_ZIP"

if [ "$SIGNED" = true ] && notarize "$STAGING_ZIP"; then
  # The ticket staples to the .app, not the zip — staple, then rebuild
  # the zip so the update feed carries the stapled bundle.
  echo "==> Stapling $APP_NAME and rebuilding the zip"
  security unlock-keychain -p "$KEYCHAIN_PASSWORD" "$KEYCHAIN"
  bounded xcrun stapler staple "$APP_PATH"
  rm -f "$STAGING_ZIP"
  ditto -c -k --sequesterRsrc --keepParent "$APP_PATH" "$STAGING_ZIP"
  NOTARIZED=true
fi

# The name suffix tracks what a downloader will actually experience:
# only a notarized+stapled build opens without a Gatekeeper warning, so
# only that build earns the clean name. Certificate-but-no-notary-key is
# a transient setup state — mark it distinctly rather than either lying
# ("-unsigned") or overpromising (clean).
if [ "$NOTARIZED" = true ]; then
  SUFFIX=""
elif [ "$SIGNED" = true ]; then
  SUFFIX="-unnotarized"
else
  SUFFIX="-unsigned"
fi
ZIP_NAME="sx-app-macos-${ARCH}${SUFFIX}.zip"
DMG_NAME="sx-app-macos-${ARCH}${SUFFIX}.dmg"
mv "$STAGING_ZIP" "$OUT_DIR/$ZIP_NAME"

echo "==> Building $DMG_NAME"
STAGING="$(mktemp -d)"
cp -R "$APP_PATH" "$STAGING/"
ln -s /Applications "$STAGING/Applications"
hdiutil create -volname "sx" -srcfolder "$STAGING" -ov -format UDZO \
  "$OUT_DIR/$DMG_NAME"
rm -rf "$STAGING"

if [ "$SIGNED" = true ]; then
  # Hours may have passed inside notarize(); re-unlock in case the
  # keychain locked anyway, and cap the signing call so a hang fails the
  # job in minutes instead of idling to the 6h runner limit.
  security unlock-keychain -p "$KEYCHAIN_PASSWORD" "$KEYCHAIN"
  bounded codesign --force --timestamp --sign "$IDENTITY" "$OUT_DIR/$DMG_NAME"
  if notarize "$OUT_DIR/$DMG_NAME"; then
    bounded xcrun stapler staple "$OUT_DIR/$DMG_NAME"
  fi
fi

echo "==> Artifacts:"
ls -la "$OUT_DIR/$ZIP_NAME" "$OUT_DIR/$DMG_NAME"
if [ -n "${GITHUB_ENV:-}" ]; then
  {
    echo "ZIP_NAME=$ZIP_NAME"
    echo "DMG_NAME=$DMG_NAME"
  } >> "$GITHUB_ENV"
fi
