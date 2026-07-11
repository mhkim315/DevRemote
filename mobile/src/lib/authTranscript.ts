// M3-auth-3A: byte-exact canonical authentication transcript.
//
// Mirror of companion-daemon/internal/devicetrust/auth_transcript.go
// AuthTranscript.Build(). Layout:
//
//   "pokit-device-auth-v1"
//   length(role)      || role        ("device" or "host")
//   length(hostId)    || hostId
//   length(deviceId)  || deviceId
//   length(bootId)    || daemonBootId
//   length(chalId)    || challengeId (32 raw bytes)
//   length(clientNC)  || clientNonce (32 raw bytes)
//   length(serverNC)  || serverNonce (32 raw bytes)
//   length(8)         || createdAtMS (8-byte big-endian uint64 ms)
//   length(8)         || expiresAtMS (8-byte big-endian uint64 ms)
//
// Every field is length-prefixed (uint32-BE). The distinct role field prevents
// a host signature from being replayed as a device signature.

export interface AuthTranscriptParams {
  role: 'device' | 'host';
  hostId: string;
  deviceId: string;
  daemonBootId: string;
  challengeId: Uint8Array;   // exactly 32 bytes
  clientNonce: Uint8Array;   // exactly 32 bytes
  serverNonce: Uint8Array;   // exactly 32 bytes
  createdAtMS: number;       // UTC unix milliseconds (integer)
  expiresAtMS: number;       // UTC unix milliseconds (integer)
}

// putField appends a length-prefixed UTF-8 string field (uint32-BE length).
function putField(b: number[], s: string): void {
  const encoded = new TextEncoder().encode(s);
  putUint32(b, encoded.length);
  for (let i = 0; i < encoded.length; i++) b.push(encoded[i]);
}

// putBytes appends a length-prefixed raw byte field (uint32-BE length).
function putBytes(b: number[], d: Uint8Array): void {
  putUint32(b, d.length);
  for (let i = 0; i < d.length; i++) b.push(d[i]);
}

// putUint64Field appends length(8) then 8-byte big-endian ms timestamp.
function putUint64Field(b: number[], v: number): void {
  b.push(0, 0, 0, 8); // length prefix uint32-BE = 8
  // 8-byte big-endian uint64
  const hi = Math.floor(v / 0x100000000);
  const lo = v >>> 0;
  b.push((hi >>> 24) & 0xff, (hi >>> 16) & 0xff, (hi >>> 8) & 0xff, hi & 0xff);
  b.push((lo >>> 24) & 0xff, (lo >>> 16) & 0xff, (lo >>> 8) & 0xff, lo & 0xff);
}

function putUint32(b: number[], v: number): void {
  b.push((v >>> 24) & 0xff, (v >>> 16) & 0xff, (v >>> 8) & 0xff, v & 0xff);
}

export function buildAuthTranscript(p: AuthTranscriptParams): Uint8Array {
  const b: number[] = [];
  // Domain separator prefix bytes.
  const domain = new TextEncoder().encode('pokit-device-auth-v1');
  for (let i = 0; i < domain.length; i++) b.push(domain[i]);

  putField(b, p.role);
  putField(b, p.hostId);
  putField(b, p.deviceId);
  putField(b, p.daemonBootId);
  putBytes(b, p.challengeId);
  putBytes(b, p.clientNonce);
  putBytes(b, p.serverNonce);
  putUint64Field(b, p.createdAtMS);
  putUint64Field(b, p.expiresAtMS);

  return new Uint8Array(b);
}
