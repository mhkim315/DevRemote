# M3-auth-R1 Final Acceptance

Status: **ACCEPT**

Accepted branch: `feature/phase10-multi-adapter`

Accepted commit:

```text
a834955c15a9e95535364184a7467a07587745ed
```

Baseline (accepted M3-auth-4A) — `a834955c1` is a descendant of:

```text
b34c33528dd50dd6daa8321b358a3fa3e5aca90b
```

M3-auth-R1 delivered the authenticated Android M3a end-to-end re-verification and
the smallest missing REST wiring: the mobile New Session flow (launch-profile
list + session create) now rides the same host-bound device transport as the
accepted reads.

## Accepted product contract

Confirmed by two independent verifications:

- `GET /api/session-profiles` and `POST /api/sessions` use the host-bound device
  bearer (paired-origin only), not the legacy Supabase/dev token.
- A non-paired origin (or any host/port/path/query/fragment/userinfo/non-HTTPS
  variant) makes **zero** network requests and the bearer is never sent.
- The Supabase token is never present on a paired-device request.
- The create payload is exactly `{profileId, name, cwd}` — never `id`, `runner`,
  `command`, `executable`, `adapter`, or `workspaceId`.
- A non-idempotent create is never auto-replayed after `401`/`403`: exactly one
  POST, no duplicate session; a rejected create surfaces a recoverable auth error
  and leaves the form open.
- A malformed 2xx response is not runnable — no Terminal navigation and no
  phantom session card.
- New Session navigates only on a Recorder-ready `controlled_pty` + `running`
  response with a canonical id.
- Ticket Terminal transport, Recorder single-reader ownership, binary/text
  framing, and host binding are unchanged from M3-auth-4A.

## Independent evidence

- Go build: PASS
- Go vet: PASS
- Go race tests: PASS
- `git diff --check`: PASS
- Mobile TypeScript: PASS
- Jest: PASS (`209` tests; `201` at 4A + `8` new M3a auth-wiring proofs)
- Full `scripts/build-gate.sh`: **ALL GATES PASSED**, including the Android
  Kotlin compile gate that the M3-auth-4A verifier could not independently run.

Implementation report: `docs/M3_AUTH_R1_IMPLEMENTATION_REPORT.md`.

## Remaining (non-blocking)

- Physical Android product smoke (install release APK → LAN QR pair → HTTPS
  tunnel/LTE → cold start without Supabase → create Shell/Codex/Claude → open
  ticket Terminal → bidirectional IO → background/foreground reconnect → geometry
  mirror → natural exit/history) remains an explicit **M-track** release gate. It
  is not a code blocker.
- FOLLOW-UP: an explicit user-Retry-with-fresh-bearer UX after a non-idempotent
  401 can be refined in a later interaction phase. `resolveApproval` remains on
  the legacy transport (outside the M3a create/open path).

## Next phase

`M3-auth-1B` — iOS Secure Enclave DeviceKey provider (replaces the fail-closed
iOS stub with a hardware-backed provider matching the accepted Android contract).
