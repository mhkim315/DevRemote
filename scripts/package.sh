#!/bin/sh
# Package the POKIT daemon into dist/. Dry-run only — does not publish.
# Usage: sh scripts/package.sh
set -e

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DAEMON_DIR="$PROJECT_ROOT/companion-daemon"
DIST_DIR="$PROJECT_ROOT/dist"

VERSION=$(cd "$DAEMON_DIR" && git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_SHA=$(cd "$DAEMON_DIR" && git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ)
GOOS=$(go env GOOS 2>/dev/null || echo "unknown")
GOARCH=$(go env GOARCH 2>/dev/null || echo "unknown")
ARTIFACT="pokit-daemon-${VERSION}-${GOOS}-${GOARCH}"

mkdir -p "$DIST_DIR"

echo "=== POKIT Package Dry-run ==="
echo "Version:  $VERSION"
echo "Platform: $GOOS/$GOARCH"
echo "Artifact: $ARTIFACT"
echo "Output:   $DIST_DIR"

cd "$DAEMON_DIR"
go build -ldflags="-X main.cliVersion=$VERSION -X main.cliGitSHA=$GIT_SHA -X main.cliBuildTime=$BUILD_TIME" -o "$DIST_DIR/$ARTIFACT" ./cmd/devremote

echo ""
echo "Packaged: $DIST_DIR/$ARTIFACT"
ls -lh "$DIST_DIR/$ARTIFACT"

# Write metadata
cat > "$DIST_DIR/$ARTIFACT.meta" << EOF
version=$VERSION
os=$GOOS
arch=$GOARCH
artifact=$ARTIFACT
build_time=$BUILD_TIME
EOF

echo "Metadata: $DIST_DIR/$ARTIFACT.meta"
echo ""
echo "Dist contents:"
ls -lh "$DIST_DIR/"
