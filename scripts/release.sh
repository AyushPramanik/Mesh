#!/usr/bin/env bash
#
# Build release artifacts for Mesh: cross-compiled mesh, meshd, and mesh-mcp
# binaries for each supported platform, archived with a SHA256SUMS manifest.
#
#   scripts/release.sh v0.1.0
#
# Output lands in dist/. The version is stamped into the binaries via -ldflags
# and printed by `mesh --version`, `meshd --version`, and the MCP handshake.
set -euo pipefail

VERSION="${1:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
PKG="github.com/AyushPramanik/mesh/internal/version"
LDFLAGS="-s -w -X ${PKG}.Version=${VERSION}"

PLATFORMS=(
  linux/amd64
  linux/arm64
  darwin/amd64
  darwin/arm64
  windows/amd64
)
BINARIES=(mesh meshd mesh-mcp)

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
dist="dist"
rm -rf "$dist"
mkdir -p "$dist"

echo "→ building Mesh ${VERSION}"
for platform in "${PLATFORMS[@]}"; do
  goos="${platform%/*}"
  goarch="${platform#*/}"
  ext=""
  [ "$goos" = "windows" ] && ext=".exe"

  stage="$dist/mesh_${VERSION}_${goos}_${goarch}"
  mkdir -p "$stage"
  for bin in "${BINARIES[@]}"; do
    GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 \
      go build -trimpath -ldflags "$LDFLAGS" -o "$stage/${bin}${ext}" "./cmd/${bin}"
  done
  cp README.md LICENSE "$stage/"

  archive="mesh_${VERSION}_${goos}_${goarch}"
  if [ "$goos" = "windows" ]; then
    ( cd "$dist" && zip -qr "${archive}.zip" "$(basename "$stage")" )
    echo "  packaged ${archive}.zip"
  else
    tar -czf "$dist/${archive}.tar.gz" -C "$dist" "$(basename "$stage")"
    echo "  packaged ${archive}.tar.gz"
  fi
  rm -rf "$stage"
done

echo "→ writing checksums"
( cd "$dist"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum mesh_*.tar.gz mesh_*.zip > SHA256SUMS 2>/dev/null
  else
    shasum -a 256 mesh_*.tar.gz mesh_*.zip > SHA256SUMS 2>/dev/null
  fi
)

echo "✓ artifacts in $dist/"
ls -1 "$dist"
