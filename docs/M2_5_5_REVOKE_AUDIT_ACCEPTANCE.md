# M2.5-5 Revoke and Minimal Audit — Acceptance

Status: implementation complete

Scope: local operator surface (IPC + CLI) for device list/revoke, end-to-end
revocation enforcement through the production callback (bearer invalidation,
WS ticket purge, live connection close), and a structurally redacted
append-only local audit trail.

## Product contract

Paired-device management is a **local-only** privileged operation over the
`0600` Unix socket (`/tmp/pokit.sock`). There is no remote/tunnel
device-management route this phase; `devices:manage` is deferred to M3+.

### `pokit devices`

Lists every paired device (including revoked ones) from the device registry
using the UI-safe `PublicDevice` view — the **full canonical device ID**
(printed verbatim on its own line, directly copy-pasteable into
`pokit devices revoke <id>`), plus fingerprint, display name, role, last-seen,
and `revokedAt` (if applicable). Raw public-key DER material is never exposed.

### `pokit devices revoke <deviceId>`

Revokes a device through the production wiring:

1. `DeviceRegistry.Revoke(id)` — marks the device revoked and persists it.
   An unknown device returns an error; an already-revoked device is
   idempotent (reports `"revoked"`).
2. `DeviceSessionManager.RevokeDevice(id)` — immediately invalidates the
   active bearer session (no re-authentication), purges all pending WS
   tickets via the wired `WSTicketStore.RevokeForDevice`, and closes every
   live WebSocket via the wired `AuthenticatedConnRegistry.CloseDevice`.
3. A redacted `device.revoke / ok` audit event is recorded.

A revoked key cannot re-authenticate (`DeviceRegistry.GetActive` / `Add`
return `ErrDeviceRevoked`), cannot issue new tickets (the bearer is gone),
and any existing WebSocket connections are severed before the CLI reports
success.

### `pokit audit [--limit N]`

Reads up to `N` most-recent structurally redacted audit events from the
append-only local trail (default limit: 100).

## Audit redaction contract

The audit record is an `AuditEvent` with exactly six fields:

| Field | Purpose |
|---|---|
| `timestamp` | UTC time of the event |
| `deviceId` | authenticated device, or "" if not known |
| `action` | closed set: `auth.verify`, `device.revoke`, `session.stop`, `session.kill`, `ws.ticket.deny` |
| `sessionId` | affected terminal session, or "" (e.g. for auth-only events) |
| `result` | `granted` / `denied` / `ok` / `error` |
| `correlationId` | bearer session ID, or "" (e.g. for deny events with no session) |

**Structural redaction**: there is no field capable of holding terminal
content, input, transcript, private keys, tokens, pairing secrets,
signatures, or push tokens. If something cannot be expressed by these six
fields, it is not audited.

**Closed-vocabulary + syntactic validation** (the sink accepts only supported
shapes — invalid events are dropped in full, never sanitized):

- `action` and `result` must be in their private closed sets.
- `deviceId` and `correlationId` must be hex (device fingerprints / bearer
  session IDs are always hex) and ≤ 256 bytes — a JWT, bearer token, or
  free-form text is rejected.
- `sessionId` must be "" or a canonical `<adapter>:<local-id>` (adapter `[a-z_]+`,
  local-id ≤ 256 bytes, no whitespace or control characters) — a shell command,
  escape sequence, or token shape is rejected.

**Server-derived only**: emit sites never echo untrusted request input. Lifecycle
stop/kill records the canonical `LifecycleResult.SessionID` (validated by a real
session lookup), not the URL path value; `ws.ticket.deny` omits the untrusted
`?session=` query entirely; `deviceId`/`correlationId` come from the
authenticated principal / freshly issued bearer.

## Storage

- Path: `~/.pokit/audit.jsonl` (owner-only `0600` file, `0700` parent dir —
  the same permission convention as `host_identity.json` and `devices.json`).
- Format: append-only JSONL, one JSON object per line, mutex-guarded.
- Rotation: single-generation (`audit.jsonl` → `audit.jsonl.1`) at 1 MiB
  threshold. Insecure permissions on either file cause `List()` to return
  no events (fail-closed).
- Writes are **best-effort**: an audit I/O failure never blocks or fails the
  security action that produced the event.

**Secure write path** (before every append):

- `Lstat` the path; refuse to write through a symlink or any non-regular file
  (directory, FIFO, socket).
- A pre-existing user-owned regular file with broader-than-`0600` permissions
  is corrected to `0600` and re-verified; if it cannot be secured, the write
  fails closed.
- A new file is created atomically at `0600` (no window of broader visibility;
  `0600` has no group/other bits for the umask to leave set).
- The open uses `O_NOFOLLOW` (darwin/linux) to close the `Lstat`→`open` TOCTOU
  window: if the final path component is or becomes a symlink, the open fails
  rather than following it.

## Emit points

| Event | When |
|---|---|
| `auth.verify / granted` | Successful challenge-verify — deviceId + new bearer session ID |
| `auth.verify / denied` | Device not active, or signature verification failed — deviceId only, no secret details |
| `ws.ticket.deny` | Ticket issuance capacity (429), expired authorization, or invalid grant — denials only |
| `session.stop` | POST stop handler — result ok/error |
| `session.kill` | POST kill handler — result ok/error |
| `device.revoke` | IPC devices-revoke — ok |

Grants (ws-ticket, session stop/kill) are not audited to bound noise in the
common case; denials and revocations are.

## Wire path

```
pokit devices revoke <id>
  → 0600 Unix socket → JSON IPC (devices-revoke)
  → handleDevicesRevoke
    → DeviceRegistry.Revoke(id)           — persists revokedAt
    → DeviceSessionManager.RevokeDevice(id) — fires onRevoke callback
      → connRegistry.CloseDevice(id)       — closes live WS
      → wsTickets.RevokeForDevice(id)      — purges pending tickets
    → audit.Record(device.revoke)
  ← {"status":"revoked","deviceId":…}
```

This uses the proven production wiring from M2.5-4 (the same callbacks
validated by `TestProofDeviceRevokeClosesLiveControlledPTYWS`). No new
close path is introduced.

## Implicit invariants preserved

- Recorder remains the single PTY reader.
- Auth middleware, route mode separation, permission vocabulary, bearer
  session manager, ticket model and store, connection registry, and
  controlled-PTY runtime are untouched.
- The `devices-manage` permission is not yet exposed to the remote HTTP
  product group — the device-admin surface is strictly local.

## Evidence

Automated coverage:

- `internal/devicetrust/audit_test.go`: round-trip, structural redaction
  (exact JSON key set), empty-action drop, rotation, 0600 perms, fail-closed
  List on insecure perms, concurrent Record under `-race`.
- `internal/devicetrust/audit_security_test.go`: forged action/result dropped;
  malicious identifier fields (newline/CR/NUL, escape sequences, oversized,
  non-hex tokens, bearer/JWT/terminal-text shapes) dropped in full with no
  sentinel byte reaching disk, valid canonical events still writable; filesystem
  security (new file 0600, pre-existing 0644 corrected, symlink rejected,
  directory/FIFO/non-regular rejected, append still works).
- `internal/devicetrust/audit_emit_test.go`: verify grant (deviceId +
  correlationId), bad-signature deny (device present, no secret details),
  ws-ticket deny (only the denial is audited; untrusted session query omitted).
- `internal/term/devices_ipc_test.go`: IPC dispatch over `net.Pipe` for list
  (roles ordered, no raw public-key material leaked), revoke (registry
  revoked, bearer session gone, onRevoke fires, audit recorded), and
  unknown-device error.
- `cmd/devremote/devices_cli_test.go`: the list line prints the full device ID
  verbatim, and the exact listed ID passed unchanged into revoke actually
  revokes the device (command-layer round-trip).
- `cmd/devremote/devices_admin_test.go`: real IPC socket → real
  controlled-PTY WS → revoke over the socket → WS closes, connection
  registry cleared, bearer invalid, audit recorded.
- Additionally reuse the existing full end-to-end M2.5-4 acceptance test
  suites (`TestProofReplacementClosesLiveControlledPTYWS`,
  `TestProofDeviceRevokeClosesLiveControlledPTYWS`,
  `TestProofInvalidTicketsRejectedBeforeUpgrade`,
  `TestProofGlobalTicketCapConcurrentProductionIssuance`, and the new
  `TestProofDeviceRevokeViaLocalIPCClosesLiveWS`) — all pass under
  `go test -race -count=10`.

`scripts/build-gate.sh` passes backend build/vet/race tests, mobile typecheck,
architecture invariants, and secret scanning (audit JSON and tests contain no
token/prefix/key literals).

## Explicit follow-ups

Non-blocking (no test currently exposes a defect):

- **Final-owner revoke policy**: revoking the last owner device is currently
  permitted; whether to guard it (and re-pair recovery) is unspecified.
- **Synchronous I/O wording**: "never block" refers to never failing the caller's
  security action; the file append is synchronous under the audit mutex.
- **Narrow test hook**: `Recorder.SubscriberCount()` — a read-only production API
  extension for test observability. Candidate for package-private cleanup.
- **npm moderate**: 10 moderate advisories in `npm audit`; none introduced here.
- **Per-device capability/adapter list** and **device display-name editing** are
  not yet specified; the `PublicDevice` view is ready for that enrichment.

## Commit history (this branch)

- `8cc3a85b4` — audit log core (`AuditLog`, `FileAuditLog`, structural redaction)
- `0694c4302` — emit audit at boundaries (nil-safe hooks)
- `241917961` — revoke integration + local IPC/CLI surface
- remediation — full CLI device ID, closed-vocabulary + syntactic audit
  validation (server-derived identifiers only), secure `O_NOFOLLOW` write path
  with symlink/non-regular rejection and 0600 correction, and the injection /
  filesystem / CLI-round-trip regression suites
