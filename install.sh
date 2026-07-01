#!/bin/sh
# Grab the right RedPaste release binary, verify its checksum, drop it in
# ~/.local/bin. Read it before you pipe it to a shell — that's just good sense.
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
  *) echo "unsupported OS: $os (use the Docker image or build from source)" >&2; exit 1 ;;
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

echo "downloading $asset ..."
curl -fsSL "$base/$asset" -o "$tmp/redpaste-server"
curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt" || true

if [ -f "$tmp/checksums.txt" ] && command -v sha256sum >/dev/null 2>&1; then
  want="$(grep "$asset" "$tmp/checksums.txt" | awk '{print $1}' | head -1)"
  got="$(sha256sum "$tmp/redpaste-server" | awk '{print $1}')"
  if [ -n "$want" ] && [ "$want" != "$got" ]; then
    echo "checksum mismatch, aborting" >&2
    exit 1
  fi
fi

mkdir -p "$BINDIR"
install -m 0755 "$tmp/redpaste-server" "$BINDIR/redpaste-server"

echo "installed to $BINDIR/redpaste-server"
echo
echo "run it with:  redpaste-server"
case ":$PATH:" in
  *":$BINDIR:"*) : ;;
  *) echo "note: add $BINDIR to your PATH" ;;
esac
