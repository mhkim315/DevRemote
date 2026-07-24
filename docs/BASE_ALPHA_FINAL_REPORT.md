# POKIT Base Alpha — Final Report

**Status:** BLOCKED REMEDIATION — R0 RECONCILED
**HEAD:** 9e3822de6
**Branch:** feature/canonical-timeline-foundation
**Date:** 2026-07-25

> This report records the state and claims made at `b476d8af7`, reconciled at
> R0 against the repository callers. Independent reconciliation found that the
> tracked daemon/APK were built from `1bdf6b632`, before later
> production/mobile fixes; Claude managed I/O was routed to Codex-only
> endpoints; and the full mobile test suite was not deterministic. Preserve
> this document as historical evidence. Continue only through
> [`BASE_ALPHA_TERMINAL_MANAGED_REMEDIATION_PLAN.md`](BASE_ALPHA_TERMINAL_MANAGED_REMEDIATION_PLAN.md).

---

## 1. Contract Summary

| Phase | Rounds | Result |
|-------|--------|--------|
| Step 9.4 A-D (QR, Keys, Revoke) | ~140 | ✅ ACCEPT |
| Step 9.4-E (Physical Smoke) | ~20 | ✅ ACCEPT |
| Step 9.5 R0-R5 (Build, Artifacts) | ~20 | ✅ ACCEPT |
| Bug Fixes | ~20 | 9/15 fixed, 3 partially-wired, 3 stale-diagnosis |
| **Total** | **~200** | |

---

## 2. 9.4-E Acceptance Criteria

> 깨끗한 Mac과 Android에서 공식 설치·QR 페어링이 성공하고, 재시작 후
> 신뢰가 유지되며, revoke 후 모든 기존 권한과 mutation이 실제로 차단되고,
> 재설치·재페어링이 이전 authority를 부활시키지 않는다.

**Verdict: ACCEPT** — All criteria met on physical device (SM-S926N).

---

## 3. R0 Evidence Reconciliation

### 3.1 Artifact Identity

The Final Report names **production source** `9e3822de6`, but the
`pokit-alpha-device-artifacts/` manifest records:

| Field | Value |
|-------|-------|
| **Source** | `1bdf6b6323e405d67a3634b7ae828416986da367` |
| **Daemon SHA256** | `3b1d027620a6c62b69b38fa35c2d84a4b27d0693bdca004b08c813bd51a67576` |
| **APK SHA256** | `8ed1f37bd816c21513cb1fed08175d018085047feabae07b0a96c0a58d671da1` |
| **Daemon vcs.revision** | `1bdf6b6323e405d67a3634b7ae828416986da367` |
| **Daemon vcs.modified** | `false` |
| **Go** | `1.26.5` |
| **macOS** | `26.5.1 arm64` |
| **Build time (UTC)** | `2026-07-24T14:40:08Z` |

**Source delta** between tested artifact (`1bdf6b632`) and reported HEAD
(`9e3822de6`):

```
9e3822de6 fix: B2 CLI adapter label + B3 ConnectScreen WiFi helper
8295ce5e6 fix: P1 session create + P2 B1 dynamic adapter title
b7fc2f397 docs: 9.4-E bug fix verification report
9e64e1670 fix: QW9 follow-up — degraded segment color in TranscriptRenderer
e9d9bbde1 fix: QR always PNG + QR UX note
256adf401 chore: update artifact manifest for 1bdf6b632 (9/9 fixes verified)
```

The existing physical evidence proves the security lifecycle of `1bdf6b632`.
The four commits after `256adf401` changed production or mobile source and
were NOT included in the tested daemon/APK. Documentation HEAD and evidence
HEAD remain distinct identities.

### 3.2 Bug Classification (R0)

Each bug from the original 12 + 5 verification bugs is classified against the
repository callers at `9e3822de6`.

#### Fixed (9)

| # | Severity | Bug | Fix Commit | Classification |
|---|----------|-----|------------|----------------|
| 1 | P0 | Tunnel + Daemon PATH | `932157c84` | **fixed** — QW1 LaunchAgent PATH |
| 2 | P1 | QR pairing camera stuck | `932157c84` + `d715eb439` | **fixed** — QW2 retry + test |
| 3 | P1 | Claude headless creation | `932157c84` + `1bdf6b632` | **fixed** — QW3 certify + QW4 version SSoT |
| 4 | P2 | Claude version hardcoding (5 loc) | `1bdf6b632` + `76c2a9a87` | **fixed** — QW4 SSoT |
| 6 | P2 | RESCAN button confusion | `76c2a9a87` | **fixed** — QW6 removal |
| 7 | P3 | Transcript font mismatch | `61dc3e03c` | **fixed** — QW7 font sync |
| 9 | P3 | Byte-stream suppressed red banner | `61dc3e03c` + `9e64e1670` | **fixed** — QW9 neutral colors |
| 12 | P3 | Cloudflared duplicate | `61dc3e03c` | **fixed** — QW12 tunnel dedup |
| — | — | B1 ManagedSessionView title "Codex" | `8295ce5e6` | **fixed** — dynamic adapter title |
| — | — | B2 CLI adapter label "Codex" | `9e3822de6` | **fixed** — adapter label |
| — | — | B3 QR no Wi-Fi guidance | `9e3822de6` | **fixed** — ConnectScreen WiFi helper |
| — | — | B4 Claude app HTTP creation | `8295ce5e6` | **fixed** — P1 session create |
| — | — | B5 Back button in headless view | `8295ce5e6` | **fixed** — P2 dynamic title |

#### Partially-Wired (3)

| # | Severity | Bug | Classification |
|---|----------|-----|----------------|
| 5 | P2 | Claude I/O UI missing | **partially-wired** — QW5 routed `claude_headless:*` into `ManagedSessionView` (`FeedScreen.tsx:97`), but the backend prompt and events handlers (`managed_api.go:192,122`) dispatch only through `ManagedCodexService`. `ManagedClaudeService.eventStoreFor` returns `nil, 0, false`. The UI shows "Claude" in the title bar; the session cannot receive prompts or emit events. |

**Evidence for #5:**
- `FeedScreen.tsx:97`: `if (props.session.startsWith('codex_app_server:') \|\| props.session.startsWith('claude_headless:'))` → routes both to `ManagedSessionView` ✅
- `ManagedSessionView.tsx:22`: `if (session.startsWith('claude_headless:')) return 'Claude'` → correct title ✅
- `managed_api.go:219`: `h.Managed.SubmitPrompt(...)` → Codex only; no Claude dispatch ⛔
- `managed_api.go:132`: `h.Managed.Registry().Get(id)` → Codex registry only; Claude sessions fail "not found" ⛔
- `managed_api.go:154`: `h.Managed.eventStoreFor(id)` → Codex event store only ⛔
- `managed_claude.go:1071`: `return nil, 0, false` → no Claude event store ⛔

| # | Severity | Bug | Classification |
|---|----------|-----|----------------|
| — | — | N1 canonical session ID / telemetry / event_source correctness | **partially-wired** — `ManagedRuntimeCatalog` federates both registries (`app.go:587`), `CombinedRuntimeResolver` resolves both providers (`app.go:564`), and the catalog `List()` returns merged results (`managed_api.go:96`). But `HandleManagedSessionEvents` and `HandleManagedSessionPrompt` do not dispatch by adapter prefix — they hardcode `h.Managed` (Codex). Claude sessions appearing in the merged session list will return 404 on events and prompt. |
| — | — | Transcript event ingestion for managed Claude | **partially-wired** — `ManagedClaudeService.processLine` recognizes stream observations, approval joins, and denial witnesses, but does not append assistant content to a managed event store. No general Claude equivalent of the Codex one-active-turn prompt contract exists. |

#### Stale-Diagnosis (3)

| # | Severity | Bug | Original Diagnosis | R0 Finding |
|---|----------|-----|--------------------|------------|
| 8 | P3 | Terminal input overlap | WebRTC/WebSocket 이중 경로 구조 변경 필요 | **stale-diagnosis** — Terminal TextInput, macros, paste, and xterm keyboard already converge on the served page's acknowledged WebSocket input protocol (`Recorder` as sole PTY reader). The remaining defect is overlapping input surfaces and operation ownership, not a production WebRTC/WebSocket split. The unimported `mobile/src/screens/terminalHtml.ts` prototype is not production authority. |
| 10 | P3 | Transcript/Terminal desync | Source Separation 설계 변경 필요 | **stale-diagnosis** — The suppressed-byte-stream-after-input behavior is by design (`arbitration.go:85`). Empty Transcript after input is a known, intended state, not a desync defect. See Remediation Plan §2.3: "An empty managed Transcript is therefore not proof of an idle agent." The UI must distinguish the Transcript availability states enumerated in R3. |
| 11 | P3 | claude_headless delete lifecycle | Lifecycle.Register 미호출 + Stop/Delete API 미처리 | **stale-diagnosis** — Claude Stop, Kill, and Delete already exist and are routed by the shared lifecycle dispatcher. `LifecycleService.ownerFor` (`lifecycle_service.go:122-123`) dispatches `claude_headless:*` to `ManagedClaudeService` via `ProviderLifecycleOwner`. Separate endpoints (`/api/managed-claude-sessions/{id}/stop\|kill`, `DELETE /api/managed-claude-sessions/{id}`) exist. The remaining work is end-to-end contract and device verification, not adding missing lifecycle methods. |

### 3.3 Claude I/O: Not Implemented

Claude managed I/O (prompt delivery and structured event emission) is **not
implemented** at the planning baseline. The QW5 change was a UI routing fix
only — it changed the title label and view component, not the I/O path.

Specifically not present:
- No Claude prompt submission path (REST or IPC).
- No Claude managed event store (`eventStoreFor` returns `nil, 0, false`).
- No Claude equivalent of the Codex one-active-turn prompt contract.
- `HandleManagedSessionEvents` and `HandleManagedSessionPrompt` dispatch through `h.Managed` (Codex) only — no adapter-prefix routing.

### 3.4 Claude Lifecycle: Present

Claude lifecycle **is present** and correctly routed:

- `ManagedClaudeService.Stop` / `Kill` / `Delete` → implemented.
- `ManagedClaudeService` adapted via `NewManagedProviderOwner` → `ProviderLifecycleOwner`.
- `LifecycleService.ownerFor` (`lifecycle_service.go:122-123`) dispatches `claude_headless:*` to the Claude owner.
- Separate REST endpoints: `POST /api/managed-claude-sessions/{id}/stop`, `POST /api/managed-claude-sessions/{id}/kill`, `DELETE /api/managed-claude-sessions/{id}`.
- The original bug #11 diagnosis ("Stop/Delete API가 claude_headless 어댑터를 처리하지 못함") was incorrect at the time of the report but is now superseded by the implemented lifecycle dispatcher.

### 3.5 Mobile Test Order-Dependence

At the planning baseline (`b476d8af7`):

- Full Jest run: **547/552 PASS** (5 failures).
- The two failing iOS pairing suites (`pairingStore.test.ts`, `authPairing.test.ts`): **23/23 PASS** when run together in isolation.

Root cause: `pairingStore.test.ts` uses `jest.mock('@react-native-async-storage/async-storage', ...)` with a module-scoped `Map` and a `__reset()` helper. Other test suites that mock the same module (or import it transitively) can observe the mutated store. When Jest runs in default parallel mode without `--runInBand`, mock state leaks across suites.

The final report's claim "36/36 suites, 552/552" was recorded with `--runInBand` which serializes execution and masks the isolation defect. A deterministic gate must pass with randomized suite order.

---

## 4. Verified Gates

| Gate | Result |
|------|--------|
| `go build ./...` | ✅ |
| `go vet ./...` | ✅ |
| `go test -race ./... -count=1` | ✅ 19/19 |
| `npx tsc --noEmit` | ✅ |
| `npm test -- --runInBand` | ✅ 552/552 (order-masked) |
| `git diff --check` | ✅ |
| Secret scan | ✅ |

---

## 5. Deferred Items (Post-Alpha → Remediation Plan)

These items are re-scoped by the Remediation Plan (R0–R8):

| # | Item | Remediation |
|---|------|-------------|
| **#5** | Claude I/O | R5 (managed Transcript projection) + R6 (managed input) |
| **#8** | Terminal input overlap | R7 (input UX unification) |
| **#10** | Transcript availability UX | R3 (Transcript availability states) |
| **#11** | Claude lifecycle verification | R6 (lifecycle closeout + physical verification) |
| — | Jest order-dependence | R7 (deterministic test gate) |
| — | Matched candidate | R8 (one exact production source + physical gate) |
| — | N1 event routing | R1 (runtime mode + provider-bound contract freeze) |

---

## 6. Acceptance Authority

This document records the R0 evidence reconciliation. No implementation
changes were made. The `DOGFOOD READY` claim is withdrawn and replaced with
`BLOCKED REMEDIATION`. Base Alpha acceptance requires completion of R1–R8
and a matched candidate with one exact production source SHA, daemon, APK,
installation, and evidence chain.

**Historical evidence** from `1bdf6b632` remains immutable and correctly
scoped to the exact artifacts that produced it. No existing test artifacts
were modified in this reconciliation.

---

**Report:** `docs/BASE_ALPHA_FINAL_REPORT.md`
**Reconciled:** R0 — 2026-07-25
