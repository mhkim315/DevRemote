# A1.1 CP0 — Evidence Report 2 (IN PROGRESS; not a completion claim)

Status: **CP0 IN PROGRESS — ordered lifecycle allow/deny/resolved PROVEN; process-image
gate BLOCKED via fd-exec on macOS (correct mechanism identified); #6/#7 remain. A1.1 /
A1 / N1 remain BLOCKED. Capacity stays ZERO.**

Date: 2026-07-14. Executor: Claude Code. Authorized scope: CP0 only. Baseline plan
`4e3357d`; harness `scripts/cp0/appserver_probe.py`. Aligned contract note
`docs/A1_1_CP0_CONTRACT_NOTE.md`.

Redacted evidence in `docs/a1_1_cp0_evidence/`: `wire_accept.jsonl`,
`wire_decline.jsonl` (ordered traces), `launch_chain.json`, `attest.json`,
`schema_bundle.manifest`(+`.sha256`), plus the earlier `init_handshake.redacted.jsonl`
and `certification_identity.txt`.

## Gate-by-gate status (verdict §5)

| # | Gate | Status | Evidence |
| --- | --- | --- | --- |
| 1 | actual process-image identity + deterministic adversarial replacement | **BLOCKED via fd-exec on macOS; correct mechanism identified** | `attest.json`: macOS has no `fexecve` (compile fails) and `/dev/fd` exec → `EACCES`; path-exec runs the replaced bytes (TOCTOU real). Correct macOS mechanism = code-signing cdhash attestation (codex native is signed: `Identifier=codex`, `TeamIdentifier=2DC432GLL2`, hardened runtime) — follow-up spike |
| 2 | ordered item/request/write/resolved/outcome | **PROVEN** | `wire_accept.jsonl` seq 23 `item/started` → 24 `item/commandExecution/requestApproval` → 25 our `result` (daemon→provider) → 26 `serverRequest/resolved` → 28 `item/completed`, monotonic with direction |
| 3 | matching `item/started` + request identity | **PROVEN** | 4 `item/started` per trace; request carries outer int id + threadId/turnId/itemId |
| 4 | real allow-once and deny response shapes | **PROVEN** | accept + decline both `{"id":N,"result":{"decision":...}}`; both resolved |
| 5 | `serverRequest/resolved` ordering + consumption meaning | **PROVEN** | resolved (seq 26) strictly after our write (seq 25), matching requestId; corroboration: accept → probe file created, decline → NOT created |
| 6 | cancellation, duplicate-response, timeout, child/orphan cleanup | **NOT DONE** | pending |
| 7 | bounded real PRODUCTION thread/turn entry path | **NOT PROVEN** | harness drives the protocol directly; a defensible bounded POKIT production entry is still a design question |
| — | per-file schema manifest | **DONE (with a reproducibility finding)** | `schema_bundle.manifest` has sorted per-relative-path digests |
| — | launch-chain identity | **PARTIAL** | `launch_chain.json`: node (v20.20.2, digest) + shim (sha256 134063e1) recorded; spawned image resolves to `node` (shim is exec'd by node), so the vendored-native running image is not yet attested |

## Key findings (some contradict/extend prior assumptions)

1. **macOS cannot bind verified==spawned via an open fd.** No `fexecve`; `/dev/fd` exec
   is `EACCES`. The naive TOCTOU mitigation is impossible on macOS. The codex native is
   code-signed, so the `ProcessImageAttestor` must use **cdhash attestation** (bind cert
   to the code-signing cdhash + TeamIdentifier `2DC432GLL2`; verify the running process
   cdhash via `csops`). A tampered binary changes the cdhash / is rejected by the
   hardened runtime. This is the correct macOS mechanism; a full adversarial demonstration
   is a follow-up CP0/CP1 spike.
2. **Launch chain is node → codex.js shim → vendored native.** `ps` reports the spawned
   image as `node`; attesting the actual native running image needs cdhash/csops on the
   native child, not `ps`. This reinforces (1).
3. **Schema-bundle reproducibility is not byte-stable.** Regenerating the non-experimental
   267-file bundle on the same installed `0.144.1` produced manifest digest
   `952e43f8…` vs the earlier `cafb9881…`. Same binary + same `--without --experimental`;
   the difference must be characterized (module-resolution/cache state) before a manifest
   digest can be a certification input. Committed manifest reflects the current run.
4. **environmentId="local"** and the field-freeze all reconfirmed live (see the wire
   traces and the amended plan §6).

## Remaining CP0 work (still CP0; no CP1; capacity zero)

1. macOS cdhash `ProcessImageAttestor` spike + deterministic adversarial demonstration
   (tamper → cdhash mismatch / hardened-runtime rejection) — or gate #1 stays BLOCKED.
2. cancellation / duplicate-response / timeout / child-orphan cleanup traces (#6).
3. bounded production thread/turn entry-path definition + justification (#7) — or BLOCKED.
4. characterize + resolve the schema-manifest reproducibility discrepancy.

A1.1, A1 and N1 remain BLOCKED. `provenActionMapping` empty; production delivery
capacity zero. Stopping at the CP0 boundary for review.
