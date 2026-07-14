# A1.1 CP0 Lifecycle Evidence Packet (#6) — Contract Note (pre-implementation)

Status: **CP0 EVIDENCE ONLY — capacity ZERO; no production code; H0 ACCEPTED baseline**

Date: 2026-07-15. Executor: Claude Code. Baseline `b60ed2474` (H0/H0-R1 accepted).
Authority: handoff §9/§10 (proportionate-scope CP0 criteria) + plan §7/§8/§11.2 +
reviewer ACCEPT boundary ("남은 CP0 증거 수집"; cdhash/공급망/전체 schema bundle 재개 금지).

## 1. Pinned provider executable (version-drift containment)

Global PATH codex drifted `0.144.1 → 0.144.4`. Per user directive, evidence uses a
dedicated out-of-repo prefix `~/.pokit-cp0-toolchain` holding exactly
`@openai/codex@0.144.1`; the harness executes the explicit absolute path (never
PATH), fail-closed (exit 4) on version-string or shim/native sha256 mismatch,
recording package-lock resolved/integrity, artifact digests, and the (unused)
global version for contrast. Ambient `~/.codex` auth only; no credential copies.
**Claim wording**: "exact-version 0.144.1 executable executed via explicit path,
version + artifact digests recorded before execution" — NOT a complete
process-image attestation; gate #1 remains BLOCKED per proportionate-scope criteria.

Post-install checks (all PASS before this packet): ① `--version` == 0.144.1
(fail-closed path); ② app-server initialize handshake success; ③ consumed schema
subset 17/17 raw-sha256-identical to the committed manifest (266/267 whole-bundle;
see §5); ④ global 0.144.4 preserved; ⑤ pinned child spawn/stop leaves no process,
no lock.

## 2. Traces to capture (each under the run lock, one AppServer per run)

Every trace begins with a `trace_header` record: run id, pinned identity (version,
shim/native sha256, lock integrity), supervisor-acknowledged spawned PID/PGID,
`ps lstart` process-start identity, connection generation (= run id; seq epoch
starts at 1), global-PATH version marked `not_used`.

1. **accept recapture** — fixed H0 harness; corroboration: probe file created.
2. **decline recapture** — probe file NOT created.
3. **duplicate** — respond accept; after matching `serverRequest/resolved`, write a
   SECOND response with the same JSON-RPC id via a clearly-marked wire-level seam
   that bypasses the local claim (the claim itself already refuses duplicates —
   proven by `test_response_no_deadlock`). Record provider error/silence; assert no
   second `resolved`; probe created exactly once.
4. **timeout** — never respond; bounded observation window; assert NO `resolved`
   and the request stays pending without a response; then `stop()` kills the owned
   child; a post-stop respond attempt must fail closed (`send() == -1`). No
   synthesized decision (plan §8: timeout before reservation → send nothing).
5. **cancel** — on approval-pending, send `turn/interrupt {threadId, turnId}`
   (schema-verified method); observe provider-side resolution/abort ordering. A
   `resolved` that precedes any response write is provider-side resolution, NOT
   success (plan §7). A post-interrupt respond attempt is recorded as
   refused/ambiguous, never success.

## 3. States and linearization (unchanged from H0 + one seam)

Claim linearization stays `_responded_ids` under `AppServer._lock`; wire
publication stays write+flush+seq in `send()` under the same lock; PG ownership
stays the READY-frame parse. The duplicate trace's second write is a **test-only
wire seam** deliberately outside the claim — labelled as such in code and
evidence; it exists to record PROVIDER duplicate handling, not to weaken the claim.

## 4. Success evidence and fail-closed rules

Per trace: ordered redacted JSONL (monotonic seq + direction), summary flags,
existing `_verify_cleanup` (owned-PG scan), lock released by the single outer
finally. Ambiguous orderings are recorded as ambiguous — never success. Identity
verify failure aborts before any spawn (exit 4).

## 5. Schema-subset pinning (replaces full-bundle work)

Pin ONLY the consumed subset (initialize/thread/turn/item/commandExecution
requestApproval/serverRequest resolved/turn interrupt files): per-file raw sha256 +
canonical-JSON digests, committed as a subset manifest. Finding recorded: the prior
"reproducibility discrepancy" is characterized — the committed manifest used raw
sha256 while the newer harness used canonical-JSON digests; raw re-comparison shows
266/267 byte-identical (single differing file: aggregate
`codex_app_server_protocol.v2.schemas.json`; old bytes not retained, cause not
determinable — recorded honestly, excluded from the pinned subset), and
back-to-back pinned generation is byte-stable (`schema_repro=True`).

## 6. Non-goals

No cdhash/EndpointSecurity/supply-chain/publisher certification; no full-bundle
byte-for-byte certification; no CP1/N1/A1.2/O1/O2; no production code or capacity;
no #7 production entry-path implementation (design/justification text only, marked
NOT PROVEN); no plan edits; no global toolchain changes; no credential copies.
