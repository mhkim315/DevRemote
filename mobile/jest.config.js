/** @type {import('jest').Config} */
module.exports = {
  testEnvironment: 'node',
  testMatch: ['**/__tests__/**/*.test.ts'],
  transform: {
    '^.+\\.(ts|tsx|js)$': ['ts-jest', {
      tsconfig: { strict: true, esModuleInterop: true, skipLibCheck: true, allowJs: true },
    }],
  },
  transformIgnorePatterns: ['node_modules/(?!(@noble)/)'],
  // expo-modules-core has "main": "src/index.ts" — a TS entry that jest can't
  // process correctly. Redirect to a plain JS stub (the mock is in the test file).
  moduleNameMapper: {
    '^expo-modules-core$': '<rootDir>/__tests__/__mocks__/expo-modules-core.js',
    '^react-native$': '<rootDir>/__tests__/__mocks__/react-native.js',
    '^expo-crypto$': '<rootDir>/__tests__/__mocks__/expo-crypto.js',
    // deviceIdentity (reached transitively via pairingClient) imports SecureStore.
    // A stateless default; deviceKey.test.ts overrides with a stateful jest.mock.
    '^expo-secure-store$': '<rootDir>/__tests__/__mocks__/expo-secure-store.js',
  },
};
