/** @type {import('jest').Config} */
module.exports = {
  testEnvironment: 'node',
  testMatch: ['**/__tests__/**/*.test.ts'],
  transform: {
    '^.+\\.(ts|js)$': ['ts-jest', {
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
  },
};
