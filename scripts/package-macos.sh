#!/usr/bin/env bash
# Build the macOS release asset end to end:
#
#   scripts/package-macos.sh <version> [outdir]
#
# -> <outdir>/callmqtt_<version>_darwin_universal.zip   (default outdir: dist/macos)
#
# Steps: build darwin/amd64 + darwin/arm64 (same flags as the goreleaser
# Windows build, minus -H=windowsgui), lipo them into one universal binary,
# check it reports the right version, wrap it in the .app (make-app.sh),
# optionally Developer-ID-sign + notarize, and zip the app together with
# configs/example.yaml, README.md and LICENSE. The zip has the same flat
# layout as the Windows archives.
#
# CI (ci.yml / release.yml) and local runs use this one script so the
# packaging path cannot drift between "tested in a PR" and "shipped".
#
# Signing is OFF unless MACOS_SIGN_P12 is set; see sign_and_notarize below.
set -euo pipefail

if [[ $# -lt 1 || $# -gt 2 ]]; then
  echo "usage: $0 <version> [outdir]" >&2
  exit 2
fi

version=${1#v}
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
outdir=${2:-$root/dist/macos}
mkdir -p "$outdir"
outdir=$(cd "$outdir" && pwd)

work=$(mktemp -d)
cleanup() { rm -rf "$work"; }
trap cleanup EXIT

# Keep in sync with the `callmqtt` build in .goreleaser.yaml.
build() {
  local arch=$1
  (
    cd "$root"
    CGO_ENABLED=0 GOOS=darwin GOARCH="$arch" go build \
      -tags tray -trimpath \
      -ldflags "-s -w -X main.version=$version" \
      -o "$work/callmqtt-$arch" ./cmd/callmqtt
  )
}

# Optional Developer ID signing + notarization. Needs these env vars (all
# GitHub secrets in release.yml):
#   MACOS_SIGN_P12          base64 of the "Developer ID Application" .p12
#   MACOS_SIGN_PASSWORD     its password
#   MACOS_NOTARY_KEY        contents of the App Store Connect API key (.p8)
#   MACOS_NOTARY_KEY_ID     the key id
#   MACOS_NOTARY_ISSUER_ID  the issuer id
# NOTE: wired but never exercised against a real Apple account yet.
sign_and_notarize() {
  local app=$1
  local kc="$work/signing.keychain-db"
  local kc_pass
  kc_pass=$(uuidgen)
  security create-keychain -p "$kc_pass" "$kc"
  security set-keychain-settings -lut 3600 "$kc"
  security unlock-keychain -p "$kc_pass" "$kc"
  printf '%s' "$MACOS_SIGN_P12" | base64 --decode >"$work/sign.p12"
  security import "$work/sign.p12" -k "$kc" -P "$MACOS_SIGN_PASSWORD" -T /usr/bin/codesign
  security set-key-partition-list -S apple-tool:,apple: -s -k "$kc_pass" "$kc" >/dev/null
  # Search order: our keychain first, then the user's.
  security list-keychains -d user -s "$kc" login.keychain-db
  local identity
  identity=$(security find-identity -v -p codesigning "$kc" |
    awk '/Developer ID Application/ {print $2; exit}')
  [[ -n $identity ]] || { echo "no Developer ID Application identity in the .p12" >&2; exit 1; }

  codesign -s "$identity" --force --deep --options runtime --timestamp "$app"
  codesign --verify --deep --strict "$app"

  printf '%s' "$MACOS_NOTARY_KEY" >"$work/notary.p8"
  ditto -c -k --keepParent "$app" "$work/notarize.zip"
  xcrun notarytool submit "$work/notarize.zip" \
    --key "$work/notary.p8" --key-id "$MACOS_NOTARY_KEY_ID" \
    --issuer "$MACOS_NOTARY_ISSUER_ID" --wait
  xcrun stapler staple "$app"
  spctl --assess --type execute --verbose "$app"
}

echo "== building darwin/amd64 and darwin/arm64 (version $version)"
build amd64
build arm64
lipo -create -output "$work/callmqtt" "$work/callmqtt-amd64" "$work/callmqtt-arm64"
echo "universal binary archs: $(lipo -archs "$work/callmqtt")"

# Same guard as the Windows preflight in release.yml: the shipped binary must
# report the release version, not "dev". The runner is arm64, so run that slice.
want="In a Call Notification $version"
got=$("$work/callmqtt-arm64" -version)
if [[ $got != "$want" ]]; then
  echo "version check failed: wanted '$want', got '$got'" >&2
  exit 1
fi

echo "== assembling the app bundle"
stage="$work/stage"
mkdir -p "$stage/configs"
if [[ -n ${MACOS_SIGN_P12:-} ]]; then
  CODESIGN_IDENTITY=none "$root/scripts/make-app.sh" "$work/callmqtt" "$version" "$stage"
  echo "== Developer ID signing + notarization"
  sign_and_notarize "$stage/In a Call Notification.app"
else
  "$root/scripts/make-app.sh" "$work/callmqtt" "$version" "$stage"
fi
cp "$root/internal/config/example.yaml" "$stage/configs/example.yaml"
cp "$root/README.md" "$root/LICENSE" "$stage/"

zip="$outdir/callmqtt_${version}_darwin_universal.zip"
rm -f "$zip"
# ditto (not zip) keeps the bundle's permissions and metadata intact. Without
# --keepParent the stage directory's contents land at the archive root, like
# the Windows zips.
ditto -c -k --norsrc --noextattr --noqtn "$stage" "$zip"

echo "== $zip"
unzip -l "$zip"
shasum -a 256 "$zip"
