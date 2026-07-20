# PB Legacy Removal Contract

**PB Prerequisite SHA:** `ef012136c` (PA4-Final-R15)
**PB Rollback SHA:** `ef012136c`
**Status:** PENDING — PA4 accepted, PB can begin
**Date:** 2026-07-20

## Prerequisites

PB (Phase B) removes legacy observer/registry routes that are now contained and superseded by PA4 managed isolation. All managed paths (controlled_pty, codex, claude) are fully isolated — legacy routes serve only tmux/cmux adapters.

### Pre-PB State (at `ef012136c`)

- **useRegistry=true** only for non-managed adapters (tmux, cmux)
- **Legacy observer routes** feed TelemetryService snapshots for tmux/cmux sessions only
- **Registry adapter lookup** used only by legacy `/api/sessions` GET path for non-managed adapters
- **Managed catalog** (ManagedRuntimeCatalog) is self-contained, never probes Registry
- **TerminalTransport** uses direct Recorder reference, no global registry dependency
- **RecorderFor** method removed — no bypass path exists

### PB Removal Targets

| Target | Current State | Removal |
|--------|--------------|---------|
| Registry.FindSession for tmux/cmux WS | Contained in useRegistry=true branch | Remove when tmux/cmux WS is deprecated |
| TelemetryService observer snapshot | Feeds only legacy (non-managed) sessions | Remove observer loop entirely |
| buildSimpleSnapshot | Legacy telemetry | Replace with catalog-only projection |
| mux.Registry adapter scan for sessions | Used only by legacy GET /api/sessions | Remove when legacy sessions deprecated |

### Rollback

If PB encounters issues, rollback to `96e21fff5` — all PA4 isolation invariants are verified and frozen.
