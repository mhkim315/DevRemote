# PB Legacy Physical Removal Contract

**Status:** BLOCKED — requires exact independent PA4 ACCEPT SHA

**Branch:** `feature/phase10-multi-adapter`

**PA3 ancestry:** `34d55e950012e97ccdcb03fd9abba88088ffd9a7` (not the PB rollback)

**PB prerequisite / rollback SHA:** **UNSET; must equal the future PA4 ACCEPT SHA**

**Future PB ACCEPT SHA:** **UNSET**

## 1. Objective and prerequisite

PB physically removes the isolated legacy runtime stack after PA4 ACCEPT. It
may not start from the PA3 baseline, a partial PA4 wave, or a special
legacy-disabled configuration. The executor must first record and verify the
exact independent PA4 ACCEPT SHA and use it as PB's rollback baseline.

## 2. Boundaries that remain

PB must not delete or weaken:

- POKIT-owned managed PTY creation and lifecycle;
- `Recorder` as the sole PTY reader;
- exact-generation `TerminalTransport` used by WS, IPC, input, resize, replay,
  and terminal lookup;
- the platform-neutral terminal transport boundary needed for Unix PTY and
  future Windows ConPTY;
- `ManagedRuntimeCatalog`, managed provider runtimes, `OwnedPTYRuntime`, and
  accepted PA3 generation/lifecycle cleanup mechanisms;
- managed Codex/Claude provider-native status and approval evidence.

## 3. Evidence-source classification

| Source | Classification and disposition |
|---|---|
| Managed Codex/Claude provider-native evidence | Retained managed authority |
| Legacy-session `ResolveAgentLog` | Remove with its actual legacy consumers in PA4/PB |
| Process discovery | Legacy discovery; remove after isolated consumers are gone |
| External raw JSONL observation | Legacy observer path; remove, never use as managed authority |
| Manual link and arbitrary attach | Legacy routes; remove in PB.2 |
| Gemini resolver | Not an accepted adapter by default; retain only if a real independently accepted production consumer is proven |

`AcceptedRecordSource` is not mandatory. Canonical Timeline work may introduce
it only if multiple surviving provider-native sources demonstrably require a
narrow common interface. It must never wrap or preserve legacy discovery.

## 4. Bounded deletion waves

### PB.1 — replaced localpty paths

- Remove already-replaced legacy localpty creation, session, and transport
  paths after zero production consumers are proven.
- Preserve POKIT-owned managed PTY and `TerminalTransport`.

### PB.2 — link, discovery, and external attach

- Remove manual link, process discovery, `ResolveAgentLog`, external raw JSONL
  observation, and arbitrary attach routes in consumer ownership order.
- Remove Gemini resolver unless its accepted production consumer has been
  proven separately.

### PB.3 — tmux

- Remove tmux adapter, commands, routing, tests, configuration, and packaging
  only after static and runtime proof of zero production consumers.

### PB.4 — cmux and recorder compatibility

- Remove cmux adapter paths and recorder sentinel/snapshot compatibility code.
- Preserve canonical Recorder, Transcript, and TerminalTransport behavior.

### PB.5 — dead abstractions

- Remove dead `mux.Registry`, `Adapter`, and legacy session abstractions only
  after zero production consumers and no managed fallback are proven.
- Do not retain a controlled-PTY Registry fallback as a platform boundary.

### PB.6 — mobile cleanup

- Remove obsolete capabilities, adapter unions, DTO fields, feature gates, and
  UI assumptions from mobile after daemon wire absence is proven.
- Preserve the managed Transcript and trimmed telemetry DTO contract.

### PB.7 — full live regression and independent acceptance

- Run Codex/Claude lifecycle, approval, transcript/replay, reconnect, resize,
  mobile, daemon restart, stale-generation, and cleanup regression gates.
- Produce independent PB evidence and an exact PB ACCEPT SHA.

Waves must be implemented and reviewed in order. No wave may replace a
negative zero-consumer proof with a compatibility stub.

## 5. Deletion and acceptance gates

For every deletion:

- prove zero production, test, mobile, script, packaging, and documentation
  consumers that still claim current authority;
- migrate tests to canonical production seams; never skip, return early, log
  instead of assert, or inject nil compatibility fixtures;
- run build, vet, full race, focused race, formatting/diff, mobile typecheck,
  mobile unit, Android, and live runtime gates proportional to the wave;
- prove default-configuration behavior and exact local/upstream identity;
- keep implementation and evidence commits separate.

PB exits only after independent read-only acceptance. Its exact ACCEPT SHA is
the prerequisite for Canonical Timeline; it does not authorize later provider
or product phases automatically.
