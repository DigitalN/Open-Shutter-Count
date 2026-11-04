#!/bin/bash
# Rebuilds the two binaries in this folder:
#
#   openshuttercounter   the tool, for Apple silicon Macs
#   libusb-1.0.dylib     libusb 1.0.30, from the libusb-package wheel on PyPI
#                        (github.com/pyocd/libusb-package), checked against a
#                        pinned SHA-256
#
# Runs on macOS or Linux with Go, curl and unzip. Nothing here needs cgo, and
# the Go linker ad-hoc signs darwin/arm64 binaries itself, so no Mac is needed.
# The build is reproducible: the same Go version gives the same bytes.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
root="$(dirname "$here")"

version="${VERSION:-0.0.0-dev}"

libusb_wheel_url="https://files.pythonhosted.org/packages/52/6f/26de4e9f858ab50e87931f0be268f3c1bbfce33e8584add60da857632142/libusb_package-1.0.30.0-py3-none-macosx_11_0_arm64.whl"
libusb_wheel_sha256="4ad25f8d254bbbdd234446d5580b24d62d59bb0e690d8090457dac347f044437"

echo "==> building openshuttercounter $version for darwin/arm64"
(
    cd "$root"
    CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build \
        -trimpath -buildvcs=false \
        -ldflags "-s -w -X github.com/DigitalN/openshuttercounter/internal/app.Version=$version" \
        -o "$here/openshuttercounter" ./cmd/openshuttercounter
)

echo "==> fetching libusb"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

curl -fsSL -o "$tmp/libusb.whl" "$libusb_wheel_url"

if command -v sha256sum >/dev/null; then
    actual="$(sha256sum "$tmp/libusb.whl" | cut -d' ' -f1)"
else
    actual="$(shasum -a 256 "$tmp/libusb.whl" | cut -d' ' -f1)"
fi

if [ "$actual" != "$libusb_wheel_sha256" ]; then
    echo "libusb wheel checksum mismatch: got $actual, expected $libusb_wheel_sha256" >&2
    exit 1
fi

unzip -p "$tmp/libusb.whl" libusb_package/libusb-1.0.dylib > "$here/libusb-1.0.dylib"
chmod 644 "$here/libusb-1.0.dylib"
chmod 755 "$here/openshuttercounter"

echo "==> done"
if command -v sha256sum >/dev/null; then
    (cd "$here" && sha256sum openshuttercounter libusb-1.0.dylib)
else
    (cd "$here" && shasum -a 256 openshuttercounter libusb-1.0.dylib)
fi
