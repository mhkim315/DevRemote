// Minimal stub for jest — expo-modules-core has "main": "src/index.ts" which
// ts-jest can't process. This provides the one export the test needs.
exports.requireOptionalNativeModule = function () { return null; };
exports.requireNativeModule = function () { throw new Error('no native module in test'); };
