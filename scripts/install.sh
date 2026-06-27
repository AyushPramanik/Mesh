#!/usr/bin/env bash
#
# Mesh installer. Builds the mesh, meshd, and mesh-mcp binaries from source and
# installs them to a bin directory on your PATH.
#
#   curl -fsSL https://raw.githubusercontent.com/AyushPramanik/Mesh/main/scripts/install.sh | sh
#
# Requires git and Go 1.25+. Override the install dir with MESH_BIN=/usr/local/bin.
# (Prebuilt binaries and a Homebrew tap are planned once releases are cut.)
set -euo pipefail

REPO="https://github.com/AyushPramanik/Mesh.git"
BIN_DIR="${MESH_BIN:-$HOME/.local/bin}"

need() { command -v "$1" >/dev/null 2>&1 || { echo "mesh install: '$1' is required but not found" >&2; exit 1; }; }
need git
need go

workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT

echo "→ cloning Mesh"
git clone --depth 1 "$REPO" "$workdir/mesh" --quiet \
  || { echo "mesh install: clone failed (is $REPO reachable?)" >&2; exit 1; }

echo "→ building binaries"
( cd "$workdir/mesh" && go build -o bin/mesh ./cmd/mesh && go build -o bin/meshd ./cmd/meshd && go build -o bin/mesh-mcp ./cmd/mesh-mcp )

mkdir -p "$BIN_DIR"
install "$workdir/mesh/bin/mesh" "$workdir/mesh/bin/meshd" "$workdir/mesh/bin/mesh-mcp" "$BIN_DIR/"

echo "✓ installed mesh, meshd, mesh-mcp to $BIN_DIR"
case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) echo "  add it to your PATH:  export PATH=\"$BIN_DIR:\$PATH\"" ;;
esac
echo "  next:  cd your-repo && mesh init"
