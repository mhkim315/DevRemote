import { savePairing, loadPairing, clearPairing } from '../src/lib/pairingStore';
import { toBase64 } from '../src/lib/crypto';

jest.mock('@react-native-async-storage/async-storage', () => {
  let store = new Map<string, string>();
  return {
    getItem: async (k: string) => store.get(k) ?? null,
    setItem: async (k: string, v: string) => { store.set(k, v); },
    removeItem: async (k: string) => { store.delete(k); },
    __reset: () => { store = new Map(); },
  };
}, { virtual: true });

const CANONICAL_SPKI = new Uint8Array(91).fill(0x01);
// Fix first two bytes to canonical P-256 SEQUENCE header (len-89, 30 59).
CANONICAL_SPKI[0] = 0x30; CANONICAL_SPKI[1] = 0x59;

const HOST_KEY = toBase64(CANONICAL_SPKI);

describe('pairingStore', () => {
  const AsyncStorage = require('@react-native-async-storage/async-storage');

  beforeEach(() => {
    AsyncStorage.__reset();
    if (AsyncStorage.default) AsyncStorage.default.__reset();
  });

  it('save + load round-trip', async () => {
    await savePairing({ hostId: 'h', hostPubKeyB64: HOST_KEY, deviceId: 'd', baseURL: 'http://x', pairedAt: '2026', role: 'owner' });
    const p = await loadPairing();
    expect(p).not.toBeNull();
    expect(p!.hostId).toBe('h');
  });
  it('load returns null when no pairing', async () => {
    expect(await loadPairing()).toBeNull();
  });
  it('corrupted stored SPKI → deleted', async () => {
    await AsyncStorage.setItem('pokit.pairing', JSON.stringify({ hostId: 'h', hostPubKeyB64: 'notbase64!!', deviceId: 'd', baseURL: 'x', pairedAt: '2026', role: 'owner' }));
    expect(await loadPairing()).toBeNull();
  });
  it('clear removes pairing', async () => {
    await savePairing({ hostId: 'h', hostPubKeyB64: HOST_KEY, deviceId: 'd', baseURL: 'http://x', pairedAt: '2026', role: 'owner' });
    await clearPairing();
    expect(await loadPairing()).toBeNull();
  });
});
