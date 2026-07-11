// Default jest mock for expo-secure-store (stateless — no legacy key present).
// Tests that need stateful behavior (deviceKey.test.ts legacy migration) override
// this with an in-file jest.mock.
module.exports = {
  getItemAsync: async () => null,
  setItemAsync: async () => {},
  deleteItemAsync: async () => {},
  WHEN_UNLOCKED_THIS_DEVICE_ONLY: 'when_unlocked_this_device_only',
};
