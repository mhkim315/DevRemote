/** @type {import('jest').Config} */
module.exports = {
  testEnvironment: 'node',
  testMatch: ['**/__tests__/**/*.test.ts'],
  // Tested modules are pure TS. @noble/* ships ESM, so it must be transformed
  // (not ignored) and compiled to CJS for the jest runtime.
  transform: {
    '^.+\\.(ts|js)$': ['ts-jest', {
      tsconfig: { strict: true, esModuleInterop: true, skipLibCheck: true, allowJs: true },
    }],
  },
  transformIgnorePatterns: ['node_modules/(?!(@noble)/)'],
};
