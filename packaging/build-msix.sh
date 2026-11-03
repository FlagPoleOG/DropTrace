#!/usr/bin/env bash
# Builds the Microsoft Store package. The three identity values come from
# Partner Center > your app > Product identity.
#
#   IDENTITY_NAME=... PUBLISHER=CN=... PUBLISHER_DISPLAY=... VERSION=0.3.1.0 \
#   MAKEMSIX=/path/to/makemsix packaging/build-msix.sh
#
# makemsix comes from https://github.com/microsoft/msix-packaging. On Windows,
# MakeAppx.exe from the Windows SDK also works with the staged folder.
set -euo pipefail
cd "$(dirname "$0")/.."
: "${IDENTITY_NAME:?Set IDENTITY_NAME from Partner Center}"
: "${PUBLISHER:?Set PUBLISHER from Partner Center}"
: "${PUBLISHER_DISPLAY:?Set PUBLISHER_DISPLAY from Partner Center}"
: "${VERSION:?Set VERSION with four parts ending in .0, such as 0.3.1.0}"
: "${DONATE_URL:=https://ko-fi.com/flagpoleog}"
stage=dist/msix-stage
rm -rf "$stage"; mkdir -p "$stage/assets"
GOOS=windows GOARCH=amd64 go build -trimpath \
  -ldflags "-s -w -H=windowsgui -X main.Version=${VERSION%.0} -X main.DonateURL=$DONATE_URL" -o "$stage/DropTrace.exe" .
cp "$stage/DropTrace.exe" "$stage/DropTraceStartup.exe"
cp packaging/assets/*.png "$stage/assets/"
sed -e "s|__IDENTITY_NAME__|$IDENTITY_NAME|" -e "s|__PUBLISHER__|$PUBLISHER|" \
    -e "s|__PUBLISHER_DISPLAY__|$PUBLISHER_DISPLAY|" -e "s|__VERSION__|$VERSION|" \
    packaging/AppxManifest.template.xml > "$stage/AppxManifest.xml"
"${MAKEMSIX:-makemsix}" pack -d "$stage" -p "dist/DropTrace-$VERSION.msix"
echo "Built dist/DropTrace-$VERSION.msix"
