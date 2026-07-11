import { buildPairingTranscript } from '../src/lib/pairingClient';

describe('pairing transcript (byte-exact with daemon)', () => {
  it('matches the known Go buildPairingTranscript layout', () => {
    const pn = new Uint8Array(32); pn[0] = 0x01;
    const hn = new Uint8Array(32); hn[0] = 0x02;
    const pubDER = new Uint8Array(91); pubDER[0] = 0x30; // faking SPKI prefix bytes
    const sid = 'test-session';
    const t = buildPairingTranscript(pn, hn, pubDER, sid);
    const prefix = new TextEncoder().encode('pokit-pair-v1:');
    // First len(prefix) bytes == the prefix.
    for (let i = 0; i < prefix.length; i++) expect(t[i]).toBe(prefix[i]);
    // Then phoneNonce[0]=1, then hostNonce[0]=2.
    expect(t[prefix.length]).toBe(0x01); // phoneNonce
    expect(t[prefix.length + 32]).toBe(0x02); // hostNonce
    expect(t[prefix.length + 64]).toBe(0x30); // hostPubDER[0]
    // Session ID at the end.
    expect(t.length).toBe(prefix.length + 32 + 32 + 91 + sid.length);
    const tail = t.subarray(t.length - sid.length);
    expect(new TextDecoder().decode(tail)).toBe(sid);
  });
});
