import {
  deriveIdentity, deviceFingerprint, verifyDer,
  toHex, fromHex, toBase64, fromBase64,
} from '../src/lib/crypto';

// Canonical cross-language vector — the SPKI DER and deviceId are deterministic
// and accepted by the Go daemon (mobile_vectors_test.go). Signatures come from
// the Android Keystore (non-deterministic) so fixed-signature assertions are not
// included.
const V = {
  spkiHex:
    '3059301306072a8648ce3d020106082a8648ce3d03010703420004' +
    '515c3d6eb9e396b904d3feca7f54fdcd0cc1e997bf375dca515ad0a6c3b4035f' +
    '4536be3a50f318fbf9a5475902a221502bef0d57e08c53b2cc0a56f17d9f9354',
  fingerprint: 'f1d59449b727165de732bf283338122b99628a615918fedc67d878fffcf47da7',
};

describe('SPKI DER + deviceId (byte-exact with the daemon)', () => {
  it('produces the correct deviceId from a known SPKI DER hex', () => {
    const id = deriveIdentity(V.spkiHex);
    expect(id.deviceId).toBe(V.fingerprint);
    expect(id.spkiDer.length).toBe(91);
  });
});

describe('verifyDer (signature verification, used for host pinning)', () => {
  it('verifies a noble-generated keypair signature', async () => {
    const { p256 } = await import('@noble/curves/nist.js');
    const priv = p256.utils.randomSecretKey();
    const pub = p256.getPublicKey(priv, false);
    const msg = new TextEncoder().encode('pokit-host-pinning-test');
    const sig = p256.sign(msg, priv, { format: 'der', prehash: true });
    expect(verifyDer(sig, msg, pub)).toBe(true);
    expect(verifyDer(sig, new TextEncoder().encode('tampered'), pub)).toBe(false);
  });

  // BUG-001 Layer 3 / BUG-006A: Go ecdsa.SignASN1 produces high-S ~50% of
  // the time. Noble defaults to lowS:true (rejects high-S). After the fix,
  // verifyDer(lowS:false) must accept high-S DER signatures.
  // To deterministically test: noble signs in low-S, then we malleate
  // (r, s) → (r, n - s) to produce a valid high-S counterpart.
  it('accepts malleated high-S DER (Go interop)', async () => {
    const { p256 } = await import('@noble/curves/nist.js');
    const priv = p256.utils.randomSecretKey();
    const pub = p256.getPublicKey(priv, false);
    const msg = new TextEncoder().encode('pokit-pair-v1:high-s-vector-test');

    const sigDER = p256.sign(msg, priv, { format: 'der', prehash: true, lowS: true });
    expect(verifyDer(sigDER, msg, pub)).toBe(true);

    // Parse DER: 0x30<len> 0x02<rLen><r> 0x02<sLen><s>
    const d = sigDER;
    let p = 2; p++; // skip 0x30 + totalLen
    const rLen = d[p]; p++;
    const r = d.slice(p, p + rLen); p += rLen;
    p++; // skip 0x02 tag for S
    const sLen = d[p]; p++;
    const s = d.slice(p, p + sLen);

    // Malleate to high-S: s' = n - s (n = P-256 curve order)
    const P256_N = 0xFFFFFFFF00000000FFFFFFFFFFFFFFFFBCE6FAADA7179E84F3B9CAC2FC632551n;
    let sBig = 0n;
    for (const b of s) sBig = (sBig << 8n) | BigInt(b);
    const sPrimeBig = P256_N - sBig;
    if (sPrimeBig <= 0n) return; // already high-S
    let sHex = sPrimeBig.toString(16);
    if (sHex.length % 2) sHex = '0' + sHex;
    // DER INTEGER requires a leading 0x00 if the high bit is set (positive).
    if ((parseInt(sHex[0], 16) & 0x8) !== 0) sHex = '00' + sHex;
    const sPrime = new Uint8Array(sHex.match(/.{2}/g)!.map(b => parseInt(b, 16)));

    const newTL = 2 + rLen + 2 + sPrime.length;
    const highS = new Uint8Array(2 + newTL);
    highS[0] = 0x30; highS[1] = newTL;
    highS[2] = 0x02; highS[3] = rLen; highS.set(r, 4);
    const rEnd = 4 + rLen;
    highS[rEnd] = 0x02; highS[rEnd + 1] = sPrime.length;
    highS.set(sPrime, rEnd + 2);

    // High-S must verify (lowS:false).
    expect(verifyDer(highS, msg, pub)).toBe(true);
    // Tampered transcript rejected.
    expect(verifyDer(highS, new TextEncoder().encode('tampered'), pub)).toBe(false);
  });

  it('rejects high-S DER with wrong public key', async () => {
    const { p256 } = await import('@noble/curves/nist.js');
    const priv = p256.utils.randomSecretKey();
    const pub = p256.getPublicKey(priv, false);
    const otherPub = p256.getPublicKey(p256.utils.randomSecretKey(), false);
    const msg = new TextEncoder().encode('pokit-pair-v1:wrong-key-test');

    const sigDER = p256.sign(msg, priv, { format: 'der', prehash: true, lowS: true });
    const d = sigDER;
    let p = 2; p++;
    const rLen = d[p]; p++;
    const r = d.slice(p, p + rLen); p += rLen;
    p++; const sLen = d[p]; p++;
    const s = d.slice(p, p + sLen);

    const P256_N = 0xFFFFFFFF00000000FFFFFFFFFFFFFFFFBCE6FAADA7179E84F3B9CAC2FC632551n;
    let sBig = 0n;
    for (const b of s) sBig = (sBig << 8n) | BigInt(b);
    const sPrimeBig = P256_N - sBig;
    if (sPrimeBig <= 0n) return;
    let sHex = sPrimeBig.toString(16);
    if (sHex.length % 2) sHex = '0' + sHex;
    if ((parseInt(sHex[0], 16) & 0x8) !== 0) sHex = '00' + sHex;
    const sPrime = new Uint8Array(sHex.match(/.{2}/g)!.map(b => parseInt(b, 16)));

    const newTL = 2 + rLen + 2 + sPrime.length;
    const highS = new Uint8Array(2 + newTL);
    highS[0] = 0x30; highS[1] = newTL;
    highS[2] = 0x02; highS[3] = rLen; highS.set(r, 4);
    const rEnd = 4 + rLen;
    highS[rEnd] = 0x02; highS[rEnd + 1] = sPrime.length;
    highS.set(sPrime, rEnd + 2);

    expect(verifyDer(highS, msg, pub)).toBe(true);
    expect(verifyDer(highS, msg, otherPub)).toBe(false);
  });
});

describe('strict hex codec', () => {
  it('round-trips', () => {
    expect(toHex(fromHex(V.spkiHex))).toBe(V.spkiHex);
  });
  it('rejects odd-length', () => {
    expect(() => fromHex('abc')).toThrow('even length');
  });
  it('rejects non-hex chars', () => {
    expect(() => fromHex('zzzz')).toThrow('invalid hex char');
  });
  it('accepts expected byte length', () => {
    expect(() => fromHex(V.spkiHex, 91)).not.toThrow();
  });
  it('rejects wrong byte length when expectedLen is set', () => {
    expect(() => fromHex('abcd', 5)).toThrow('expected 5');
  });
});

describe('strict base64 codec', () => {
  it('round-trips standard padded', () => {
    const u = fromHex(V.spkiHex);
    expect(fromBase64(toBase64(u))).toEqual(u);
  });
  it('rejects illegal characters (not silently stripped)', () => {
    expect(() => fromBase64('!!!!')).toThrow('invalid base64 char');
  });
  it('rejects non-multiple-of-4 length', () => {
    expect(() => fromBase64('abc')).toThrow('multiple of 4');
    // "abcd efgh" is 9 chars → length check fires before char check
    expect(() => fromBase64('abcd efgh')).toThrow('multiple of 4');
  });
  it('rejects padding in wrong position', () => {
    // "a=bc" is 4 chars with '=' mid-string → wrong-position pad.
    expect(() => fromBase64('a=bc')).toThrow('base64 pad');
  });
  it('rejects non-canonical re-encode mismatch (trailing bits set)', () => {
    // "AB==" decodes to [0x00] whose canonical form is "AA==".
    expect(() => fromBase64('AB==')).toThrow('not canonical');
  });
  it('accepts URL-safe only if canonical-checked (currently rejected)', () => {
    expect(() => fromBase64('+-+-')).toThrow('invalid base64 char');
  });
});
