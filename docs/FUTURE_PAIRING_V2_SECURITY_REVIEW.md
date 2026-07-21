# Future Pairing V2 Security Review and Plan

**Verdict:** ACCEPT WITH CHANGES

**Status:** FUTURE POST-DEVICE-GATE PACKET ONLY — not authorized for implementation

**Review baseline:** `e302a7c825d87ab3c92f9bc82fa4395214e644ec`

This review does not alter the frozen QR renderer/security packet, PB evidence,
or the SM-S926N device-gate sequence. Pairing V2 may begin only after the
current device gate and final independent PB acceptance.

## 1. Decision

The full host fingerprint can be removed from the QR without materially
weakening the intended LAN pairing model if, and only if, the QR carries a
uniformly random 128-bit one-time secret and Pairing V2 uses a reviewed
PSK-authenticated ephemeral AKE, proves possession of the existing host and
phone P-256 signing keys, binds the complete application transcript, performs
local SAS confirmation and explicit key confirmation, and commits registry
state through the crash-consistent protocol below.

The selected protocol target is
`Noise_XXpsk0_25519_ChaChaPoly_SHA256`. The OOB secret is mixed from the
beginning of the handshake; both Noise static/ephemeral contributions and the
handshake hash are retained. The existing hardware-backed P-256 device identity
and host P-256 identity are not replaced by Noise keys. Each side must prove
possession of its P-256 key with a role-separated signature over the finalized
Noise handshake hash plus the canonical Pairing V2 application transcript.

This selection is conditional. Implementation is blocked until a dependency
spike proves maintained, test-vector-compatible Go and React Native
implementations of the exact PSK pattern, framing, and cipher suite, including
cross-language vectors on Hermes and physical Android/iOS. The current tree has
no Noise dependency; Go has P-256 host identity, and the mobile hardware modules
expose only P-256 signing. If the spike cannot satisfy those conditions, stop
and re-review another named AKE. Do not implement Noise primitives or the AKE
state machine locally.

Primary construction references are the
[Noise Protocol Framework](https://noiseprotocol.org/noise.html), its
[revision 34 specification](https://noiseprotocol.org/noise_rev34.pdf), and the
[TLS 1.3 external-PSK comparison](https://www.rfc-editor.org/rfc/rfc8446.html).
TLS 1.3 external PSK plus (EC)DHE is cryptographically viable, but is not the
selected target because the current Go and React Native stacks do not expose a
shared, proven external-PSK integration. A PAKE is unnecessary for a uniform
128-bit QR secret and would add complexity without addressing QR theft.

## 2. QR-only envelope

Use a canonical binary envelope followed by unpadded Base64URL and the fixed
ASCII prefix `pokit:v2:`. No alternate JSON, Base45, field ordering, optional
padding, or content-sniffed representation is accepted.

Recommended fields:

| Field | Encoding |
|---|---|
| magic | 2 fixed bytes |
| protocol version | 1 byte |
| suite | 1 fixed-value byte |
| flags | 1 byte; unknown bits reject |
| locator type | 1 byte |
| locator length | 1 byte |
| canonical locator | type-specific bounded bytes |
| invitation ID | 16 uniform random bytes |
| OOB secret | 16 uniform random bytes |

The 128-bit host-key commitment is **not included**. It is not an independent
factor because it travels beside the bearer secret; it does not stop a stolen
QR, a fully replaced QR, or a relay to the legitimate host. It adds 16 bytes and
pushes all measured candidates beyond an 80×24 terminal at QR error correction
M. A full fingerprint adds 32 bytes and is likewise unnecessary under the
accepted AKE and P-256 possession proof.

The commitment decision must be reopened if the final AKE fails to authenticate
the host P-256 key, if a new threat requires pre-network host-key binding
independent of the OOB secret, or if the QR size goal changes. It must not be
credited as protection against QR disclosure or replacement.

The QR contains no expiry. The host owns expiry and pairing generation and
delivers them inside the authenticated encrypted transcript. The QR remains a
bearer invitation until expiry; screenshots and camera access therefore remain
security-sensitive.

## 3. Exact QR size analysis

The table fixes the following inputs so results are reproducible:

- fixed binary overhead: 39 bytes before locator/commitment;
- IPv4 locator: 4-byte address plus 2-byte port = 6 bytes;
- short hostname locator: exactly 12 ASCII hostname bytes plus 2-byte port =
  14 bytes;
- discovery locator: exactly 16 opaque bytes;
- unpadded Base64URL encoded in QR byte mode;
- URI overhead: 9 ASCII bytes for `pokit:v2:`;
- QR versions measured with the repository's `rsc.io/qr` encoder;
- four-module quiet zone on every side;
- terminal dimensions are one column per module and two module rows per
  half-block row.

| Variant | Locator | Raw bytes | Base64URL chars | Total QR chars | M version/modules | Q version/modules | Terminal at M (cols×rows) | 80×24 with one instruction row |
|---|---|---:|---:|---:|---|---|---|---|
| 128-bit secret, no commitment | IPv4 | 45 | 60 | 69 | V5 / 37 | V6 / 41 | 45×23 | YES at M; NO at Q |
| 128-bit secret, no commitment | 12-byte hostname | 53 | 71 | 80 | V5 / 37 | V7 / 45 | 45×23 | YES at M; NO at Q |
| 128-bit secret, no commitment | 16-byte discovery | 55 | 74 | 83 | V5 / 37 | V7 / 45 | 45×23 | YES at M; NO at Q |
| plus 128-bit commitment | IPv4 | 61 | 82 | 91 | V6 / 41 | V8 / 49 | 49×25 | NO |
| plus 128-bit commitment | 12-byte hostname | 69 | 92 | 101 | V6 / 41 | V8 / 49 | 49×25 | NO |
| plus 128-bit commitment | 16-byte discovery | 71 | 95 | 104 | V6 / 41 | V8 / 49 | 49×25 | NO |
| plus full 256-bit fingerprint | IPv4 | 77 | 103 | 112 | V7 / 45 | V9 / 53 | 53×27 | NO |
| plus full 256-bit fingerprint | 12-byte hostname | 85 | 114 | 123 | V8 / 49 | V9 / 53 | 57×29 | NO |
| plus full 256-bit fingerprint | 16-byte discovery | 87 | 116 | 125 | V8 / 49 | V9 / 53 | 57×29 | NO |

Quiet-zone dimensions are module width + 8 and
`ceil((module height + 8) / 2)`. Q-level results are respectively 49×25 for V6,
53×27 for V7, 57×29 for V8, and 61×31 for V9, so none fits 80×24. If the final
wire format, prefix, locator length, encoder, or error-correction level differs,
this table is invalid and must be regenerated before protocol freeze.

## 4. Channel and transcript contract

Use a canonical length-prefixed binary encoding, never JSON serialization or
delimiter concatenation, for application transcript `T`. Bind exactly:

1. `"POKIT-PAIRING-V2"` protocol domain;
2. protocol version and the complete fixed Noise protocol name;
3. the exact canonical QR envelope bytes, including normalized locator bytes;
4. invitation ID;
5. server pairing generation and authenticated expiry instant;
6. server-generated connection identity;
7. host P-256 long-term SPKI DER and derived host ID;
8. phone P-256 long-term SPKI DER and derived device ID;
9. host and phone Noise static public keys, if distinct from ephemeral keys;
10. both Noise ephemeral public keys;
11. host and phone nonces;
12. finalized Noise handshake hash/channel binding;
13. protocol service identity;
14. requested role, explicitly tagged `untrusted_request`;
15. phone display metadata only if the host will persist it.

Host and phone P-256 signatures use different domain/role labels and cover
`Hash(T || finalizedNoiseHandshakeHash)`. The signature bytes, transcript hash,
and Noise split keys feed role-separated explicit key-confirmation messages.
Reflection of a host confirmation as a phone confirmation must fail.

Locator handling is fixed by type:

- IPv4 is four network-order bytes plus a network-order port.
- Hostnames are lowercase IDNA A-labels with no trailing dot plus a port;
  IP-like alternate spellings reject.
- Discovery uses a 16-byte opaque service instance, not discovered address text.
- The QR locator bytes are always bound. The observed peer address is recorded
  separately but is not required to equal the advertised address after a valid
  authenticated handshake.
- Address rewriting is allowed only when the authenticated invitation ID,
  service identity, host P-256 key, pairing generation, and channel binding all
  match. A redirect to another origin starts no fallback and carries no secret
  in a URL or header.

Before handshake encryption, transmit only the versioned route, invitation ID,
Noise framing, and unavoidable locator/network metadata. After encrypted PSK-
authenticated Noise state exists, exchange long-term public keys, signatures,
nonces, expiry/generation, and requested display metadata. Authoritative role,
permissions, durable device ID, host metadata, and commit records are delivered
only through the final key-confirmed encrypted channel.

## 5. Handshake and invitation state machine

```text
issued
→ oob_authenticated
→ authenticated_and_atomically_reserved
→ identity_keys_proven
→ sas_pending_local_confirmation
→ user_confirmed
→ key_confirmed
→ commit_prepared
→ client_commit_acknowledged
→ committed_and_consumed
```

1. `issued`: create a 16-byte invitation ID and independent 16-byte OOB secret
   with the OS CSPRNG. Keep the secret only in locked process memory when
   available; never log, persist in plaintext, place in argv, or send outside
   the QR. Default lifetime is the existing CLI default of two minutes.
2. `oob_authenticated`: process bounded Noise frames. Unauthenticated packets
   cannot mutate the invitation. PSK proof failure is rate-limited without
   revealing whether an invitation exists.
3. `authenticated_and_atomically_reserved`: after the first message that
   cryptographically proves OOB-secret possession, atomically reserve for the
   exact phone P-256 key, Noise static/ephemeral keys, connection ID, and pairing
   generation. Competing connections receive a generic unavailable result.
4. `identity_keys_proven`: verify role-separated P-256 possession signatures
   and the finalized transcript before notifying the CLI or displaying SAS.
5. `sas_pending_local_confirmation`: phone displays exactly six decimal digits
   derived with a dedicated label from the authenticated transcript. The SAS is
   not key material. Only the local CLI/host-local IPC accepts input.
6. `user_confirmed`: constant-time compare the local entry. Three wrong entries
   exhaust the invitation. Do not reveal the expected value or mismatch offset.
7. `key_confirmed`: exchange both role-separated confirmation MACs over the
   encrypted channel. Each MAC covers transcript, signatures, SAS-confirmed
   state, connection, generation, and direction.
8. Complete the crash-consistent commit protocol in Section 6.

Reservation timeout is 30 seconds, reusing the current pairing server idle
timeout, but never beyond invitation expiry. After an authenticated transport
failure before user confirmation, one reconnect may resume only with a resume
secret derived from the original AKE and proof by the exact phone P-256 key;
the reservation and attempt count remain. There is no restart resume: daemon
restart zeroizes the OOB secret, expires all non-committed invitations, and
requires a new QR. No failed invitation can bind a different key.

Use the existing device-trust limiter defaults as the starting minimum: burst
3 and refill 10/minute per source, plus one active reservation per invitation.
SAS failures are a separate non-refilling maximum of three. A bounded global
pending-handshake capacity must be derived and load-tested before freeze; until
then V2 implementation is blocked. Unauthenticated traffic must not reserve or
consume an invitation, but may be dropped before expensive cryptography when
the global budget is exhausted.

## 6. Crash-consistent registration

Distributed persistence cannot be made simultaneously atomic. V2 therefore
uses an idempotent staged commit keyed by a 128-bit transaction ID derived from
the confirmed transcript, with one durable host transaction record:

1. After bilateral key confirmation, the host writes and fsyncs
   `commit_prepared`: exact phone key/device ID, host key, assigned role and
   permissions, invitation, generation, transcript hash, and transaction ID.
   It is not accepted by authentication paths.
2. Host sends encrypted `commit_offer` with assigned role/permissions,
   transaction ID, host pin, and confirmation MAC.
3. Phone atomically stores a **pending, non-authoritative** host record and
   returns a signed/MACed `client_commit_ack`.
4. Host performs one durable atomic transaction that promotes the exact device
   registration, assigns role/permissions under the registry lock, marks the
   invitation consumed, and records the final receipt. If durable registration
   fails, none of those effects commits.
5. Host sends the encrypted final receipt. Phone verifies it and atomically
   promotes its pending host pin to active.

If the final receipt is lost, only the same phone P-256 key may request the
stored receipt through a versioned finalization endpoint using a
transaction-specific resumption secret and fresh signature. The endpoint cannot
change keys, role, permissions, generation, or invitation and never reruns the
write. Duplicate finalization returns the identical receipt. Other keys receive
no existence oracle.

On restart, the host replays only fsynced transaction records. Prepared records
remain non-authoritative and expire; committed records remain active and expose
only idempotent receipt recovery. The phone never promotes a pending pin without
the final receipt. Owner assignment is calculated and committed under the same
registry transaction; a role request cannot create an owner. Cleanup is bounded
and audit records contain identifiers/status only, never secrets or key
material.

## 7. Attack review

| Attack | Required outcome |
|---|---|
| Active LAN MITM | Cannot authenticate without OOB secret; transcript/channel mutation fails |
| Passive observer | Sees locator, invitation ID, and Noise framing only; identity metadata remains encrypted |
| Stolen QR image | Attacker has a bearer credential and may race; atomic reservation, local SAS, expiry, and audit limit damage but cannot make theft harmless |
| Replaced QR image | Can direct the phone to an attacker-controlled pairing flow; local Mac SAS cannot confirm against the legitimate CLI, but display compromise is outside cryptographic recovery |
| Relay to legitimate Mac | Not prevented by commitment; local CLI SAS and exact connection/transcript binding expose or reject session mismatch, but a transparent live relay with stolen QR remains in the bearer-secret threat model |
| Unknown-key-share/cross-host | Both long-term keys, service identity, invitation, generation, locator, and channel binding must match |
| Reflection | Direction- and role-separated signatures/MACs reject reflected messages |
| Replay | Fresh invitation, generation, nonces, ephemerals, connection ID, single-use reservation, and consumed transaction reject |
| Downgrade | Explicit V2 QR/route/message domains; no V1 fallback or content sniffing |
| Concurrent race | First valid PSK-authenticated reservation wins atomically; no second key can resume |
| Invitation exhaustion/DoS | Bounded frames, source limiter, global capacity, cheap prechecks; unauthenticated traffic cannot consume |
| SAS brute force | Local-only entry, three total failures, constant-time comparison, no remote oracle |
| Malicious role request | Request remains untrusted; host assigns under registry transaction |
| Compromised client | QR secret authorizes only this pending pairing; hardware P-256 possession, local confirmation, and least-privilege assignment still apply |
| Remote CLI access | Host-local IPC must enforce local OS ownership/permissions; remote shell compromise is equivalent to host compromise and is not cured by SAS |
| Lost confirmation/crash | Staged non-authoritative records and idempotent exact-key receipt recovery prevent invitation/key substitution |

The host commitment does not change the stolen/replaced/relay rows enough to
justify its terminal-size cost. The principal residual risk is QR bearer-secret
theft followed by a live race or relay; documentation and UI must state that
fact rather than claiming the six-digit SAS adds cryptographic entropy.

## 8. Version separation

- V1 and V2 use explicit, disjoint QR magic/version, routes, message types,
  token namespaces, transcript domains, invitation stores, and audit events.
- Unsupported or malformed V2 fails closed. There is no handshake sniffing,
  V2-to-V1 retry, invitation conversion, or hidden production compatibility.
- V1 removal, coexistence duration, and mobile minimum-version policy require a
  separate migration contract. V2 cannot weaken or silently replace V1 during
  the current device-gate sequence.

## 9. Mandatory verification

- Official Noise vectors for the exact suite plus Go/mobile cross-language
  vectors for every handshake message, split key, transcript hash, and PSK.
- Android Keystore and iOS Secure Enclave P-256 signature fixtures bound to the
  Noise/application transcript; wrong key, role, hash, and direction negatives.
- Every single-field transcript mutation, non-canonical encoding, unknown flag,
  wrong PSK, wrong static/ephemeral key, downgrade, reflection, UKS, locator
  rewrite, origin mismatch, replay, and cross-host/session negative.
- SAS appears only after PSK and identity authentication; exact derivation
  vectors, wrong entry, constant-time comparison, local-only input, attempt
  exhaustion, and absence from logs/network endpoints.
- Atomic reservation under high concurrency, exact-key reconnect, competing-key
  rejection, expiry/confirmation races, reservation timeout, global/source
  exhaustion, bounded unauthenticated work, and restart invalidation.
- Crash/fault injection before and after every durable transition, fsync, rename,
  registry promotion, invitation consumption, ACK, and receipt; no authoritative
  tentative records, duplicate registration, owner duplication, or invitation
  reuse.
- Idempotent same-key receipt recovery and wrong-key/changed-role/changed-
  permission negatives.
- Passive packet/log/process-argument capture proving that identities and
  metadata appear only at the permitted encryption phase and OOB secret never
  escapes QR handling.
- Exact V1/V2 route, namespace, malformed-input, and no-fallback tests.
- Canonical envelope decode/re-encode and the exact QR-size table for all
  locator/commitment candidates at M and Q.
- Post-pairing challenge authentication against the persisted host fingerprint
  and registered phone key, including cold start and token renewal.
- Full daemon build/vet/race, mobile typecheck/Jest/native tests, physical
  Android and iOS interoperability, and an independent cryptographic review.

## 10. Stop conditions

Stop before implementation or acceptance if:

- maintained compatible implementations of the exact named suite and official
  vectors are unavailable on either Go or React Native/Hermes;
- application code implements Noise primitives or assembles a custom AKE;
- P-256 hardware-key possession is not cryptographically bound to the finalized
  handshake and transcript;
- the SAS is network-submittable, displayed before authentication, used as key
  material, or described as more than a 20-bit check;
- unauthenticated traffic can reserve/consume an invitation or trigger registry
  mutation;
- any auth path accepts a tentative registration or pending phone pin;
- registry promotion, role assignment, and invitation consumption cannot be
  committed/recovered as one exact transaction;
- a retry can change phone key, role, permissions, generation, transcript, or
  transaction identity;
- V2 can downgrade/fallback to V1 or reuse a V1/V2 token namespace;
- QR bytes or measured dimensions exceed the frozen table without re-review;
- the proposal is pulled before the current SM-S926N gate and final PB ACCEPT.

Pairing V2 remains a separate post-device-gate security packet. This document
authorizes only future design validation and dependency/interoperability spikes;
it authorizes no production implementation.
