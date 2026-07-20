# Post-PA3 Authoritative Roadmap

**Status:** AUTHORITATIVE POST-PA3 EXECUTION ORDER — PA4 COMPLETE, PB PENDING (gated on human authorization)

**Branch:** `feature/phase10-multi-adapter`

**Frozen production baseline:** `34d55e950012e97ccdcb03fd9abba88088ffd9a7` (PA3 ACCEPT)

**Documentation input HEAD:** `b8884b5934d3a89b132f887de6339b976e01c236`

## 1. SHA and phase ledger

These identities have different meanings and must not be substituted for one another.

| Identity | Exact SHA | Meaning |
|---|---|---|
| PA2 final implementation | `b93c521b7de45f3f577805dbb7de505e16f3172d` | Accepted PA2 implementation ancestry |
| PA3 frozen production baseline | `34d55e950012e97ccdcb03fd9abba88088ffd9a7` | Last accepted production baseline before PA4 |
| Current documentation input HEAD | `b8884b5934d3a89b132f887de6339b976e01c236` | Documentation state reconciled by this plan |
| PA4 ACCEPT | `74560edd88ef5b53c3b3d8d215f3007efbf468a2` | Independently verified and frozen |
| PB prerequisite and rollback | `74560edd88ef5b53c3b3d8d215f3007efbf468a2` | Set at PA4 ACCEPT freeze |
| Future PB ACCEPT | **UNSET** | Assigned only by an independent PB verifier |

| Packet | Status |
|---|---|
| PF | ACCEPT |
| PA0 | ACCEPT (inventory/rationale) |
| PA1 | ACCEPT |
| PA2 | ACCEPT |
| PA3 | ACCEPT at `34d55e950012e97ccdcb03fd9abba88088ffd9a7` |
| PA4 | READY FOR INDEPENDENT CONTRACT REVIEW; not frozen or authorized for implementation |
| PB | BLOCKED pending an exact independently accepted PA4 SHA |

## 2. Mandatory execution order

```text
PA3 COMPLETE
→ PA4 managed-path isolation
→ independent PA4 ACCEPT
→ PB legacy physical removal
→ independent PB ACCEPT
→ Canonical Timeline
→ Codex/Claude common provider contract
→ Grok/ACP conformance
→ Navigator/Guard
→ durable orchestration and product hardening
```

No later packet may be pulled forward to justify a shortcut in PA4 or PB. PB
cannot start from the PA3 SHA, and Canonical Timeline cannot preserve legacy
discovery while PA4/PB remain incomplete.

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

PB is physical removal after PA4 ACCEPT. Its prerequisite and rollback SHA is
the future exact PA4 ACCEPT SHA, never the PA3 baseline. The normative contract
is [`PB_LEGACY_REMOVAL_CONTRACT.md`](PB_LEGACY_REMOVAL_CONTRACT.md).

## 5. Post-PB direction

Canonical Timeline follows PB acceptance. `AcceptedRecordSource` is not
mandated: it may be introduced only if multiple surviving provider-native
sources demonstrably need a narrow common interface, and it must never preserve
legacy discovery. The common Codex/Claude provider contract follows Timeline;
Grok/ACP conformance, Navigator/Guard, then durable orchestration and product
hardening follow in that order.

## 6. Document authority

| Document | Classification after this reconciliation |
|---|---|
| This document | **Authoritative execution order and status ledger** |
| `docs/PA4_MANAGED_ISOLATION_CONTRACT.md` | **Authoritative PA4 review candidate**; frozen only after independent acceptance |
| `docs/PB_LEGACY_REMOVAL_CONTRACT.md` | **Authoritative PB boundary**, blocked until PA4 ACCEPT |
| `docs/POST_CLAUDE_MANAGED_ONLY_RESTRUCTURING_PLAN.md` | Historical rationale only; post-PA3 sequence superseded |
| `docs/ROADMAP_AFTER_E10B.md` | Historical product roadmap; post-PA3 status/order superseded |
| `docs/PA0_LEGACY_CONSUMER_INVENTORY_CONTRACT.md` | Accepted historical inventory input, not current execution authority |
| PA1, PA2, and PA3 frozen contracts/evidence | Frozen accepted inputs; not editable by this planning packet |
| `companion-daemon/docs/PB_PA4_CONTRACT.md` | Superseded combined draft; historical inventory only |
| `companion-daemon/docs/PB_PA4_CONTRACT_EVIDENCE.md` | Historical evidence for the superseded draft |
| Older README, handover, adapter-layer, or implementation plans | Historical rationale unless explicitly re-authorized here |

Future agents must not reconstruct a tmux/cmux/localpty-centered product
direction from superseded documents. When language conflicts, the three
post-PA3 documents named above control.

## 7. Stop conditions

- Stop PA4 if a managed request can reach `mux.Registry` or observer evidence.
- Stop PB if the exact PA4 ACCEPT SHA is unavailable or is not its baseline.
- Stop either packet if PA3 generation/lifecycle/transport invariants change.
- Stop rather than retaining Gemini or legacy discovery as an “accepted
  adapter” without a separately accepted production consumer.
