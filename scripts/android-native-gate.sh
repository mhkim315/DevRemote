#!/bin/sh
# M3-auth-1A MANDATORY native gate — runs the real Android Keystore
# instrumentation against a connected emulator/device.
#
# This is SEPARATE from scripts/build-gate.sh (which compiles Kotlin but does
# not run Keystore instrumentation). A connected Android emulator or device is
# required. M3-auth-1A is NOT complete on a compile-only gate.
#
# Usage:
#   sh scripts/android-native-gate.sh
#
# Steps:
#   1. clean Expo prebuild (reproducible generated project)
#   2. release Kotlin compile (catches Gradle/Kotlin/autolinking errors)
#   3. connected instrumentation (real AndroidKeyStore behavior + concurrency)
set -e

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
MOBILE_DIR="$PROJECT_ROOT/mobile"

echo "=== M3-auth-1A Android native gate ==="

if ! command -v adb >/dev/null 2>&1; then
  echo "FAILED: adb not found — a connected Android emulator/device is required." >&2
  exit 1
fi
if [ -z "$(adb devices | sed '1d' | grep -w device)" ]; then
  echo "FAILED: no connected Android device/emulator (adb devices is empty)." >&2
  exit 1
fi

cd "$MOBILE_DIR"
echo "--- clean prebuild ---"
npx expo prebuild --clean --platform android --no-install

cd "$MOBILE_DIR/android"
echo "--- release Kotlin compile ---"
./gradlew :app:compileReleaseKotlin

echo "--- connected instrumentation ---"
./gradlew :pokit-device-key:connectedAndroidTest

echo "=== ANDROID NATIVE GATE PASSED ==="
echo "To capture the Go interop fixture:"
echo "  adb shell run-as <app-id> cat files/android_signature_fixture.json > \\"
echo "    $PROJECT_ROOT/companion-daemon/internal/devicetrust/testdata/android_signature_fixture.json"
