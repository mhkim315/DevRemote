// M3-auth-2A: strict QR-payload parser. The QR is untrusted input; every
// field is validated before the first network call. The daemon-built QR
// payload from pair.go (runPairClient) is the canonical source for the v1
// schema. Throws are caught by the public entry point and returned as
// {error} — never thrown to callers.

import { fromHex, toBase64 } from './crypto';

export interface PairingQRPayload {
  sessionId: string;
  hostId: string;
  fingerprint: string;
  hostPubKeyB64: string;
  hostPubKeyDer: Uint8Array; // 91B canonical P-256 SPKI
  bootstrapToken: string;
  endpoint: string;
  expiresAt: Date;
}

const SESSION_ID_MAX = 128;
const HOST_ID_MAX = 128;
const FINGERPRINT_LEN = 64;
const BOOTSTRAP_MAX = 256;
const ENDPOINT_MAX = 256;

const ALLOWED_FIELDS = new Set([
  'sessionId', 'hostId', 'fingerprint', 'hostPubKey', 'bootstrapToken', 'endpoint', 'expiresAt',
]);

interface ParseError { error: string }

function err(msg: string): never { throw { error: msg } as ParseError; }

export function parsePairingQR(raw: unknown): PairingQRPayload | { error: string } {
  try { return _parse(raw); } catch (e) {
    const pe = e as ParseError | undefined;
    return { error: pe && typeof pe.error === 'string' ? pe.error : 'QR payload is not valid' };
  }
}

function _parse(raw: unknown): PairingQRPayload {
  if (typeof raw !== 'string') err('QR payload must be a JSON string');
  let obj: unknown;
  try { obj = JSON.parse(raw); } catch { err('QR payload is not valid JSON'); }
  if (typeof obj !== 'object' || obj === null || Array.isArray(obj)) err('QR payload must be a JSON object');
  const o = obj as Record<string, unknown>;

  for (const k of Object.keys(o)) {
    if (!ALLOWED_FIELDS.has(k)) err(`QR payload contains unknown field: ${k}`);
    if (typeof o[k] !== 'string') err(`QR field ${k} must be a string`);
  }

  const sessionId = strField(o, 'sessionId', 1, SESSION_ID_MAX);
  const hostId = strField(o, 'hostId', 1, HOST_ID_MAX);
  const fingerprint = strField(o, 'fingerprint', FINGERPRINT_LEN, FINGERPRINT_LEN);
  if (!/^[0-9a-f]{64}$/.test(fingerprint)) err('host fingerprint must be 64-char lowercase hex');

  const hostPubKeyHex = o.hostPubKey as string;
  if (!/^[0-9a-fA-F]{182}$/.test(hostPubKeyHex)) err('hostPubKey must be 182-char hex (91-byte P-256 SPKI)');
  let hostPubKeyDer: Uint8Array;
  try { hostPubKeyDer = fromHex(hostPubKeyHex, 91); } catch { err('host public key is not valid canonical hex SPKI'); }
  // Re-encode to base64 for storage (wire is hex, persisted is b64 for pairingStore SPKI checks).
  const hostPubKeyB64 = toBase64(hostPubKeyDer);

  const bootstrapToken = strField(o, 'bootstrapToken', 1, BOOTSTRAP_MAX);
  const endpoint = strField(o, 'endpoint', 1, ENDPOINT_MAX);

  let parsed: URL;
  try { parsed = new URL(endpoint); } catch { err('endpoint is not a valid URL'); }
  if (parsed.protocol !== 'http:') err(`endpoint scheme must be http, got ${parsed.protocol}`);
  if (parsed.username || parsed.password) err('endpoint must not contain credentials');
  if (parsed.hash) err('endpoint must not contain a fragment');
  if (parsed.search) err('endpoint must not contain a query string');
  if (!/^\d+\.\d+\.\d+\.\d+$/.test(parsed.hostname) || !isPrivateIPv4(parsed.hostname)) {
    err('endpoint must be a private-IPv4 LAN address with an explicit port');
  }
  if (!parsed.port) err('endpoint must include an explicit port');

  const expiresAt = new Date(o.expiresAt as string);
  if (isNaN(expiresAt.getTime())) err('expiresAt is not a valid ISO date');
  if (expiresAt <= new Date()) err('QR payload has expired');

  return { sessionId, hostId, fingerprint, hostPubKeyB64, hostPubKeyDer, bootstrapToken, endpoint, expiresAt };
}

function strField(o: Record<string, unknown>, key: string, min: number, max: number): string {
  const v = o[key];
  if (typeof v !== 'string') err(`missing field: ${key}`);
  if (v.length < min || v.length > max) err(`${key} must be ${min}-${max} characters`);
  for (let i = 0; i < v.length; i++) {
    const c = v.charCodeAt(i);
    if (c < 0x20 || c === 0x7f) err(`${key} contains control characters`);
  }
  return v;
}

function isPrivateIPv4(host: string): boolean {
  const parts = host.split('.').map(Number);
  if (parts.length !== 4 || parts.some(p => isNaN(p) || p < 0 || p > 255)) return false;
  if (parts[0] === 10) return true;
  if (parts[0] === 172 && parts[1] >= 16 && parts[1] <= 31) return true;
  if (parts[0] === 192 && parts[1] === 168) return true;
  return false;
}
