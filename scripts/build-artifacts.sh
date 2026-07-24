#!/usr/bin/env bash
# 9.5-R1: Clean-clone build entrypoint for pokit daemon + APK artifact bundle.
# Starts from a clean clone of EXPECTED_SOURCE_SHA and either produces a
# complete matched bundle or fails closed.
set -euo pipefail

EXPECTED_SOURCE_SHA="${EXPECTED_SOURCE_SHA:-}"
ARTIFACT_DIR="${ARTIFACT_DIR:-pokit-alpha-device-artifacts}"

# ── Stage 1: Validate checkout ──

if [ ! -d .git ]; then
  echo "FATAL: not a real clone (.git is not a directory)" >&2
  exit 1
fi

ACTUAL_SHA=$(git rev-parse HEAD)
if [ -n "$EXPECTED_SOURCE_SHA" ] && [ "$ACTUAL_SHA" != "$EXPECTED_SOURCE_SHA" ]; then
  echo "FATAL: source SHA mismatch: expected $EXPECTED_SOURCE_SHA, got $ACTUAL_SHA" >&2
  exit 1
fi

if [ -n "$(git status --porcelain)" ]; then
  echo "FATAL: working tree is not clean" >&2
  exit 1
fi

echo "[1/7] Checkout OK: $ACTUAL_SHA"

# ── Stage 2: Validate toolchain ──

echo "[2/7] Validating toolchain..."

# macOS
[[ "$(uname -s)" == "Darwin" ]] || { echo "FATAL: requires macOS" >&2; exit 1; }
[[ "$(uname -m)" == "arm64" ]] || { echo "FATAL: requires arm64" >&2; exit 1; }
echo "  macOS $(sw_vers -productVersion) arm64 OK"

# Go
go version | grep -q "go1.26" || { echo "FATAL: requires Go 1.26.x" >&2; exit 1; }
echo "  $(go version) OK"

# Node
NODE_VERSION=$(node --version)
[[ "$NODE_VERSION" == v20.* ]] || { echo "FATAL: requires Node 20.x, got $NODE_VERSION" >&2; exit 1; }
echo "  Node $NODE_VERSION OK"

# npm (via Corepack)
NPM_VERSION=$(npm --version)
[[ "$NPM_VERSION" == 10.* ]] || { echo "FATAL: requires npm 10.x, got $NPM_VERSION" >&2; exit 1; }
echo "  npm $NPM_VERSION OK"

# JDK (Temurin/Adoptium 21)
JAVA_PROPS=$(java -XshowSettings:properties -version 2>&1) || { echo "FATAL: java not found" >&2; exit 1; }
echo "$JAVA_PROPS" | grep 'java.vendor ' | grep -qE 'Temurin|Adoptium' || { echo "FATAL: JDK vendor is not Temurin/Adoptium" >&2; exit 1; }
echo "$JAVA_PROPS" | grep 'java.version ' | grep -q '21\.' || { echo "FATAL: JDK version is not 21.x" >&2; exit 1; }
echo "  JDK Temurin 21 OK"

# Android SDK
SDK_DIR="${ANDROID_HOME:-$HOME/Library/Android/sdk}"
[ -d "$SDK_DIR/platforms/android-36" ] || { echo "FATAL: platform android-36 not installed in $SDK_DIR" >&2; exit 1; }
[ -d "$SDK_DIR/build-tools/36."* ] || { echo "FATAL: build-tools 36.x not installed in $SDK_DIR" >&2; exit 1; }
echo "  Android SDK 36 OK"

# ── Stage 3: Install dependencies ──

echo "[3/7] Installing mobile dependencies..."
cd mobile
npm ci || { echo "FATAL: npm ci failed" >&2; exit 1; }
cd ..
echo "  npm ci OK"

# ── Stage 4: Prebuild Android ──

echo "[4/7] Prebuild Android..."
cd mobile
npx expo prebuild --platform android --no-install || { echo "FATAL: expo prebuild failed" >&2; exit 1; }
cd ..
echo "  prebuild OK"

# ── Stage 5: Validate generated manifest ──

echo "[5/7] Validating generated manifest..."
MANIFEST="mobile/android/app/src/main/AndroidManifest.xml"
grep -q 'android:usesCleartextTraffic="true"' "$MANIFEST" || { echo "FATAL: usesCleartextTraffic missing in generated manifest" >&2; exit 1; }
grep -q 'com.pokit.mobile' "$MANIFEST" || { echo "FATAL: package name mismatch" >&2; exit 1; }
grep -q 'android.permission.CAMERA' "$MANIFEST" || { echo "FATAL: CAMERA permission missing" >&2; exit 1; }
echo "  Manifest OK: cleartext, package, camera verified"

# ── Stage 6: Build daemon ──

echo "[6/7] Building daemon..."
cd companion-daemon
COMMIT_TIME=$(git log -1 --format=%ct)
go build -trimpath -buildvcs=true \
  -ldflags "-X main.cliBuildTime=$(date -u -r $COMMIT_TIME +%Y-%m-%dT%H:%M:%SZ)" \
  -o "../$ARTIFACT_DIR/pokit-daemon" \
  ./cmd/devremote || { echo "FATAL: daemon build failed" >&2; exit 1; }
cd ..

# Validate daemon identity
DAEMON_VCS=$(go version -m "$ARTIFACT_DIR/pokit-daemon" | grep 'vcs.revision=' || true)
DAEMON_DIRTY=$(go version -m "$ARTIFACT_DIR/pokit-daemon" | grep 'vcs.modified=true' || true)
[ -n "$DAEMON_VCS" ] || { echo "FATAL: daemon missing vcs.revision" >&2; exit 1; }
[ -z "$DAEMON_DIRTY" ] || { echo "FATAL: daemon vcs.modified=true" >&2; exit 1; }
DAEMON_SHA256=$(shasum -a 256 "$ARTIFACT_DIR/pokit-daemon" | awk '{print $1}')
echo "  Daemon OK: SHA256=$DAEMON_SHA256"

# ── Stage 7: Build APK ──

echo "[7/7] Building APK..."
cd mobile/android
./gradlew assembleRelease || { echo "FATAL: APK build failed" >&2; exit 1; }
cd ../..

APK_PATH=$(find mobile/android/app/build/outputs/apk/release -name '*.apk' | head -1)
[ -n "$APK_PATH" ] || { echo "FATAL: APK not found" >&2; exit 1; }
cp "$APK_PATH" "$ARTIFACT_DIR/pokit-app-release.apk"
APK_SHA256=$(shasum -a 256 "$ARTIFACT_DIR/pokit-app-release.apk" | awk '{print $1}')
echo "  APK OK: SHA256=$APK_SHA256"

# ── Freeze artifacts ──

echo "$ACTUAL_SHA" > "$ARTIFACT_DIR/SOURCE_SHA.txt"
echo "$DAEMON_SHA256" > "$ARTIFACT_DIR/DAEMON_SHA256.txt"
echo "$APK_SHA256" > "$ARTIFACT_DIR/APK_SHA256.txt"

echo ""
echo "=== DONE ==="
echo "Source: $ACTUAL_SHA"
echo "Daemon: $DAEMON_SHA256"
echo "APK:    $APK_SHA256"
echo "Artifacts: $ARTIFACT_DIR/"
