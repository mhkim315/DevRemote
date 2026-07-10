/** @type {import('jest').Config} */
module.exports = {
  preset: 'ts-jest',
  testEnvironment: 'node',
  testMatch: ['**/__tests__/**/*.test.ts'],
  // The tested modules (src/lib/client.ts, src/lib/lifecycle.ts) are pure TS
  // with no React Native imports, so no RN/Expo jest preset is needed.
  transform: {
    '^.+\\.ts$': ['ts-jest', { tsconfig: { strict: true, esModuleInterop: true, skipLibCheck: true } }],
  },
};
