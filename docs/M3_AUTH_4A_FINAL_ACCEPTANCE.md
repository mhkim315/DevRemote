# M3-auth-4A Final Acceptance

Status: **ACCEPT**

Accepted branch: `feature/phase10-multi-adapter`

Accepted commit:

```text
b34c33528dd50dd6daa8321b358a3fa3e5aca90b
```

## Accepted product contract

- Android hardware-backed paired DeviceKey is the remote authentication root.
- Paired-device product routing does not require Supabase.
- Operational REST reads use a short device bearer with refresh behavior.
- The central device transport is bound to the exact canonical paired origin;
  cross-origin variants fail before any network request.
- Terminal bootstrap uses the bearer only for authenticated REST requests.
- WebSocket upgrade uses a one-time, session-bound ticket.
- Tickets are exact 32-byte lowercase-hex values, memory-only, and ephemeral.
- Reconnect obtains fresh tickets without reuse or parallel issuance.
- Binary WebSocket frames are raw PTY data; text frames are closed control
  messages.
- Geometry control does not contaminate Terminal output, Recorder bootstrap,
  Activity, Transcript, or local attach.
- Recorder remains the sole PTY reader.
- Pairing installation is origin-bound and completes before connection.

## Independent evidence

- Go build: PASS
- Go vet: PASS
- Go race tests: PASS
- Mobile TypeScript: PASS
- Jest: PASS (`201` tests at acceptance)
- Production-path generated-script tests: PASS
- Daemon ticket/replay/revoke/replacement/expiry proofs retained

Physical Android WebView smoke remains an explicit M3-auth-R1 release gate.

## Non-blocking follow-ups

- Manual Disconnect/reconnect UX cleanup
- base URL trailing-slash normalization
- old-APK text-frame Send compatibility verification
- `npm audit` moderate advisories not introduced by this phase

## Next phase

`M3-auth-R1` — authenticated Android M3a end-to-end re-verification.

Authoritative handoff:

`docs/NEXT_SESSION_M3_AUTH_R1_HANDOFF.md`
