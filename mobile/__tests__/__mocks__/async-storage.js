// DS-STAB5: global mock for @react-native-async-storage/async-storage.
// Jest module caching causes per-file jest.mock() calls to be order-dependent.
// A moduleNameMapper entry pinned here makes the mock deterministic regardless
// of suite ordering.
let store = new Map();

module.exports = {
  default: {
    getItem: async (k) => store.get(k) ?? null,
    setItem: async (k, v) => { store.set(k, v); },
    removeItem: async (k) => { store.delete(k); },
    mergeItem: async (k, v) => { store.set(k, v); },
    clear: async () => { store = new Map(); },
    getAllKeys: async () => Array.from(store.keys()),
    multiGet: async (keys) => keys.map((k) => [k, store.get(k) ?? null]),
    multiSet: async (pairs) => { pairs.forEach(([k, v]) => store.set(k, v)); },
    multiRemove: async (keys) => { keys.forEach((k) => store.delete(k)); },
    multiMerge: async (pairs) => { pairs.forEach(([k, v]) => store.set(k, v)); },
    flushGetRequests: async () => {},
    useSQLiteBackend: false,
    __reset: () => { store = new Map(); },
    __dump: () => new Map(store),
  },
  getItem: async (k) => store.get(k) ?? null,
  setItem: async (k, v) => { store.set(k, v); },
  removeItem: async (k) => { store.delete(k); },
  mergeItem: async (k, v) => { store.set(k, v); },
  clear: async () => { store = new Map(); },
  getAllKeys: async () => Array.from(store.keys()),
  multiGet: async (keys) => keys.map((k) => [k, store.get(k) ?? null]),
  multiSet: async (pairs) => { pairs.forEach(([k, v]) => store.set(k, v)); },
  multiRemove: async (keys) => { keys.forEach((k) => store.delete(k)); },
  multiMerge: async (pairs) => { pairs.forEach(([k, v]) => store.set(k, v)); },
  flushGetRequests: async () => {},
  useSQLiteBackend: false,
  __reset: () => { store = new Map(); },
  __dump: () => new Map(store),
};
