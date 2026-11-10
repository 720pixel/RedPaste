#!/bin/sh
# Download the RedPaste release binary for this machine, verify it against the
# release checksums and install it to ~/.local/bin (override with BINDIR).
# Pin a release with VERSION=v1.0.0. Read it before you pipe it to a shell.
set -eu

REPO="720pixel/RedPaste"
BINDIR="${BINDIR:-$HOME/.local/bin}"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  aarch64|arm64) arch="arm64" ;;
  *) echo "unsupported arch: $arch" >&2; exit 1 ;;
esac

case "$os" in
  linux|darwin) : ;;
  *)
    echo "unsupported OS: $os" >&2
    echo "on Windows, download redpaste-server_windows_${arch}.exe from https://github.com/$REPO/releases/latest" >&2
    exit 1
    ;;
esac

tag="${VERSION:-latest}"
if [ "$tag" = "latest" ]; then
  base="https://github.com/$REPO/releases/latest/download"
else
  base="https://github.com/$REPO/releases/download/$tag"
fi

asset="redpaste-server_${os}_${arch}"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "downloading $asset ($tag) ..."
curl -fsSL "$base/$asset" -o "$tmp/redpaste-server"
curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt"

if command -v sha256sum >/dev/null 2>&1; then
  got="$(sha256sum "$tmp/redpaste-server" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
  got="$(shasum -a 256 "$tmp/redpaste-server" | awk '{print $1}')"
else
  echo "need sha256sum or shasum to verify the download" >&2
  exit 1
fi
want="$(awk -v a="$asset" '$2 == a || $2 == "*" a { print $1; exit }' "$tmp/checksums.txt")"
if [ -z "$want" ]; then
  echo "no checksum for $asset in checksums.txt, aborting" >&2
  exit 1
fi
if [ "$want" != "$got" ]; then
  echo "checksum mismatch for $asset, aborting" >&2
  exit 1
fi
echo "checksum ok"

mkdir -p "$BINDIR"
install -m 0755 "$tmp/redpaste-server" "$BINDIR/redpaste-server"

echo "installed to $BINDIR/redpaste-server"
echo
echo "run it with:  redpaste-server"
case ":$PATH:" in
  *":$BINDIR:"*) : ;;
  *) echo "note: add $BINDIR to your PATH" ;;
esac
