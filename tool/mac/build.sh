#!/bin/bash
# Rebuilds the two binaries in this folder, which the Open Shutter Count app
# bundles, and fetches libusb's source:
#
#   openshuttercount          the tool, for Apple silicon Macs, with
#                             app/version.py's version
#   libusb-1.0.dylib          libusb 1.0.30, from the libusb-package wheel on PyPI
#                             (github.com/pyocd/libusb-package), checked against a
#                             pinned SHA-256
#   libusb-1.0.30.tar.bz2     libusb's own release tarball, the source of that
#                             library. libusb is LGPL-2.1, and the wheel has no
#                             source distribution, so this repository carries the
#                             source (and so does each release's source download).
#                             Checked against a pinned SHA-256; it isn't bundled in
#                             the app.
#
# Runs on macOS or Linux with Go, curl and unzip. Nothing here needs cgo, and
# the Go linker ad-hoc signs darwin/arm64 binaries itself, so no Mac is needed.
# The build is reproducible: the same Go version gives the same bytes, so it's
# pinned below, the same as the Makefile's. Any Go 1.21 or later fetches that
# version itself. Raise both on purpose, and commit the binary it makes.
set -euo pipefail

go_version="go1.27.0"

here="$(cd "$(dirname "$0")" && pwd)"
tool="$(dirname "$here")"
root="$(dirname "$tool")"

# The version is app/version.py's, the one the app inside a release has.
version="${VERSION:-$(sed -n 's/^VERSION = "\(.*\)"$/\1/p' "$root/app/version.py")}"
if [ -z "$version" ]; then
    echo "couldn't read VERSION from app/version.py" >&2
    exit 1
fi

libusb_wheel_url="https://files.pythonhosted.org/packages/52/6f/26de4e9f858ab50e87931f0be268f3c1bbfce33e8584add60da857632142/libusb_package-1.0.30.0-py3-none-macosx_11_0_arm64.whl"
libusb_wheel_sha256="4ad25f8d254bbbdd234446d5580b24d62d59bb0e690d8090457dac347f044437"

# The pinned hash is of the tarball whose signature (libusb-1.0.30.tar.bz2.asc, from
# the same release) was checked against Tormod Volden's key,
# C681 8737 9B23 DE9E FC46  651E 2C80 FF56 C683 0A0E, which the release's
# SIGNING_KEYS.txt names. A new libusb version means checking that again.
libusb_source_name="libusb-1.0.30.tar.bz2"
libusb_source_url="https://github.com/libusb/libusb/releases/download/v1.0.30/$libusb_source_name"
libusb_source_sha256="fea36f34f9156400209595e300840767ab1a385ede1dc7ee893015aea9c6dbaf"

sha256() {
    if command -v sha256sum >/dev/null; then
        sha256sum "$1" | cut -d' ' -f1
    else
        shasum -a 256 "$1" | cut -d' ' -f1
    fi
}

echo "==> building openshuttercount $version for darwin/arm64"
(
    cd "$tool"
    GOTOOLCHAIN="$go_version" CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build \
        -trimpath -buildvcs=false \
        -ldflags "-s -w -X github.com/DigitalN/Open-Shutter-Count/tool/internal/app.Version=$version" \
        -o "$here/openshuttercount" ./cmd/openshuttercount
)

echo "==> fetching libusb"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

curl -fsSL -o "$tmp/libusb.whl" "$libusb_wheel_url"

actual="$(sha256 "$tmp/libusb.whl")"
if [ "$actual" != "$libusb_wheel_sha256" ]; then
    echo "libusb wheel checksum mismatch: got $actual, expected $libusb_wheel_sha256" >&2
    exit 1
fi

unzip -p "$tmp/libusb.whl" libusb_package/libusb-1.0.dylib > "$here/libusb-1.0.dylib"
chmod 644 "$here/libusb-1.0.dylib"
chmod 755 "$here/openshuttercount"

echo "==> fetching libusb's source"
curl -fsSL -o "$tmp/$libusb_source_name" "$libusb_source_url"

actual="$(sha256 "$tmp/$libusb_source_name")"
if [ "$actual" != "$libusb_source_sha256" ]; then
    echo "libusb source checksum mismatch: got $actual, expected $libusb_source_sha256" >&2
    exit 1
fi

cp "$tmp/$libusb_source_name" "$here/$libusb_source_name"
chmod 644 "$here/$libusb_source_name"

echo "==> done"
(cd "$here" && for f in openshuttercount libusb-1.0.dylib "$libusb_source_name"; do echo "$(sha256 "$f")  $f"; done)
