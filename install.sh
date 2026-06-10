#!/bin/sh
# legacy-map installer — downloads the latest release binary.
# Usage: curl -fsSL https://raw.githubusercontent.com/drkilla/legacy-map/main/install.sh | sh
set -e

REPO="drkilla/legacy-map"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)

case "$arch" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *)
    echo "Unsupported architecture: $arch" >&2
    exit 1
    ;;
esac

case "$os" in
  linux | darwin) ;;
  *)
    echo "Unsupported OS: $os — download a binary from https://github.com/$REPO/releases" >&2
    exit 1
    ;;
esac

tag=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name"' | head -1 | cut -d'"' -f4)
if [ -z "$tag" ]; then
  echo "Could not determine the latest release — check https://github.com/$REPO/releases" >&2
  exit 1
fi

url="https://github.com/$REPO/releases/download/$tag/legacy-map_${os}_${arch}.tar.gz"
echo "Downloading legacy-map $tag ($os/$arch)..."

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

curl -fsSL "$url" -o "$tmp/legacy-map.tar.gz"
tar -xzf "$tmp/legacy-map.tar.gz" -C "$tmp"

mkdir -p "$INSTALL_DIR"
install -m 0755 "$tmp/legacy-map" "$INSTALL_DIR/legacy-map"
echo "✓ Installed to $INSTALL_DIR/legacy-map"

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) echo "⚠ $INSTALL_DIR is not in your PATH — add: export PATH=\"$INSTALL_DIR:\$PATH\"" ;;
esac

"$INSTALL_DIR/legacy-map" --version
