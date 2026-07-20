# PA4 Managed-Path Isolation Contract

**Status:** IMPLEMENTATION COMPLETE — awaiting independent verification (not yet frozen)

**Branch:** `feature/phase10-multi-adapter`

**Frozen PA3 production baseline / PA4 rollback:** `34d55e950012e97ccdcb03fd9abba88088ffd9a7`

**PA4 implementation HEAD:** `f8ad56028` (pre-R16), R16 in progress

**Future PA4 ACCEPT SHA:** **UNSET** (set by independent verifier)

## 1. Objective

PA4 is structural isolation of managed production paths from legacy adapter
authority. It does not physically delete the legacy stack; that is PB. All
requirements below must hold under the normal production default configuration,
not merely behind a legacy-disabled test flag.

## 2. Required authority boundaries

### 2.1 Catalog and semantic state

- Managed list, get, and status authority uses `ManagedRuntimeCatalog`.
- Managed semantic status and approval come from accepted provider-native
  evidence and must not depend on screen parsing, PTY-text inference, process
  discovery, or legacy raw JSONL observation.
- Managed Codex/Claude provider-native evidence remains supported.
- `ResolveAgentLog`, process discovery, external raw JSONL observation, manual
  link, and arbitrary attach are legacy-session facilities and cannot become
  managed fallback authority.

### 2.2 Lifecycle

- Managed provider runtimes and `OwnedPTYRuntime` remain lifecycle authority.
- Creation, replacement, cleanup, and retirement operate on captured exact
  identities and generations.
- Approval lookup resolves exact managed provider/session authority; it cannot
  consult screen text, process discovery, or a legacy registry row.

### 2.3 Terminal transport

- WS, IPC, input, resize, replay, and terminal lookup use exact-generation
  `TerminalTransport`.
- Managed paths never fall back to `mux.Registry`.
- `Recorder` remains the sole PTY reader; transcript and replay remain bounded
  by the accepted generation lease.
- POKIT-owned managed PTY transport and the platform-neutral transport boundary
  for Unix PTY and future Windows ConPTY are explicitly retained.

### 2.4 Frozen PA3 mechanisms

PA4 must not redesign `CreateSessionAndCapture`,
`GenerationCleanupCapability`, `GenerationCompletion`, recorder instance
capture, transport generation identity, compare-and-terminate,
`RetireIfGeneration`, transcript generation, or stale-generation rejection.

## 3. Forbidden shortcuts

- No managed-to-legacy `mux.Registry` lookup or fallback.
- No temporary comparison facade on a managed production path at PA4 exit.
- No special configuration flag as the only proof of isolation.
- No legacy discovery renamed as provider-native evidence.
- No `AcceptedRecordSource` introduced merely to retain legacy discovery.
- No physical legacy deletion before independent PA4 acceptance.

Gemini resolver is not an accepted adapter exception. It may survive PA4 only
when a real, separately accepted production consumer is proven; otherwise its
consumer-owned removal belongs in PA4/PB.

## 4. Bounded implementation waves

### PA4.1 — managed REST/list/get/status isolation

- Route managed list/get/status solely through `ManagedRuntimeCatalog`.
- Remove managed comparison/fallback reads from `mux.Registry`.
- Prove legacy-only rows cannot alter a managed response.

### PA4.2 — lifecycle and approval lookup isolation

- Route managed lifecycle through provider runtimes and `OwnedPTYRuntime`.
- Resolve approval through exact provider/session identity.
- Prove discovery, screen, and raw-log evidence cannot satisfy managed status
  or approval decisions.

### PA4.3 — WS/IPC/input/resize/replay isolation

- Resolve every managed terminal operation through exact-generation
  `TerminalTransport`.
- Remove Registry/session fallback and prove stale generations are rejected.
- Retain platform-neutral Unix PTY/future ConPTY transport boundaries.

### PA4.4 — observer containment

- Prevent observer telemetry, screen parsing, raw JSONL readers, and process
  discovery from reaching managed catalog, status, approval, or lifecycle.
- Keep any still-live legacy observer strictly within an explicitly legacy
  route pending PB removal.

### PA4.5 — facade/fallback deletion and live acceptance

- Delete temporary comparison facades and managed-to-legacy fallbacks.
- Run live Codex, Claude, and mobile acceptance under default configuration.
- Produce implementation evidence followed by independent verification and an
  exact PA4 ACCEPT SHA.

Each wave is independently reviewable and may not pull PB deletion into scope.

## 5. Required proofs and gates

Tests must include positive managed catalog/lifecycle/terminal behavior and
negative controls proving legacy registry rows, discovery results, screen text,
raw JSONL, stale generations, and absent legacy-disable flags cannot affect it.
Skipped, nil-filled, DTO-injected, or fake-route proofs are not acceptance.

Minimum gates from repository root:

```sh
cd companion-daemon && go build ./...
cd companion-daemon && go vet ./...
cd companion-daemon && go test -race ./... -count=1
cd mobile && npx tsc --noEmit
```

Also run focused race tests for each touched boundary and existing mobile
unit/Android gates when those surfaces are touched. Diff checks must prove no
unauthorized PA3 mechanism change.

## 6. Exit and PB handoff

PA4 exits only after an independent read-only verifier accepts the complete
default-configuration boundary and records an exact SHA. That exact PA4 ACCEPT
SHA becomes both the PB prerequisite and PB rollback baseline. Until then PB is
BLOCKED.
