#!/bin/sh
# Install Envry on Linux or macOS:
#
#   curl -sSfL https://raw.githubusercontent.com/arifurrahmanns/envry/main/install.sh | sh
#
# Environment variables:
#   ENVRY_VERSION      release tag to install, e.g. v0.1.0 (default: the latest release)
#   ENVRY_INSTALL_DIR  directory for the binary (default: /usr/local/bin, using sudo
#                      when needed, or ~/.local/bin when sudo is unavailable)
#
# The download is verified against the release's checksums.txt.
set -eu

repo="arifurrahmanns/envry"

fail() {
	echo "envry install: $*" >&2
	exit 1
}

case "$(uname -s)" in
Linux) os=linux ;;
Darwin) os=darwin ;;
*) fail "unsupported system $(uname -s); on Windows, use Scoop or download a zip from https://github.com/$repo/releases" ;;
esac

case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) fail "unsupported architecture $(uname -m)" ;;
esac

if [ -n "${ENVRY_VERSION:-}" ]; then
	base="https://github.com/$repo/releases/download/$ENVRY_VERSION"
else
	base="https://github.com/$repo/releases/latest/download"
fi

if command -v curl >/dev/null 2>&1; then
	fetch() { curl -fsSL -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
	fetch() { wget -q -O "$2" "$1"; }
else
	fail "curl or wget is required"
fi

if command -v sha256sum >/dev/null 2>&1; then
	sha256() { sha256sum "$1" | awk '{ print $1 }'; }
elif command -v shasum >/dev/null 2>&1; then
	sha256() { shasum -a 256 "$1" | awk '{ print $1 }'; }
else
	fail "sha256sum or shasum is required to verify the download"
fi

archive="envry-$os-$arch.tar.gz"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

echo "Downloading $archive"
fetch "$base/$archive" "$tmp/$archive" || fail "could not download $base/$archive"
fetch "$base/checksums.txt" "$tmp/checksums.txt" || fail "could not download $base/checksums.txt"

expected=$(awk -v name="$archive" '{ f = $2; sub(/^\*/, "", f) } f == name { print $1 }' "$tmp/checksums.txt")
[ -n "$expected" ] || fail "$archive is not listed in checksums.txt"
[ "$(sha256 "$tmp/$archive")" = "$expected" ] || fail "checksum mismatch for $archive"

tar -xzf "$tmp/$archive" -C "$tmp" envry
chmod 0755 "$tmp/envry"

place() {
	mkdir -p "$1" 2>/dev/null && cp "$tmp/envry" "$1/envry" 2>/dev/null
}

if [ -n "${ENVRY_INSTALL_DIR:-}" ]; then
	dir=$ENVRY_INSTALL_DIR
	place "$dir" || fail "cannot write to $dir"
elif place /usr/local/bin; then
	dir=/usr/local/bin
elif command -v sudo >/dev/null 2>&1; then
	dir=/usr/local/bin
	echo "Installing to $dir with sudo"
	sudo mkdir -p "$dir" && sudo cp "$tmp/envry" "$dir/envry" || fail "cannot write to $dir"
else
	dir="$HOME/.local/bin"
	place "$dir" || fail "cannot write to $dir"
fi

echo "Installed $("$dir/envry" version | head -n 1) to $dir/envry"
case ":$PATH:" in
*":$dir:"*) ;;
*) echo "Note: $dir is not in your PATH; add it to run envry by name." ;;
esac
