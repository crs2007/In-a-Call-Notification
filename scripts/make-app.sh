#!/usr/bin/env bash
# Assemble "In a Call Notification.app" around an already-built macOS binary.
#
#   scripts/make-app.sh <binary> <version> <outdir>
#
#   <binary>   the (universal) callmqtt executable
#   <version>  release version, with or without a leading "v" (e.g. 0.2.0)
#   <outdir>   directory in which the .app is created (created if missing,
#              an existing bundle of the same name is replaced)
#
# The bundle is ad-hoc signed (codesign -s -) so the bundle seal covers
# Info.plist. Set CODESIGN_IDENTITY to sign with something else, or
# CODESIGN_IDENTITY=none to skip signing. Runs locally and in CI; needs the
# macOS tools plutil and codesign (and sips + iconutil if an icon is supplied).
set -euo pipefail

if [[ $# -ne 3 ]]; then
  echo "usage: $0 <binary> <version> <outdir>" >&2
  exit 2
fi

binary=$1
version=${2#v}
outdir=$3

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
template="$root/packaging/darwin/Info.plist"
# Optional: drop a square PNG (1024x1024 ideal) here to get an app icon.
icon_png="$root/packaging/darwin/icon.png"

app_name="In a Call Notification.app"
app="$outdir/$app_name"

[[ -f $binary ]] || { echo "binary not found: $binary" >&2; exit 1; }
[[ -n $version ]] || { echo "empty version" >&2; exit 1; }
# The version is substituted into XML and sed; allow only semver-ish text.
if [[ ! $version =~ ^[0-9A-Za-z.+-]+$ ]]; then
  echo "refusing odd-looking version: $version" >&2
  exit 1
fi

rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
cp "$binary" "$app/Contents/MacOS/callmqtt"
chmod 755 "$app/Contents/MacOS/callmqtt"

sed "s/@VERSION@/$version/g" "$template" >"$app/Contents/Info.plist"

if [[ -f $icon_png ]]; then
  tmp=$(mktemp -d)
  iconset="$tmp/icon.iconset"
  mkdir -p "$iconset"
  for size in 16 32 128 256 512; do
    sips -z "$size" "$size" "$icon_png" --out "$iconset/icon_${size}x${size}.png" >/dev/null
    sips -z "$((size * 2))" "$((size * 2))" "$icon_png" --out "$iconset/icon_${size}x${size}@2x.png" >/dev/null
  done
  iconutil -c icns "$iconset" -o "$app/Contents/Resources/icon.icns"
  rm -rf "$tmp"
  plutil -insert CFBundleIconFile -string icon "$app/Contents/Info.plist"
fi

plutil -lint "$app/Contents/Info.plist"

identity=${CODESIGN_IDENTITY:--}
if [[ $identity != none ]]; then
  codesign -s "$identity" --force --deep "$app"
  codesign --verify --deep --strict "$app"
fi

echo "built $app (version $version)"
