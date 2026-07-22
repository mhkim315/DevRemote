# Post-PA3 Authoritative Roadmap

**Status:** AUTHORITATIVE PA/PB IDENTITY LEDGER — product and post-PB execution
direction moved to `POKIT_NATIVE_SESSION_COORDINATION_ROADMAP.md`

> **Authority notice:** This document retains accepted PA/PB identities and
> frozen safety boundaries. The current product definition, post-PB order, MVP,
> coordination/workspace model, and CT-P1 transition are authoritative in
> [`POKIT_NATIVE_SESSION_COORDINATION_ROADMAP.md`](POKIT_NATIVE_SESSION_COORDINATION_ROADMAP.md).
> If the older post-PB sequence below conflicts with that document, the new
> roadmap wins. PB physical evidence remains pending and PB ACCEPT remains
> **UNSET**.

**Branch:** `feature/phase10-multi-adapter`

**Frozen PA3 production baseline:** `34d55e950012e97ccdcb03fd9abba88088ffd9a7`

**Original plan-reconciliation input:** `b8884b5934d3a89b132f887de6339b976e01c236`

## 1. SHA and phase ledger

These identities have different meanings and must not be substituted for one another.

| Identity | Exact SHA | Meaning |
|---|---|---|
| PA2 final implementation | `b93c521b7de45f3f577805dbb7de505e16f3172d` | Accepted PA2 implementation ancestry |
| PA3 frozen production baseline | `34d55e950012e97ccdcb03fd9abba88088ffd9a7` | Last accepted production baseline before PA4 |
| PA4 production implementation | `4aaf3b76a5a4bfab64b041b3be19eaa32d2dd0e2` | Frozen managed-isolation production tree |
| PA4 ACCEPT | `74560edd88ef5b53c3b3d8d215f3007efbf468a2` | Independently verified and frozen |
| PB prerequisite | `74560edd88ef5b53c3b3d8d215f3007efbf468a2` | Contract authority required before PB may start |
| PB start baseline | `abe4df1d6485a8eceafde30e7dfc8da06b8c7f06` | Operational rollback point that preserves the frozen PA4/roadmap documents |
| Current non-device checkpoint | `f052e7f8a60ae0ece8b7a5535045f59c55b8e3c4` | Automated PB checkpoint before the frozen QR/Input remediation sequence |
| Planned PB device candidate alias | `ab1884662` | Historical short form of the exact frozen candidate recorded below |
| Future PB automated evidence | `b18123e77` | Evidence-only head after PB.6/PB.7 regeneration |
| Future PB ACCEPT | **UNSET** | Assigned only by an independent PB verifier |
| Frozen PB device candidate | `ab18846622327334300de8436aff600db5c08e17` | Immutable production/mobile source for the pending physical matrix |
| Frozen daemon artifact | `5c1470a0d58072564298572aa8184a9c9565a8f57452eba56e2ebda56b10e580` | SHA-256; matched to the frozen candidate |
| Frozen APK artifact | `934febb17b8de175816413a1aaa1a17b8d1e8f43c2cbfcb4fe26e20fce433c7d` | SHA-256; matched to the frozen candidate |

| Packet | Status |
|---|---|
| PF | ACCEPT |
| PA0 | ACCEPT (inventory/rationale) |
| PA1 | ACCEPT |
| PA2 | ACCEPT |
| PA3 | ACCEPT at `34d55e950012e97ccdcb03fd9abba88088ffd9a7` |
| PA4 | ACCEPT at `74560edd88ef5b53c3b3d8d215f3007efbf468a2` |
| PB | AUTOMATED LEGACY REMOVAL COMPLETE; pre-device remediation planned; device gate not started; ACCEPT unset |
| CT-PRE | CT-P0 and CT-P1 ACCEPT (`317bb0cb76a73bd49bebaaede562bfa77cd1e7bc`); post-PB operational amendment pending; production composition forbidden |

## 2. Mandatory execution order

```text
PA3 COMPLETE
→ PA4 managed-path isolation
→ independent PA4 ACCEPT
→ PB legacy physical removal
→ automated PB closeout checkpoint
→ QR renderer/security remediation and independent ACCEPT
→ Input-A permission/read-only UX and independent ACCEPT
→ Input-B exact-generation acknowledged input and independent ACCEPT
→ PB.6/PB.7 evidence regeneration
→ exact device-candidate freeze and matched daemon/APK builds
→ bounded offline CT-P0/CT-P1 candidate work only
→ SM-S926N device matrix using the exact frozen artifacts
→ independent PB ACCEPT
→ CT-P1 operational-evidence amendment and independent ACCEPT
→ fail-open Operational Canonical Timeline shadow
→ workspace identity and cooperative write lease
→ manual coordination and frozen-snapshot validation
→ mobile operational cockpit and alpha/beta
```

No later packet may be pulled forward to justify a shortcut in PA4 or PB. PB
cannot start from the PA3 SHA. The sole additional exception before PB ACCEPT is
the offline, zero-production-composition CT-PRE foundation governed by
[`CANONICAL_TIMELINE_CT_PRE_EXECUTION_PLAN.md`](CANONICAL_TIMELINE_CT_PRE_EXECUTION_PLAN.md).
It cannot preserve or revive legacy discovery, change the frozen device
candidate, write production data, or authorize production shadow-write/cutover.

## 3. Frozen authority model

PA4 and PB must preserve the accepted PA3 mechanisms:

- `ManagedRuntimeCatalog` owns managed list/get/status projection.
- Managed provider runtimes and `OwnedPTYRuntime` own managed lifecycle.
- Exact-generation `TerminalTransport` owns WS, IPC, input, resize, replay, and
  terminal lookup; `Recorder` remains the sole PTY reader.
- Provider-native Codex/Claude semantic status and approval evidence remains.
- Generation capture, cleanup capability/completion, compare-and-terminate,
  transcript generation, and stale-generation rejection remain unchanged.
- POKIT-owned managed PTY transport and the platform-neutral terminal boundary
  remain for Unix PTY and future Windows ConPTY.

## 4. PA4 and PB boundaries

PA4 structurally isolates every managed production path from legacy adapter
authority under the normal production default configuration. Managed paths may
not fall back to `mux.Registry`, legacy screen parsing, PTY-text inference,
process discovery, or external raw JSONL observation. The normative contract is
[`PA4_MANAGED_ISOLATION_CONTRACT.md`](PA4_MANAGED_ISOLATION_CONTRACT.md).

PB is physical removal after PA4 ACCEPT. Its contract prerequisite is the exact
PA4 ACCEPT SHA. Its operational rollback is separately recorded as the exact
PB start baseline after the PA4 freeze documents exist, so recovery does not
discard the accepted authority ledger. The normative contract is
[`PB_LEGACY_REMOVAL_CONTRACT.md`](PB_LEGACY_REMOVAL_CONTRACT.md).

The bounded pre-device exception is governed exclusively by
[`PRE_DEVICE_QR_INPUT_REMEDIATION_PLAN.md`](PRE_DEVICE_QR_INPUT_REMEDIATION_PLAN.md).
It is required because usable pairing and truthful input are prerequisites for
the physical gate; it does not reopen PB legacy-removal scope.

The bounded offline Canonical Timeline foundation exception is governed by
[`CANONICAL_TIMELINE_CT_PRE_EXECUTION_PLAN.md`](CANONICAL_TIMELINE_CT_PRE_EXECUTION_PLAN.md).
CT-P0 and CT-P1 are accepted; the CT-P1 operational-evidence amendment, CT-P2,
and later CT waves are not currently authorized. Production
shadow-write and every authority/UI cutover remain blocked until an exact PB
ACCEPT SHA and independent acceptance of the revised operational CT-P1 exist.

## 5. Post-PB direction

The QR renderer/security and exact-generation input packets defined by the
pre-device plan and bounded zero-composition CT-P1 candidate work are the only
approved exceptions before PB acceptance. Remaining terminal/restart debt and
remote-pairing hardening stay in separate reviewed packets. Product and
architecture direction after PB is exclusively defined by
[`POKIT_NATIVE_SESSION_COORDINATION_ROADMAP.md`](POKIT_NATIVE_SESSION_COORDINATION_ROADMAP.md).
Grok/ACP, Navigator/Guard, generic adapters, and broad orchestration are not an
automatic continuation of the accepted PA/PB work.

## 6. Document authority

| Document | Classification after this reconciliation |
|---|---|
| This document | **Authoritative accepted PA/PB identity and safety ledger** |
| `docs/POKIT_NATIVE_SESSION_COORDINATION_ROADMAP.md` | **Authoritative product definition, authority model, MVP, and post-PB order** |
| `docs/PRE_DEVICE_QR_INPUT_REMEDIATION_PLAN.md` | **Authoritative pre-device QR/Input packet contract and device-entry gate** |
| `docs/CANONICAL_TIMELINE_CT_PRE_EXECUTION_PLAN.md` | **Authoritative offline CT-PRE contract; production composition/cutover blocked pending PB ACCEPT** |
| `docs/PA4_MANAGED_ISOLATION_CONTRACT.md` | **Frozen authoritative PA4 contract** at the independently accepted SHA |
| `docs/PB_LEGACY_REMOVAL_CONTRACT.md` | **Authoritative PB boundary**; implementation starts only after independent PB plan review |
| `docs/POST_CLAUDE_MANAGED_ONLY_RESTRUCTURING_PLAN.md` | Historical rationale only; post-PA3 sequence superseded |
| `docs/ROADMAP_AFTER_E10B.md` | Historical product roadmap; post-PA3 status/order superseded |
| `docs/PA0_LEGACY_CONSUMER_INVENTORY_CONTRACT.md` | Accepted historical inventory input, not current execution authority |
| PA1, PA2, and PA3 frozen contracts/evidence | Frozen accepted inputs; not editable by this planning packet |
| `companion-daemon/docs/PB_PA4_CONTRACT.md` | Superseded combined draft; historical inventory only |
| `companion-daemon/docs/PB_PA4_CONTRACT_EVIDENCE.md` | Historical evidence for the superseded draft |
| Older README, handover, adapter-layer, or implementation plans | Historical rationale unless explicitly re-authorized here |

Future agents must not reconstruct a tmux/cmux/localpty-centered product or a
generic orchestration harness from superseded documents. Product and post-PB
ordering conflicts are resolved by
`POKIT_NATIVE_SESSION_COORDINATION_ROADMAP.md`; frozen PA/PB identities and
safety boundaries remain controlled by this ledger and its accepted contracts.

## 7. Stop conditions

- Stop PA4 if a managed request can reach `mux.Registry` or observer evidence.
- Stop PB if the exact PA4 prerequisite or the separately recorded PB start baseline is unavailable or not in its ancestry.
- Stop either packet if PA3 generation/lifecycle/transport invariants change.
- Stop the device gate until QR, Input-A, and Input-B have independent ACCEPT
  identities, regenerated PB.6/PB.7 evidence is clean, and daemon/APK identities
  equal the frozen production candidate.
- Do not begin CT-P2. Keep CT-P1 production-unwired until PB has an exact
  independent ACCEPT SHA and the operational-evidence amendment is independently
  accepted. Any premature production import,
  construction, goroutine, filesystem write, route, DTO, mobile dependency, or
  authority change is an immediate CT-PRE reject.
- Stop rather than retaining Gemini or legacy discovery as an “accepted
  adapter” without a separately accepted production consumer.
