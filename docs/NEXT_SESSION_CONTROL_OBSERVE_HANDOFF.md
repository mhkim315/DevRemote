# Next Session Handoff — Adapter Classification and Controlled PTY Runtime

Read first:

- `docs/ADAPTER_CLASSIFICATION_AND_PTY_RUNTIME_PLAN.md`
- `docs/E8G4_CMUX_TERMINAL_DUP_DIAGNOSIS.md`
- `docs/E8_EXECUTION_PLAN.md`
- `docs/PRODUCT_PHASE_PLAN.md`

## Current accepted state

Accepted / established:

- E8f2 Session-owned Recorder.
- ActivityBuffer is recorder-owned for stream output.
- Multi-viewer recorder duplication resolved.
- R1a LTE connectivity baseline accepted.
- tmux/localpty work naturally as PTY byte-stream control adapters.
- cmux Terminal live view disabled for `screen_snapshot_delta` adapters.
- cmux Transcript is not reliable history; it is derived from viewport-dependent
  snapshots.

## Important product/architecture conclusion

Do not treat every adapter as equivalent.

Use this model:

```text
Control Adapter
→ reliable byte stream / append-oriented source
→ Recorder-backed Transcript
→ input/control capable

Observe Adapter
→ snapshot/log/app-state observation
→ best-effort or degraded Transcript
→ limited/no input/control
```

cmux is currently an Observe Adapter.

tmux/localpty are Control Adapter candidates.

Future Controlled PTY Runtime should be a first-class Control Adapter, but it is
not the next implementation task.

## Immediate target

```text
E8g5 — Adapter Capability Reclassification
```

Mission:

Make adapter capabilities explicit so backend/mobile stop implying that cmux has
tmux-equivalent live terminal or reliable transcript behavior.

## E8g5 scope

In scope:

- Define adapter class/capability contract in code or docs as appropriate.
- Capabilities to cover:
  - observe
  - control
  - input
  - liveTerminal
  - reliableTranscript
  - bestEffortTranscript
- tmux/localpty should remain live terminal / reliable transcript capable.
- cmux should be observe / best-effort transcript / no live terminal.
- Mobile should render capability state, not vendor-specific behavior.
- Unknown/future adapters should degrade safely.

Out of scope:

- Controlled PTY Runtime implementation.
- Attaching to existing Terminal.app/iTerm/Ghostty/Warp sessions.
- New cmux transcript heuristics.
- Re-enabling cmux Terminal live xterm.
- Custom terminal emulator.
- Terminal app plugins.

## Required acceptance for E8g5

Backend:

- Capability/class contract is explicit.
- cmux exposes no live terminal capability.
- cmux exposes best-effort/degraded transcript capability if Transcript remains
  available.
- tmux/localpty preserve live terminal capability.
- Unknown adapters degrade safely.

Mobile:

- No hardcoded adapter-name branch for product behavior.
- Terminal tab disabled/unavailable based on capability.
- Transcript quality shown based on capability.
- Existing tmux/localpty flows continue.
- cmux communicates observe/best-effort state clearly.

Tests / evidence:

- Tests prove cmux live terminal unavailable behavior still holds.
- Tests prove byte-stream adapters still allow live terminal.
- Tests prove capability payloads are preserved through session API if the API is
  changed.
- If mobile code changes, typecheck/build gate must run or be explicitly
  reported as not-run with reason.

## Do not do this

Do not attempt:

```text
existing Terminal.app attach
existing iTerm2/Ghostty/Warp attach
Accessibility input injection
AppleScript screen scraping
private macOS APIs
more cmux reliable-transcript heuristics
```

Those are not E8g5.

## After E8g5

Expected next phases:

```text
E8g6 — cmux Observe Mode Stabilization
E9-pre — Controlled PTY Runtime Design
E9 — Controlled PTY Runtime MVP
E10 — Agent Launch UX
E11 — Optional Terminal App Integrations
```

Do not start E9 implementation until E9-pre design is accepted.

## Completion report format

Use this format:

```text
E8g5 complete.

Commit: <sha>

Changed:
- <files>

Capability contract:
- tmux: ...
- localpty: ...
- cmux: ...
- unknown: ...

Verified:
- cmux live terminal unavailable: ...
- byte-stream adapters still work: ...
- mobile capability rendering: ...
- tests/build gate: ...

Not included:
- Controlled PTY Runtime
- existing terminal attach
- new cmux transcript heuristics
```

