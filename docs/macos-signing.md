# macOS signing & notarization

The app release workflow (`.github/workflows/app-release.yml`) packages
`sx.app` into a **DMG** (first install) and a **zip** (the app's
auto-update feed) via `.github/scripts/package-macos.sh`. Signing is
driven entirely by repository secrets:

| Secrets present | App | Artifacts |
|---|---|---|
| none | ad-hoc signed | `sx-app-macos-<arch>-unsigned.{zip,dmg}` — Gatekeeper warns on open |
| certificate only | Developer ID + hardened runtime | signed but **not notarized** (loud warning in the job log) — Gatekeeper still warns |
| certificate + notary key | signed, notarized, stapled | `sx-app-macos-<arch>.{zip,dmg}` — opens cleanly |

Nothing needs to change in the workflow when the secrets land — the next
tagged release picks them up.

## One-time setup (after Apple Developer Program enrollment)

### 1. Developer ID certificate → two secrets

1. In Xcode (Settings → Accounts → Manage Certificates…) or at
   [developer.apple.com/account/resources/certificates](https://developer.apple.com/account/resources/certificates),
   create a **Developer ID Application** certificate.
2. Export it from Keychain Access as a `.p12` with a strong password
   (select the certificate *and* its private key → Export).
3. Add the GitHub secrets:

```bash
base64 -i DeveloperID.p12 | gh secret set MACOS_CERTIFICATE_P12
gh secret set MACOS_CERTIFICATE_PASSWORD   # the .p12 password
```

### 2. App Store Connect API key → three secrets

Notarization authenticates with an API key, not your Apple ID password.

1. At [App Store Connect → Users and Access → Integrations](https://appstoreconnect.apple.com/access/integrations/api),
   create a **Team key** with the **Developer** role. Note the Key ID and
   the Issuer ID, and download the `.p8` file (one chance).
2. Add the secrets:

```bash
gh secret set NOTARY_KEY_ID       # e.g. 2X9R4HXF34
gh secret set NOTARY_ISSUER_ID    # UUID shown above the key list
gh secret set NOTARY_KEY < AuthKey_XXXX.p8
```

## Verifying a release

On a signed release, spot-check the DMG on any Mac:

```bash
spctl --assess --type open --context context:primary-signature -v sx-app-macos-arm64.dmg
xcrun stapler validate sx-app-macos-arm64.dmg
```

Both should report acceptance. Mounting the DMG and dragging `sx.app` to
Applications should open with no Gatekeeper dialog at all.

## Notes

* The zip is rebuilt after stapling so the auto-update feed also carries
  the notarization ticket; the updater (`app/update.go`) matches
  `sx-app-macos-<arch>*.zip`, so the suffix change is transparent to
  older installs.
* A notarization **rejection** fails the release on purpose — a signed
  but unnotarized build looks done and still trips Gatekeeper.
* Windows signing (Authenticode / Azure Trusted Signing) is a separate
  effort and not covered here.
