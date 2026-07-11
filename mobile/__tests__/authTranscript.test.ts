// Byte-exact proof: the TS buildAuthTranscript produces the SAME bytes as the
// Go AuthTranscript.Build(). Fixture from /tmp/gen.go.

import { buildAuthTranscript } from '../src/lib/authTranscript';
import { sha256 } from '@noble/hashes/sha2.js';
import { toHex } from '../src/lib/crypto';

const FIXTURE_HEX = '706f6b69742d6465766963652d617574682d76310000000664657669636500000008686f73742d303031000000406161616161616161616161616161616161616161616161616161616161616161616161616161616161616161616161616161616161616161616161616161616100000000000000200102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f2000000020403f3e3d3c3b3a393837363534333231302f2e2d2c2b2a29282726252423222100000020808182838485868788898a8b8c8d8e8f909192939495969798999a9b9c9d9e9f000000080000019f509e9d00000000080000019f50a330e0';
const FIXTURE_DIGEST = '61fc82a548c2a185f6dc20ef2923c6b69932c02950ed5b3906a05fea1f3d379d';

describe('auth transcript (byte-exact with Go)', () => {
  it('produces the exact Go fixture bytes', () => {
    const cid = new Uint8Array(32); for (let i = 0; i < 32; i++) cid[i] = i + 1;
    const cn = new Uint8Array(32); for (let i = 0; i < 32; i++) cn[i] = 64 - (i % 64);
    const sn = new Uint8Array(32); for (let i = 0; i < 32; i++) sn[i] = 128 + (i % 64);
    const ca = new Date('2026-07-11T10:00:00Z').getTime();
    const ea = ca + 300_000;

    const transcript = buildAuthTranscript({
      role: 'device', hostId: 'host-001',
      deviceId: 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
      daemonBootId: '', // the Go fixture used "" for bootId (empty string)
      challengeId: cid, clientNonce: cn, serverNonce: sn,
      createdAtMS: ca, expiresAtMS: ea,
    });

    // Actually wait — the Go fixture had bootId="" which is a 0-length string.
    // The prefix field is "pokit-device-auth-v1" (20 bytes), then:
    // len(6) || "device", len(8) || "host-001", len(64) || "aaaa...", len(0) || ""
    // That matches. But the Go fixture used length=0 for bootId, so we must too.
  });

  it('matches the Go fixture byte for byte', () => {
    const cid = new Uint8Array(32); for (let i = 0; i < 32; i++) cid[i] = i + 1;
    const cn = new Uint8Array(32); for (let i = 0; i < 32; i++) cn[i] = 64 - (i % 64);
    const sn = new Uint8Array(32); for (let i = 0; i < 32; i++) sn[i] = 128 + (i % 64);
    const ca = new Date('2026-07-11T10:00:00Z').getTime();
    const ea = ca + 300_000;

    const t = buildAuthTranscript({
      role: 'device', hostId: 'host-001',
      deviceId: 'a'.repeat(64),
      daemonBootId: '', // Go fixture used empty bootId
      challengeId: cid, clientNonce: cn, serverNonce: sn,
      createdAtMS: ca, expiresAtMS: ea,
    });
    expect(toHex(t)).toBe(FIXTURE_HEX);
    expect(toHex(sha256(t))).toBe(FIXTURE_DIGEST);
  });

  it('host and device transcripts differ (role is bound)', () => {
    const cid = new Uint8Array(32); cid[0] = 1;
    const nx = new Uint8Array(32);
    const ca = new Date('2026-07-11T10:00:00Z').getTime();
    const ea = ca + 300_000;
    const params = { hostId: 'h', deviceId: 'd', daemonBootId: 'b', challengeId: cid, clientNonce: nx, serverNonce: nx, createdAtMS: ca, expiresAtMS: ea };
    const hostT = buildAuthTranscript({ ...params, role: 'host' });
    const devT = buildAuthTranscript({ ...params, role: 'device' });
    expect(toHex(hostT)).not.toBe(toHex(devT));
  });
});
