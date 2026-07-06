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
  security set-keychain-settings -lut 1800 "$KEYCHAIN"
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
  codesign --force --options runtime --timestamp \
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
  local artifact="$1"
  if [ -z "${NOTARY_KEY_ID:-}" ] || [ -z "${NOTARY_ISSUER_ID:-}" ] || [ -z "${NOTARY_KEY:-}" ]; then
    echo "WARNING: signed but NOT notarized — Gatekeeper will still warn. Add the NOTARY_* secrets." >&2
    return 1
  fi
  local key_path="$RUNNER_TEMP/notary-key.p8"
  echo "$NOTARY_KEY" > "$key_path"
  echo "==> Notarizing $(basename "$artifact")"
  if ! xcrun notarytool submit "$artifact" \
    --key "$key_path" --key-id "$NOTARY_KEY_ID" --issuer "$NOTARY_ISSUER_ID" \
    --wait; then
    echo "ERROR: notarization failed for $(basename "$artifact")" >&2
    rm -f "$key_path"
    exit 1
  fi
  rm -f "$key_path"
}

STAGING_ZIP="$OUT_DIR/.staging-update.zip"
echo "==> Building update zip (auto-update feed)"
ditto -c -k --sequesterRsrc --keepParent "$APP_PATH" "$STAGING_ZIP"

if [ "$SIGNED" = true ] && notarize "$STAGING_ZIP"; then
  # The ticket staples to the .app, not the zip — staple, then rebuild
  # the zip so the update feed carries the stapled bundle.
  echo "==> Stapling $APP_NAME and rebuilding the zip"
  xcrun stapler staple "$APP_PATH"
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
  codesign --force --timestamp --sign "$IDENTITY" "$OUT_DIR/$DMG_NAME"
  if notarize "$OUT_DIR/$DMG_NAME"; then
    xcrun stapler staple "$OUT_DIR/$DMG_NAME"
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
