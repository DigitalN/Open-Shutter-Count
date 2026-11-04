#!/bin/bash
# Builds what a GitHub release carries, into dist/:
#
#   openshuttercounter-<version>-mac.zip     double-click copy for Apple silicon
#                                            Macs: Read Shutter Count.command
#                                            and the mac/ folder, libusb included
#   openshuttercounter-<version>-<os>-<arch> command line builds
#   checksums.txt                            SHA-256 of each of those
#
# Run by .github/workflows/release.yml with VERSION set to the release's tag.
# Runs on Linux or macOS with Go, curl, unzip and zip.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
version="${VERSION:?set VERSION to the release tag, e.g. v1.0.0}"
dist="$root/dist"
ldflags="-s -w -X github.com/DigitalN/openshuttercounter/internal/app.Version=$version"

rm -rf "$dist"
mkdir -p "$dist"
cd "$root"

echo "==> Mac copy"
VERSION="$version" bash mac/build.sh

stage="$(mktemp -d)"
trap 'rm -rf "$stage"' EXIT

bundle="$stage/openshuttercounter"
mkdir -p "$bundle/mac"
cp "Read Shutter Count.command" README.md LICENSE "$bundle/"
cp mac/openshuttercounter mac/libusb-1.0.dylib mac/launcher.sh \
    "mac/Find Shutter Counter.command" mac/README.md "$bundle/mac/"
(cd "$stage" && zip -qry "$dist/openshuttercounter-$version-mac.zip" openshuttercounter)

echo "==> command line builds"
for target in linux/amd64 linux/arm64 windows/amd64 darwin/arm64; do
    os="${target%/*}"
    arch="${target#*/}"
    name="openshuttercounter-$version-$os-$arch"
    ext=""
    [ "$os" = windows ] && ext=".exe"

    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -buildvcs=false \
        -ldflags "$ldflags" -o "$dist/$name$ext" ./cmd/openshuttercounter
done

echo "==> checksums"
(
    cd "$dist"
    if command -v sha256sum >/dev/null; then
        sha256sum -- * > checksums.txt
    else
        shasum -a 256 -- * > checksums.txt
    fi
    cat checksums.txt
)
