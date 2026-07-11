#!/bin/sh
# M3-auth-1B MANDATORY native gate — runs the real iOS Secure Enclave
# XCTest against a connected device.
#
# This is SEPARATE from scripts/build-gate.sh (which does not compile or run any
# iOS Swift). A connected iOS device is required for a meaningful run: the Secure
# Enclave is unavailable on the Simulator, so the enclave-backed sign/verify
# tests skip there and M3-auth-1B is NOT complete on a Simulator-only run.
#
# Usage:
#   sh scripts/ios-native-gate.sh
#
# Steps:
#   1. clean Expo prebuild (reproducible generated iOS project)
#   2. pod install
#   3. build + run the PokitDeviceKey XCTest test_spec on the connected device
set -e

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
MOBILE_DIR="$PROJECT_ROOT/mobile"

echo "=== M3-auth-1B iOS native gate ==="

if ! command -v xcodebuild >/dev/null 2>&1; then
  echo "FAILED: xcodebuild not found — Xcode and a connected iOS device are required." >&2
  exit 1
fi

# A booted Simulator can build/run but has NO Secure Enclave; the functional
# tests will skip. Warn loudly so a Simulator run is not mistaken for a pass.
DEVICE_ID="$(xcrun xctrace list devices 2>/dev/null | awk '/\(([0-9]+\.)+[0-9]+\) \(/{print}' | grep -vi simulator | head -1 || true)"
if [ -z "$DEVICE_ID" ]; then
  echo "WARNING: no physical iOS device detected. Secure Enclave tests will SKIP;" >&2
  echo "         this run cannot be claimed as the M3-auth-1B hardware gate." >&2
fi

cd "$MOBILE_DIR"
echo "--- clean prebuild (ios) ---"
npx expo prebuild --clean --platform ios --no-install

cd "$MOBILE_DIR/ios"
echo "--- pod install ---"
pod install

echo "--- xcodebuild test (PokitDeviceKey Tests) ---"
# Runs the podspec test_spec. On a device the enclave tests execute; on a
# Simulator they XCTSkip. Adjust -scheme/-destination to the generated project.
xcodebuild test \
  -workspace POKIT.xcworkspace \
  -scheme POKIT \
  -destination 'generic/platform=iOS' \
  -only-testing:PokitDeviceKey-Unit-Tests

echo "=== iOS NATIVE GATE COMPLETE ==="
echo "To capture the Go interop fixture, copy the app container's"
echo "  Documents/ios_signature_fixture.json into"
echo "  $PROJECT_ROOT/companion-daemon/internal/devicetrust/testdata/ios_signature_fixture.json"
