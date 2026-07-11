import { parsePairingQR } from '../src/lib/qrParser';
import { toBase64 } from '../src/lib/crypto';

const CANONICAL_HOST_SPKI = Uint8Array.from([
  0x30, 0x59, 0x30, 0x13, 0x06, 0x07, 0x2a, 0x86, 0x48, 0xce, 0x3d, 0x02, 0x01,
  0x06, 0x08, 0x2a, 0x86, 0x48, 0xce, 0x3d, 0x03, 0x01, 0x07, 0x03, 0x42, 0x00,
  ...Array.from({ length: 65 }, (_, i) => i + 1),
]);
const HOST_PUB_B64 = toBase64(CANONICAL_HOST_SPKI);
const FUTURE = new Date(Date.now() + 300_000).toISOString();

function validPayload(overrides: Record<string, unknown> = {}): string {
  return JSON.stringify({
    sessionId: 'sess-abc',
    hostId: 'host-001',
    fingerprint: '0000111122223333444455556666777788889999aaaabbbbccccddddeeeeffff',
    hostPubKey: HOST_PUB_B64,
    bootstrapToken: 'tok-123',
    endpoint: 'http://192.168.1.10:8765',
    expiresAt: FUTURE,
    ...overrides,
  });
}

describe('qrParser — reject', () => {
  it('rejects non-JSON string', () => {
    expect(parsePairingQR('not json')).toMatchObject({ error: expect.stringContaining('JSON') });
  });
  it('rejects null', () => {
    expect(parsePairingQR(null)).toMatchObject({ error: expect.stringContaining('JSON string') });
  });
  it('rejects missing field', () => {
    expect(parsePairingQR(validPayload({ sessionId: undefined }))).toMatchObject({ error: expect.stringContaining('missing') });
  });
  it('rejects unknown field', () => {
    expect(parsePairingQR(validPayload({ secret: 'x' }))).toMatchObject({ error: expect.stringContaining('unknown') });
  });
  it('rejects control char in id', () => {
    expect(parsePairingQR(validPayload({ hostId: 'host\nid' }))).toMatchObject({ error: expect.stringContaining('control') });
  });
  it('rejects bad hex fingerprint', () => {
    expect(parsePairingQR(validPayload({ fingerprint: 'gggg11112222...'.padEnd(64, '0') }))).toMatchObject({ error: expect.stringContaining('hex') });
  });
  it('rejects wrong fingerprint length', () => {
    expect(parsePairingQR(validPayload({ fingerprint: 'abcd' }))).toMatchObject({ error: expect.stringContaining('64') });
  });
  it('rejects bad base64 host key', () => {
    expect(parsePairingQR(validPayload({ hostPubKey: '!!!!' }))).toMatchObject({ error: expect.stringContaining('base64') });
  });
  it('rejects wrong-length SPKI', () => {
    expect(parsePairingQR(validPayload({ hostPubKey: toBase64(new Uint8Array(90)) }))).toMatchObject({ error: expect.stringContaining('91') });
  });
  it('rejects unsafe endpoint scheme (https)', () => {
    expect(parsePairingQR(validPayload({ endpoint: 'https://192.168.1.10:8765' }))).toMatchObject({ error: expect.stringContaining('http') });
  });
  it('rejects endpoint with userinfo', () => {
    expect(parsePairingQR(validPayload({ endpoint: 'http://user:pass@192.168.1.10:8765' }))).toMatchObject({ error: expect.stringContaining('credential') });
  });
  it('rejects public-IP endpoint', () => {
    expect(parsePairingQR(validPayload({ endpoint: 'http://8.8.8.8:8765' }))).toMatchObject({ error: expect.stringContaining('private') });
  });
  it('rejects expired QR', () => {
    expect(parsePairingQR(validPayload({ expiresAt: '2020-01-01T00:00:00Z' }))).toMatchObject({ error: expect.stringContaining('expired') });
  });
  it('rejects oversized field', () => {
    expect(parsePairingQR(validPayload({ hostId: 'x'.repeat(200) }))).toMatchObject({ error: expect.stringContaining('128') });
  });
  it('accepts a valid payload', () => {
    const r = parsePairingQR(validPayload());
    expect('error' in r).toBe(false);
  });
});
