#!/bin/sh
# Package the POKIT CLI/daemon into a versioned release archive in dist/.
# This script creates artifacts only; publishing is a separate release action.
# Usage: sh scripts/package.sh
set -e

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DAEMON_DIR="$PROJECT_ROOT/companion-daemon"
DIST_DIR="$PROJECT_ROOT/dist"

VERSION=$(cd "$DAEMON_DIR" && git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_SHA=$(cd "$DAEMON_DIR" && git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ)
GOOS="${GOOS:-$(go env GOOS 2>/dev/null || echo "unknown")}"
GOARCH="${GOARCH:-$(go env GOARCH 2>/dev/null || echo "unknown")}"
ARTIFACT="pokit-${VERSION}-${GOOS}-${GOARCH}"
ARCHIVE="$DIST_DIR/$ARTIFACT.tar.gz"
STAGE_DIR=$(mktemp -d "${TMPDIR:-/tmp}/pokit-package.XXXXXX")
trap 'rm -rf "$STAGE_DIR"' EXIT HUP INT TERM

mkdir -p "$DIST_DIR"

echo "=== POKIT Package ==="
echo "Version:  $VERSION"
echo "Platform: $GOOS/$GOARCH"
echo "Artifact: $ARTIFACT"
echo "Output:   $DIST_DIR"

cd "$DAEMON_DIR"
GOOS="$GOOS" GOARCH="$GOARCH" go build \
  -ldflags="-X main.cliVersion=$VERSION -X main.cliGitSHA=$GIT_SHA -X main.cliBuildTime=$BUILD_TIME" \
  -o "$STAGE_DIR/pokit" ./cmd/devremote
tar -C "$STAGE_DIR" -czf "$ARCHIVE" pokit
SHA256=$(shasum -a 256 "$ARCHIVE" | awk '{print $1}')

echo ""
echo "Packaged: $ARCHIVE"
echo "SHA-256:  $SHA256"

# Write metadata
cat > "$DIST_DIR/$ARTIFACT.meta" << EOF
version=$VERSION
os=$GOOS
arch=$GOARCH
artifact=$(basename "$ARCHIVE")
sha256=$SHA256
build_time=$BUILD_TIME
EOF

printf '%s  %s\n' "$SHA256" "$(basename "$ARCHIVE")" > "$ARCHIVE.sha256"

echo "Metadata: $DIST_DIR/$ARTIFACT.meta"
echo "Checksum: $ARCHIVE.sha256"
echo ""
echo "Dist contents:"
ls -lh "$DIST_DIR/"
