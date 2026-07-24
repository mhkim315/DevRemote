#!/usr/bin/env bash
# 9.5-R4: Clean-clone build entrypoint for pokit daemon + APK artifact bundle.
# REQUIRED: EXPECTED_SOURCE_SHA must be set to the exact production candidate.
# Produces a complete matched bundle or fails closed.
set -euo pipefail

EXPECTED_SOURCE_SHA="${EXPECTED_SOURCE_SHA:?FATAL: EXPECTED_SOURCE_SHA must be set}"
ARTIFACT_TMP="${ARTIFACT_TMP:-/tmp/pokit-artifacts-$$}"
ARTIFACT_DIR="${ARTIFACT_DIR:-pokit-alpha-device-artifacts}"
MOBILE_DIR="mobile"
DAEMON_DIR="companion-daemon"
ASSETS_DIR="$MOBILE_DIR/android/app/src/main/assets"

# ── Stage 0: Atomic artifact directory ──

echo "[0/8] Preparing artifact directory..."
rm -rf "$ARTIFACT_TMP"
mkdir -p "$ARTIFACT_TMP"
trap 'rm -rf "$ARTIFACT_TMP"' EXIT
echo "  tmp: $ARTIFACT_TMP"

# ── Stage 1: Validate checkout ──

echo "[1/8] Validating checkout..."
[ -d .git ] || { echo "FATAL: not a real clone (.git is not a directory)" >&2; exit 1; }

ACTUAL_SHA=$(git rev-parse HEAD)
[ "$ACTUAL_SHA" = "$EXPECTED_SOURCE_SHA" ] || { echo "FATAL: source SHA mismatch: expected $EXPECTED_SOURCE_SHA, got $ACTUAL_SHA" >&2; exit 1; }

[ -z "$(git status --porcelain)" ] || { echo "FATAL: working tree is not clean" >&2; exit 1; }
echo "  Checkout OK: $ACTUAL_SHA"

# ── Stage 2: Validate toolchain ──

echo "[2/8] Validating toolchain..."

# Invariant 1: macOS arm64
[[ "$(uname -s)" == "Darwin" ]] || { echo "FATAL: requires macOS" >&2; exit 1; }
[[ "$(uname -m)" == "arm64" ]] || { echo "FATAL: requires arm64" >&2; exit 1; }
echo "  macOS $(sw_vers -productVersion) arm64 OK"

# Invariant 2: Go 1.26.x
go version | grep -q "go1.26" || { echo "FATAL: requires Go 1.26.x" >&2; exit 1; }
echo "  $(go version) OK"

# Invariant 3: Node exact 20.19.4
NODE_VERSION=$(node --version)
[ "$NODE_VERSION" = "v20.19.4" ] || { echo "FATAL: requires Node v20.19.4, got $NODE_VERSION" >&2; exit 1; }
echo "  Node $NODE_VERSION OK"

# Invariant 4: npm exact 10.8.2 (Corepack)
NPM_VERSION=$(npm --version)
[ "$NPM_VERSION" = "10.8.2" ] || { echo "FATAL: requires npm 10.8.2, got $NPM_VERSION" >&2; exit 1; }
echo "  npm $NPM_VERSION OK"

# Invariant 5: JDK Temurin/Adoptium 21
JAVA_PROPS=$(java -XshowSettings:properties -version 2>&1) || { echo "FATAL: java not found" >&2; exit 1; }
echo "$JAVA_PROPS" | grep 'java.vendor ' | grep -qE 'Temurin|Adoptium' || { echo "FATAL: JDK vendor is not Temurin/Adoptium" >&2; exit 1; }
echo "$JAVA_PROPS" | grep 'java.version ' | grep -q '21\.' || { echo "FATAL: JDK version is not 21.x" >&2; exit 1; }
echo "  JDK Temurin 21 OK"

# Invariant 6: Android SDK 36
SDK_DIR="${ANDROID_HOME:-$HOME/Library/Android/sdk}"
[ -d "$SDK_DIR/platforms/android-36" ] || { echo "FATAL: platform android-36 not installed" >&2; exit 1; }
[ -d "$SDK_DIR/build-tools/36."* ] || { echo "FATAL: build-tools 36.x not installed" >&2; exit 1; }
echo "  Android SDK 36 OK"

# ── Stage 3: Install dependencies ──

echo "[3/8] Installing mobile dependencies..."
(cd "$MOBILE_DIR" && npm ci) || { echo "FATAL: npm ci failed" >&2; exit 1; }
echo "  npm ci OK"

# ── Stage 4: Prebuild Android ──

echo "[4/8] Prebuild Android..."
(cd "$MOBILE_DIR" && npx expo prebuild --platform android --no-install) || { echo "FATAL: expo prebuild failed" >&2; exit 1; }

# Set Gradle wrapper version (expo-build-properties may not control this in all SDK versions)
GRADLE_PROPS="$MOBILE_DIR/android/gradle/wrapper/gradle-wrapper.properties"
sed -i.bak 's/gradle-[0-9.]*-bin/gradle-8.13-bin/' "$GRADLE_PROPS"
echo "  prebuild OK"

# ── Stage 5: Validate generated manifest ──

echo "[5/8] Validating generated manifest..."
MANIFEST="$MOBILE_DIR/android/app/src/main/AndroidManifest.xml"

# Invariant 7: usesCleartextTraffic preserved
grep -q 'android:usesCleartextTraffic="true"' "$MANIFEST" || { echo "FATAL: usesCleartextTraffic missing" >&2; exit 1; }
echo "  cleartext OK"

# Verify package in build.gradle (AndroidManifest uses namespace, not package attr)
BUILD_GRADLE="$MOBILE_DIR/android/app/build.gradle"
grep -q 'com.pokit.mobile' "$BUILD_GRADLE" || { echo "FATAL: package namespace mismatch" >&2; exit 1; }
echo "  package OK"

grep -q 'android.permission.CAMERA' "$MANIFEST" || { echo "FATAL: CAMERA permission missing" >&2; exit 1; }
echo "  camera OK"
echo "  Manifest OK"

# ── Stage 6: Build daemon ──

echo "[6/8] Building daemon..."
(cd "$DAEMON_DIR" && \
  COMMIT_TIME=$(git log -1 --format=%ct) && \
  go build -trimpath -buildvcs=true \
    -ldflags "-X main.cliBuildTime=$(date -u -r "$COMMIT_TIME" +%Y-%m-%dT%H:%M:%SZ)" \
    -o "../$ARTIFACT_TMP/pokit-daemon" \
    ./cmd/devremote) || { echo "FATAL: daemon build failed" >&2; exit 1; }

# Validate daemon identity
DAEMON_VCS=$(go version -m "$ARTIFACT_TMP/pokit-daemon" | grep 'vcs.revision=' || true)
DAEMON_DIRTY=$(go version -m "$ARTIFACT_TMP/pokit-daemon" | grep 'vcs.modified=true' || true)
[ -n "$DAEMON_VCS" ] || { echo "FATAL: daemon missing vcs.revision" >&2; exit 1; }
[ -z "$DAEMON_DIRTY" ] || { echo "FATAL: daemon vcs.modified=true" >&2; exit 1; }
DAEMON_SHA256=$(shasum -a 256 "$ARTIFACT_TMP/pokit-daemon" | awk '{print $1}')
echo "  Daemon OK: SHA256=$DAEMON_SHA256"

# ── Stage 7: Inject APK identity assets ──

echo "[7/8] Injecting APK identity assets..."
mkdir -p "$ASSETS_DIR"
echo "$ACTUAL_SHA" > "$ASSETS_DIR/SOURCE_SHA.txt"
echo "$DAEMON_SHA256" > "$ASSETS_DIR/DAEMON_SHA256.txt"
echo "  Assets: SOURCE_SHA.txt + DAEMON_SHA256.txt"

# Validate Gradle wrapper
WRAPPER_PROPS="$MOBILE_DIR/android/gradle/wrapper/gradle-wrapper.properties"
grep -q 'gradle-8.13' "$WRAPPER_PROPS" || { echo "FATAL: Gradle wrapper is not 8.13" >&2; exit 1; }
echo "  Gradle wrapper OK"

# ── Stage 8: Build APK ──

echo "[8/8] Building APK..."
(cd "$MOBILE_DIR/android" && ./gradlew assembleRelease --no-daemon) || { echo "FATAL: APK build failed" >&2; exit 1; }

APK_PATH=$(find "$MOBILE_DIR/android/app/build/outputs/apk/release" -name '*.apk' | head -1)
[ -n "$APK_PATH" ] || { echo "FATAL: APK not found" >&2; exit 1; }
cp "$APK_PATH" "$ARTIFACT_TMP/pokit-app-release.apk"
APK_SHA256=$(shasum -a 256 "$ARTIFACT_TMP/pokit-app-release.apk" | awk '{print $1}')
echo "  APK OK: SHA256=$APK_SHA256"

# ── Freeze artifacts ──

echo "$ACTUAL_SHA" > "$ARTIFACT_TMP/SOURCE_SHA.txt"
echo "$DAEMON_SHA256" > "$ARTIFACT_TMP/DAEMON_SHA256.txt"
echo "$APK_SHA256" > "$ARTIFACT_TMP/APK_SHA256.txt"

# Invariant 8: Atomic atomically swap artifact directory
rm -rf "$ARTIFACT_DIR"
mv "$ARTIFACT_TMP" "$ARTIFACT_DIR"

echo ""
echo "=== DONE ==="
echo "Source: $ACTUAL_SHA"
echo "Daemon: $DAEMON_SHA256"
echo "APK:    $APK_SHA256"
echo "Artifacts: $ARTIFACT_DIR/"
