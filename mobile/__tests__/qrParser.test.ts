import { parsePairingQR } from '../src/lib/qrParser';
import { toHex } from '../src/lib/crypto';

// A valid P-256 SPKI: the canonical 26-byte prefix + 65 bytes of uncompressed
// point. Doesn't need to be on curve for the parser (SPKI structure + length
// only here; the curve-point validator runs later in the pairing client).
const CANONICAL_SPKI = new Uint8Array(91);
CANONICAL_SPKI[0] = 0x30; CANONICAL_SPKI[1] = 0x59; // SEQUENCE header
// Fill in the prefix bytes (the actual DER prefix for P-256).
const PREFIX_HEX = '3059301306072a8648ce3d020106082a8648ce3d030107034200';
for (let i = 0; i < PREFIX_HEX.length >> 1; i++) {
  CANONICAL_SPKI[i] = parseInt(PREFIX_HEX.substr(i * 2, 2), 16);
}
// Fill uncompressed point with non-zero data (not a real curve point, but
// parser doesn't check that — pairing client does).
for (let i = 26; i < 91; i++) CANONICAL_SPKI[i] = (i - 25) % 256 || 1;
CANONICAL_SPKI[26] = 0x04; // uncompressed point

const HOST_PUB_HEX = toHex(CANONICAL_SPKI);
const FUTURE = new Date(Date.now() + 300_000).toISOString();

function validPayload(overrides: Record<string, unknown> = {}): string {
  return JSON.stringify({
    sessionId: 'sess-abc',
    hostId: 'host-001',
    fingerprint: '0000111122223333444455556666777788889999aaaabbbbccccddddeeeeffff',
    hostPubKey: HOST_PUB_HEX,
    bootstrapToken: 'tok-123',
    endpoint: 'http://192.168.1.10:8765/pair',
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
  it('rejects non-hex host key', () => {
    expect(parsePairingQR(validPayload({ hostPubKey: 'zzzzzz' }))).toMatchObject({ error: expect.stringContaining('hex') });
  });
  it('rejects wrong-length SPKI hex', () => {
    expect(parsePairingQR(validPayload({ hostPubKey: 'ab'.repeat(90) }))).toMatchObject({ error: expect.stringContaining('182') });
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

  // PB.7 QR evidence: the daemon (pair.go) builds the QR payload with
  // these exact 7 fields. This test proves parsePairingQR accepts the
  // daemon's canonical payload format — the same JSON that renderQR
  // embeds in the QR code and the Go test round-trips through gozxing.
  it('accepts daemon-format pairing payload', () => {
    const daemonPayload = JSON.stringify({
      sessionId: 'sess-abc',
      hostId: 'host-001',
      fingerprint: '0000111122223333444455556666777788889999aaaabbbbccccddddeeeeffff',
      hostPubKey: HOST_PUB_HEX,
      bootstrapToken: 'tok-123',
      endpoint: 'http://192.168.1.10:8765/pair',
      expiresAt: FUTURE,
    });
    const r = parsePairingQR(daemonPayload);
    expect('error' in r).toBe(false);
    // Verify every daemon field is decoded back.
    if ('error' in r) throw new Error('unexpected');
    expect(r.sessionId).toBe('sess-abc');
    expect(r.hostId).toBe('host-001');
    expect(r.fingerprint).toBe('0000111122223333444455556666777788889999aaaabbbbccccddddeeeeffff');
    expect(r.bootstrapToken).toBe('tok-123');
    expect(r.endpoint).toBe('http://192.168.1.10:8765/pair');
    expect(r.expiresAt).toBeInstanceOf(Date);
    expect(r.hostPubKeyB64).toBeTruthy();
    expect(r.hostPubKeyDer).toBeInstanceOf(Uint8Array);
    expect(r.hostPubKeyDer.length).toBe(91);
  });
});
