# PB Legacy Physical Removal Contract

**Status:** EXECUTED — PB deletion complete through PB.6; PB.7 awaiting device gate. PB ACCEPT SHA remains UNSET.

**Branch:** `feature/phase10-multi-adapter`

**PA3 ancestry:** `34d55e950012e97ccdcb03fd9abba88088ffd9a7` (not the PB rollback)

**PA4_PRODUCTION_SHA:** `4aaf3b76a5a4bfab64b041b3be19eaa32d2dd0e2`

**PA4_ACCEPT_SHA / PB_PREREQUISITE_SHA:** `74560edd88ef5b53c3b3d8d215f3007efbf468a2`

**PB_START_BASELINE_SHA:** `abe4df1d6485a8eceafde30e7dfc8da06b8c7f06`

**Future PB ACCEPT SHA:** **UNSET**

## 1. Objective and prerequisite

PB physically removes the isolated legacy runtime stack after PA4 ACCEPT. It
may not start from the PA3 baseline, a partial PA4 wave, or a special
legacy-disabled configuration. The executor must verify both the immutable
`PB_PREREQUISITE_SHA` and the later `PB_START_BASELINE_SHA`. The former proves
authority; the latter is the operational rollback point that retains the PA4
freeze and roadmap documents.

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

The reviewed implementation inventory, baseline comparison matrix, wave gates,
and reproducible commands are authoritative in
[`PB_EXECUTION_PLAN.md`](PB_EXECUTION_PLAN.md).

## 4. Bounded deletion waves

### PB.1 — replaced localpty paths

- Remove already-replaced legacy localpty creation, session, and transport
  paths after zero production consumers are proven.
- Preserve POKIT-owned managed PTY and `TerminalTransport`.

### PB.2a — link and external attach

- Remove manual link and arbitrary attach routes, stores, commands, and mobile
  consumers in ownership order.
- Removed HTTP routes return one contractually frozen status; tests may not
  accept either 404 or 410 interchangeably.

### PB.2b — discovery, resolver, and external observer

- Remove process discovery, `ResolveAgentLog`, external raw JSONL observation,
  and their legacy telemetry projections.
- Preserve provider-runtime-owned Codex/Claude evidence and parsers.
- Remove Gemini resolver unless its accepted production consumer has been
  proven separately.

### PB.3 — tmux

- Remove tmux adapter, commands, routing, tests, configuration, and packaging
  only after static and runtime proof of zero production consumers.

### PB.4 — cmux and recorder compatibility

- Remove cmux adapter paths and recorder sentinel/snapshot compatibility code.
- Preserve canonical Recorder, Transcript, and TerminalTransport behavior.

### PB.5a — managed PTY spawn seam independence

- Move controlled-PTY creation and termination off `mux.Registry` onto a
  narrow platform-neutral PTY launch/session boundary.
- Preserve POKIT-owned Unix PTY transport and the future Windows ConPTY seam;
  do not preserve registry/list/discovery APIs as a platform abstraction.

### PB.5b — dead abstractions and global identity

- Remove dead `mux.Registry`, `Adapter`, and legacy session abstractions only
  after zero production consumers and no managed fallback are proven.
- Remove global ID-addressed recorder lookup after exact-generation owners are
  the only consumers.
- Do not retain a controlled-PTY Registry fallback as a platform boundary.

### PB.6 — mobile cleanup

- Remove obsolete capabilities, adapter unions, DTO fields, feature gates, and
  UI assumptions from mobile after daemon wire absence is proven.
- Preserve the managed Transcript and trimmed telemetry DTO contract.

### PB.7 — full live regression and independent acceptance

- Run all automated gates first, then enter `AWAITING_DEVICE_GATE`.
- Run Android physical-device comparison against the frozen PA4 baseline for
  pairing/auth, managed discovery, lifecycle, approval, transcript/replay,
  reconnect, resize, daemon restart, stale-generation, and cleanup.
- Existing managed-shell empty-terminal, terminal-input authorization, WebView
  keyboard, and restart/recovery defects are recorded comparison debt: an
  unchanged failure may remain deferred, but any new or changed failure is a
  PB regression and blocks acceptance.
- Produce independent PB evidence and an exact PB ACCEPT SHA.

Waves and sub-waves must be implemented and independently reviewed in order.
No wave may replace a negative zero-consumer proof with a compatibility stub.

## 5. Deletion and acceptance gates

For every deletion:

- prove zero production, test, mobile, script, packaging, and documentation
  consumers that still claim current authority;
- migrate tests to canonical production seams; never skip, return early, log
  instead of assert, or inject nil compatibility fixtures;
- run build, vet, full race, focused race, formatting/diff, mobile typecheck,
  mobile unit, and Android build gates proportional to the wave;
- reserve physical-device execution for PB.7, using the PB.0 baseline matrix;
- prove default-configuration behavior and exact local/upstream identity;
- keep implementation and evidence commits separate.

PB exits only after independent read-only acceptance. Its exact ACCEPT SHA is
the prerequisite for Canonical Timeline; it does not authorize later provider
or product phases automatically.
