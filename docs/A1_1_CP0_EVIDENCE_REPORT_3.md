# A1.1 CP0 — Evidence Report 3 (lifecycle traces; NOT a completion claim)

Status: **CP0 IN PROGRESS — gate #6 (cancel/duplicate/timeout/cleanup) PROVEN with
pinned 0.144.1; #2–#5 re-corroborated on the fixed harness; #1 stays BLOCKED
(risk-proportional); #7 NOT PROVEN (design open). A1.1 / A1 / N1 / CP1 remain
BLOCKED. Capacity stays ZERO.**

Date: 2026-07-15. Executor: Claude Code, canonical checkout. Baseline `b60ed2474`
(H0/H0-R1 independently ACCEPTED). Contract note:
`docs/A1_1_CP0_LIFECYCLE_EVIDENCE_CONTRACT_NOTE.md`. This revision incorporates the
reviewer-identified privacy/identity remediation (below) with fully re-captured
traces. The independent reviewer validated and committed the prepared packet from
the canonical checkout without rerunning provider turns.

Implementation/evidence commit: `3f5164b0cf93d198aeb455a39f1d7745f7937fee`.

## 1. Pinned provider executable (version-drift containment)

Global PATH codex drifted `0.144.1 → 0.144.4` between sessions. Per user directive:

- exact `@openai/codex@0.144.1` installed in the dedicated out-of-repo prefix
  `~/.pokit-cp0-toolchain` (npm; package-lock resolved/integrity recorded in every
  trace header); global 0.144.4 untouched (recorded as `not_used` for contrast);
- the harness executes the explicit absolute path only (no PATH lookup; prefix via
  `expanduser`, no hardcoded username) and fail-closes (exit 4) on version-string
  mismatch, on shim/native artifact-digest mismatch, and when `.bin/codex` does not
  `realpath`-resolve to the digest-verified shim (`_pinned_codex_bin`);
- artifact digests match the original 0.144.1 evidence exactly:
  shim `134063e1…`, native `29915529…`;
- ambient `~/.codex` auth only; no credential copies.

**Claim wording (bounded)**: an exact-version 0.144.1 executable in a dedicated
directory was executed via explicit path with version + artifact digests recorded
before execution. This is NOT complete process-image attestation; gate #1 stays
BLOCKED per the risk-proportional criteria (cdhash/supply-chain not resumed).

Post-install checks (all PASS): version fail-closed path; initialize handshake;
consumed schema subset 17/17 raw-sha256-identical to the committed manifest;
global preserved; spawn/stop leaves no process and no lock.

## 2. Privacy/identity remediation (reviewer blockers, all fixed + re-captured)

The first capture round was correctly held back from commit. Blockers and fixes,
all in `scripts/cp0/appserver_probe.py`:

1. **Raw prompt/command payload leak** — `turn/start.input` and
   `availableDecisions` passed nested structures verbatim. Fixed with
   method-specific structural projectors (`turn/start`, `turn/started`,
   `item/started`, `item/completed`, `item/commandExecution/requestApproval`):
   `input` → `<REDACTED-INPUT>`; `availableDecisions` preserves ONLY bounded
   decision tags (`['accept', '<acceptWithExecpolicyAmendment:PAYLOAD-DROPPED>',
   'cancel']`) and drops amendment/exec-policy payloads; commands always
   `<REDACTED-CMD>`. Defense in depth: the generic allowlist path now redacts any
   nested dict/list (`<REDACTED>`) so no future allowlisted nested field can leak.
2. **Pseudonyms destroyed identity relations** — `_pseudo` was a per-label counter
   (same real thread ID → thread-1/3/5…). Now a stable
   `(field-domain, original-value) → pseudonym` map, reset once per run;
   `requestId` shares the `objid` domain with the JSON-RPC `id`. In the committed
   accept trace the relation `request.id == our-response.id ==
   resolved.requestId == objid-4` and the single `thread-1` token are now
   independently machine-checkable, and `item/started.itemId ==
   requestApproval.itemId (item-4)` binds the ordered chain.
3. **Pinned-path boundary** — hardcoded `/Users/<user>` removed (`expanduser`);
   added fail-closed `.bin/codex realpath == verified shim` check.

Re-verified on the final re-capture: leak scan (prompt / command / payload /
`/Users` / `/tmp` / hostname patterns) CLEAN over all five wire traces and three
summaries; gate #2 ordering fully visible in redacted form:
`item/started(seq 23) < requestApproval(24) < our write(25) < resolved(26)`.

**Disclosure — older committed evidence**: the SAME leak classes (fixed harness
prompt text, benign probe command strings, and amendment payloads) existed in the
earlier-round versions of these paths and remain in git history:
`wire_accept.jsonl`, `wire_decline.jsonl`, `command_approval_accept.json`,
`command_approval_decline.json`. Git history is not rewritten. In the current tree,
this packet replaces all four legacy full-payload artifacts with small
`superseded_evidence` tombstones pointing to the privacy-safe pinned traces. The raw
historical content contains no credentials or user-authored content and is
non-authoritative for all future review.

## 3. Gate #6 — lifecycle traces (pinned 0.144.1, real turns, remediated redaction)

Evidence: `docs/a1_1_cp0_evidence/wire_{accept_pinned,decline_pinned,duplicate,
timeout,cancel}.jsonl` (ordered, monotonic seq + direction, `trace_header` with
run id, connection generation, supervisor-acknowledged PID/PGID, `ps lstart`
process-start identity, pinned identity) + `lifecycle_{duplicate,timeout,cancel}
_summary.json`.

| Trace | Result | Key facts |
| --- | --- | --- |
| accept (recapture) | PASS | item/started(23) → request(24) → our write(25) → resolved(26); identity chain objid-4/item-4/turn-1/thread-1 stable; probe file created; cleanup verified |
| decline (recapture) | PASS | same ordering; probe file NOT created |
| duplicate | PASS | first response → resolved; second response with SAME JSON-RPC id (wire-level seam, deliberately bypassing the local claim): **provider silently ignores — no error, no second resolved**; probe created exactly once |
| timeout | PASS | no response for the 60 s window → **no resolved without a response** (request stays pending); then stop() kills the owned child; post-stop send fails closed (−1); probe NOT created; nothing synthesized |
| cancel | PASS | `turn/interrupt {threadId, turnId}` (schema-verified; turnId from the approval request itself) → **resolved arrives BEFORE any response write** = provider-side resolution, NOT success (plan §7); late response silently ignored (no provider error); probe NOT created |

**Findings that materially affect CP3 design**: the provider emits NO error for
duplicate or late responses — it silently ignores them. POKIT's own claim /
reservation / resolved-ordering machinery is therefore the ONLY duplicate/late
defense; no corroborating provider error signal exists. Fail-closed ordering
(`resolved` counts only after our exact write) is confirmed live in both
directions. The fixed H0 response path (claim under lock → send after release) is
live-validated by all five traces.

## 4. Schema-subset pinning + reproducibility finding RESOLVED

`docs/a1_1_cp0_evidence/schema_subset.manifest`: 17 consumed-subset files
(initialize/thread/turn/item/commandExecutionRequestApproval/serverRequest
resolved/turnInterrupt), each with raw sha256 + canonical-JSON sha256, generated
by the pinned binary.

The prior "reproducibility discrepancy" (evidence report 2 finding 3) is
**characterized and resolved**: the committed manifest used raw byte sha256 (old
harness) while later regeneration used canonical-JSON digests (newer harness) — a
digest-METHOD difference, not generation instability. Raw re-comparison:
**266/267 files byte-identical** between the pinned install and the committed
manifest; consumed subset **17/17 identical**; back-to-back pinned generation is
byte-stable (`schema_repro=True mismatch_count=0`). Single differing file:
aggregate `codex_app_server_protocol.v2.schemas.json` (old bytes not retained;
cause not determinable; contains no env-dependent strings; excluded from the
pinned subset — recorded honestly).

## 5. Honest scope notes

- The duplicate trace's second write uses a labelled wire-level seam that bypasses
  the local claim — evidence of PROVIDER behavior only; the local claim's
  duplicate refusal is separately proven by `test_response_no_deadlock`.
- Focused no-model suite on the final tree: 8/8 PASS. No production code, mobile
  or adapter changes; capacity zero; `provenActionMapping` empty.
- Remaining CP0 work (unchanged boundaries): gate #1 process-image identity
  BLOCKED (cdhash/EndpointSecurity/supply-chain NOT resumed); gate #7 bounded
  production thread/turn entry path NOT PROVEN (design open, nothing implemented);
  runtime replacement/reconnect invalidation evidence deferred to a later packet.

A1.1, A1, N1, CP1 remain BLOCKED. This report stops at the CP0 evidence boundary.

## 6. Review marker

```text
REVIEW REQUEST: A1.1 CP0 Lifecycle Evidence (#6) — 3f5164b0cf93d198aeb455a39f1d7745f7937fee
```
