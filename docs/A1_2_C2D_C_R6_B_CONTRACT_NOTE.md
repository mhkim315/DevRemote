# A1.2 C2D-C R6-B — Production Denial Decoder/Binding Contract Note

Status: **R6-B AUTHORIZED BY R6-A7 ACCEPT — DECODER/BINDING ONLY — C2D-D/C3D PROHIBITED**

Authority: the independent verifier accepted the R6-A7 evidence packet
(`dbf6e50`, report `docs/A1_2_C2D_C_R6_A7_EVIDENCE_REPORT.md`, projections
`docs/a1_2_c2d_c_r6a7_projection_run_{a,b}.json`) and froze the production
deny witness as:

```text
session_id
+ tool_use_id
+ tool_name
+ canonical_digest(raw tool_input decoded from the provider denial event)
+ current runtime / resume-attempt binding
```

This note is committed before any production change. It binds exactly what
R6-B may implement and how each prior rejection is closed.

## 1. Frozen wire contract (evidence-backed)

From both live projections (stable, run-independent):

- The resumed stream-json contains **one** `type:"result"` event carrying
  `permission_denials` as a non-empty array.
- The carrying event's top level has 20 observed fields; only `type`,
  `session_id`, and `permission_denials` are witness-authority fields. The
  rest (usage, durations, cost, uuid, …) are advisory and must be tolerated
  without being decoded into authority state.
- Each denial entry's top level is **exactly**
  `{tool_input: object, tool_name: string, tool_use_id: string}`.
- There is **no provider digest field**. `input_sha256` on the wire was an
  invention of the rejected `317a4eb` and must never be assumed again.
- The canonical digest of the entry's raw `tool_input` equals the deferred
  invocation's private input digest when and only when the input is the same.

## 2. Prohibitions carried from rejections and the verifier verdict

| # | Prohibition | Source |
|---|---|---|
| P1 | No stored-digest substitution: the digest passed to `MarkWitnessed` for the deny path MUST be recomputed from the exact event's `tool_input`; `ctx.inputDigest` fallback is forbidden, including when the event omits `tool_input` | R6-A2 B2, R6-A5 §1, verdict |
| P2 | No coordinator scan by partial provider identity (`LookupClaimToken`-style discovery) | R6-A2 B1 |
| P3 | `PostToolUse` can never prove deny; `handlePostTool` always routes `WitnessPostToolUse` | R6-A3/A4 preserved fix |
| P4 | Raw `tool_input` is internal-only: bounded decode + digest comparison; never stored beyond the decode scope, never in DTOs, logs, receipts, or public evidence | verdict |
| P5 | The evidence probe/harness (`docs/a1_2_c2d_c_r6a7_*`) is not promoted to production authority; production code cites the committed projections, not the probe | verdict |
| P6 | A denial-shaped line that fails strict decode is never silently skipped (the evidence parser's skip-malformed behavior must not be copied) | verdict caution |
| P7 | Hook bodies are bounded by reading `max+1` bytes and rejecting `> max` (already implemented in all three bridge handlers; regression-tested, not weakened) | verdict caution |
| P8 | No production installation: `app.go`, mobile, Codex, and the provider-neutral A1 core are untouched; the boundary remains exercised by controlled composition only | C2D contract §8/§12 |

## 3. Decoder contract (`managed_claude.go`)

Two-phase, all under existing pump ownership:

1. **Detection** — a line is denial-shaped iff it parses as a JSON object
   with `type == "result"` and a `permission_denials` member. Non-JSON and
   non-denial lines keep today's behavior (other decoders / ignore). A line
   the scanner cannot deliver (> 1 MiB buffer) ends the pump and the runtime
   terminates; the delivery then fails closed via the existing
   cancel/timeout path — this is recorded honestly as a limitation, not
   hidden.
2. **Strict authority decode** — applied to every detected line:
   - token-walk the top level: authority fields (`session_id`,
     `permission_denials`) extracted exactly once each — a duplicate
     authority key is malformed; unknown top-level fields are skipped as
     opaque JSON values without interpretation;
   - `session_id`: non-empty, ≤ 256 bytes, printable ASCII;
   - `permission_denials`: 1..16 entries;
   - each entry: CLOSED allowlist `{tool_use_id, tool_name, tool_input}`,
     token-walked; unknown or duplicate entry fields are malformed;
     `tool_use_id`/`tool_name` non-empty ≤ 256 printable; `tool_input`
     present, valid JSON, canonical form ≤ 32 KiB (`maxToolInputBytes`);
   - per-entry digest = `sha256Hex(canonicalJSON(tool_input))` — the same
     two functions used at PreToolUse observation and deferred join;
   - **malformed ⇒ fail closed**: the pump cancels the active resume entry
     (`CancelEntry`), producing a non-success terminal result; it never
     continues waiting for a better-shaped denial.

## 4. Binding contract (`routeDenial` + resume attempt)

- The resumed runtime owns an **immutable private `resumeCtx`** field set in
  `ResumeForApproval` before `go rt.pump()` starts (happens-before via
  goroutine start). `routeDenial` reads only this field. It no longer
  fetches context through the bridge (closes R6-A3 "pump reads
  `bridge.resumeCtx`"). C1D observation runtimes have nil `resumeCtx` and can
  never witness.
- Matching: within one decoded denial event, entries whose `tool_use_id`
  equals the bound tool-use ID are counted. Zero matches → not our witness
  (event ignored; retry-denial entries for other tool-use IDs are expected).
  More than one match → ambiguous → `CancelEntry` (fail closed).
- For the single match, `routeDenial` requires `event.session_id ==
  ctx.claudeSessionID` and `entry.tool_name == ctx.toolName`, then calls
  `MarkWitnessed(ctx.claimToken, WitnessPermissionDenials, event.session_id,
  entry.tool_use_id, entry.tool_name, recomputedDigest, ctx.originalRuntime)`.
  A session/tool-name mismatch on a tool-use-ID match is a cross-binding
  anomaly → `CancelEntry` (fail closed), never a retry wait.
- `MarkWitnessed` (unchanged) enforces the rest of the frozen contract:
  entry exists for the exact claim token, state is `decisionWritten`, the
  witness kind matches the stored deny decision, all four identity fields
  equal the stored entry (so the recomputed digest must equal the digest
  captured at PreToolUse observation), and the RuntimeRef equals the original
  approval target. Early, duplicate, late, or mismatched witnesses leave the
  entry unchanged or fail closed exactly as today.

## 5. Adversarial counterexamples (each becomes a test)

| # | Counterexample | Required outcome |
|---|---|---|
| CE-1 | Real-wire-shaped denial with same sid/tuid/tname but **mutated `tool_input`** | no witness; delivery non-success; known-bad control proves `MarkWitnessed` alone (fed the stored digest) would accept — the decoder recomputation is the enforcing boundary, so the test is non-vacuous |
| CE-2 | Old-style minimal event without `tool_input` (the pre-R6-B fixture shape) | malformed → fail closed; proves the old circular fixture can no longer pass |
| CE-3 | Entry with an unknown extra field | malformed → fail closed |
| CE-4 | Two entries with the bound tool_use_id | ambiguous → fail closed |
| CE-5 | Matching tool_use_id but wrong session_id or tool_name | fail closed (cross-binding anomaly) |
| CE-6 | Denial delivered before the decision write | `MarkWitnessed` rejects (state machine), delivery not accepted |
| CE-7 | Denial event reaching a C1D (non-resume) runtime pump | no-op, no witness |
| CE-8 | `tool_input` whose canonical form exceeds 32 KiB | malformed → fail closed |
| CE-9 | Duplicate `session_id` or `permission_denials` top-level key | malformed → fail closed |
| CE-10 | Exact evidence-shaped event (20 advisory top-level fields) with the correct input | decoded, witnessed, receipt commits — proves the decoder accepts the REAL wire, which the pre-R6-B decoder (`DisallowUnknownFields` top level) could not |

Fixture provenance: composition/unit fixtures are generated from the
field/type sets frozen in the committed R6-A7 projections (synthetic values,
same shape). Fixtures may not simplify the top level back to
authority-fields-only, so decoder and fixtures can no longer co-evolve
circularly.

## 6. Defects this packet closes (audit of current tree)

1. `strictDenialDecode` uses `DisallowUnknownFields` on the whole event, so
   the real 20-field wire event is rejected and the production deny witness
   can never fire. (Wire-blindness.)
2. `streamDenial` entries have no `tool_input`; `routeDenial` passes
   `ctx.inputDigest`, so the event never proves the digest. (P1 violation in
   current tree, flagged by R6-A2/A3.)
3. `routeDenial` reads `bridge.resumeCtx` under the bridge mutex instead of
   a runtime-owned immutable context. (R6-A3 finding.)
4. The composition deny fixture emits the old minimal shape; it must emit the
   evidence shape and must fail against any decoder that needs the old shape.

## 7. Files and gates

Touched: `internal/term/managed_claude.go` (decoder, `routeDenial`,
`resumeCtx` field), `internal/term/claude_approval_delivery.go` (pass ctx to
the resumed runtime if needed by `ResumeForApproval` signature),
`internal/term/managed_claude_test.go` / `claude_boundary_test.go` (decoder +
binding tests), `cmd/devremote/claude_delivery_composition_test.go`
(evidence-shaped fixtures + negatives). Nothing else.

Gates before review: `gofmt`/`git diff --check`; focused
`go build ./... && go vet ./...`; `go test -race ./internal/term
./cmd/devremote -count=1` plus repeated runs of the Claude delivery suites;
frozen C1D/SP1 regressions (same packages); repository secret scan; frozen
HEAD; push; stop with a review request. No self-acceptance; the verifier
alone decides.
