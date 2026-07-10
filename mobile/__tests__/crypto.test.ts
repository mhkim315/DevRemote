import {
  spkiDer, publicKeyUncompressed, deviceFingerprint, signDer, verifyDer,
  toHex, fromHex, toBase64, fromBase64, generatePrivateKey, deriveIdentity,
} from '../src/lib/crypto';

// Canonical cross-language vector — the SAME bytes are asserted by the Go test
// companion-daemon/internal/devicetrust/mobile_vectors_test.go. Any drift here
// breaks daemon compatibility.
const V = {
  privHex: '0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20',
  spkiHex:
    '3059301306072a8648ce3d020106082a8648ce3d03010703420004' +
    '515c3d6eb9e396b904d3feca7f54fdcd0cc1e997bf375dca515ad0a6c3b4035f' +
    '4536be3a50f318fbf9a5475902a221502bef0d57e08c53b2cc0a56f17d9f9354',
  fingerprint: 'f1d59449b727165de732bf283338122b99628a615918fedc67d878fffcf47da7',
  message: 'pokit-mobile-vector-v1',
  sigDerHex:
    '3044022001a7c80dc4afea65966c4e138bf1038a9bf21ee38ffdc09604533a12f6ecaf78' +
    '022035097449d58db7da4d8c95a8e86424273820699df463920dd60e9a7361c74bdd',
};

describe('P-256 SPKI DER + fingerprint (byte-exact with the daemon)', () => {
  it('produces the exact SPKI DER and deviceId for a fixed key', () => {
    const priv = fromHex(V.privHex);
    const pub = publicKeyUncompressed(priv);
    expect(pub.length).toBe(65);
    expect(pub[0]).toBe(0x04);
    const spki = spkiDer(pub);
    expect(spki.length).toBe(91);
    expect(toHex(spki)).toBe(V.spkiHex);
    expect(deviceFingerprint(spki)).toBe(V.fingerprint);
  });

  it('deriveIdentity matches the vector', () => {
    const id = deriveIdentity(fromHex(V.privHex));
    expect(toHex(id.spkiDer)).toBe(V.spkiHex);
    expect(id.deviceId).toBe(V.fingerprint);
  });
});

describe('ASN.1 DER signatures', () => {
  it('signs and verifies over sha256(message)', () => {
    const priv = fromHex(V.privHex);
    const pub = publicKeyUncompressed(priv);
    const msg = new TextEncoder().encode(V.message);
    const sig = signDer(priv, msg);
    expect(sig[0]).toBe(0x30); // DER SEQUENCE
    // Deterministic (RFC 6979) and byte-identical to the Go vector.
    expect(toHex(sig)).toBe(V.sigDerHex);
    expect(verifyDer(sig, msg, pub)).toBe(true);
    // wrong message must not verify
    expect(verifyDer(sig, new TextEncoder().encode('tampered'), pub)).toBe(false);
  });

  it('round-trips a freshly generated key', () => {
    const priv = generatePrivateKey();
    const pub = publicKeyUncompressed(priv);
    const msg = new TextEncoder().encode('pokit-device-auth-v1 sample');
    expect(verifyDer(signDer(priv, msg), msg, pub)).toBe(true);
  });
});

describe('pure codecs (Hermes/Node identical)', () => {
  it('hex round-trips', () => {
    const u = fromHex(V.spkiHex);
    expect(toHex(u)).toBe(V.spkiHex);
  });
  it('base64 is standard (padded) and round-trips', () => {
    const u = fromHex(V.spkiHex);
    // matches Node Buffer base64 (== Go base64.StdEncoding used by JSON []byte)
    const expected = Buffer.from(u).toString('base64');
    expect(toBase64(u)).toBe(expected);
    expect(toHex(fromBase64(toBase64(u)))).toBe(V.spkiHex);
  });
  it('base64 handles 1- and 2-byte remainders', () => {
    expect(toBase64(Uint8Array.from([1]))).toBe(Buffer.from([1]).toString('base64'));
    expect(toBase64(Uint8Array.from([1, 2]))).toBe(Buffer.from([1, 2]).toString('base64'));
  });
});
