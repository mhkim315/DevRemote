# Historical Transcript Contract Reset Plan — Rescoped into T3

Status: **HISTORICAL INPUT — not the active T0 execution plan**
Depends on: stable Recorder single-reader invariant, R1 evidence baseline
Does not depend on: provider-specific Claude/Codex integration

Roadmap correction: active T0 now defines the common AgentEvent and stable
adapter contract; T1 implements Codex, T2 implements Claude, D1 provides
user-approved Adapter Doctor/Repair, and T3 performs Transcript integration.
The byte-stream/Recorder safety content below remains authoritative input for T3
but must not be executed under the old T0/T1/T2 phase names.

## Decision

Restart Transcript projection from a narrow byte-stream contract. Do not add
another cleanup heuristic to the current `Recorder → ActivityBuffer` path.

```text
raw PTY bytes ──> Recorder ──> Live Terminal subscribers
                    │
                    └──> bounded, non-blocking projection input
                              │
                              └──> TranscriptProjector
                                        │
                                        └──> TranscriptStore / read API
```

The Recorder remains the sole PTY reader. It owns raw byte capture, the raw
bootstrap ring, and subscriber broadcast. It does not own transcript parsing,
ANSI stripping, line merging, semantic inference, or UI formatting.

## Why reset instead of tune

The current implementation has three conflated responsibilities:

| Current component | Current responsibility | T0 decision |
| --- | --- | --- |
| `Recorder.readLoop` | reads/broadcasts bytes, strips ANSI, rewrites CR, recognizes cmux sentinels and snapshots, appends Activity | retain read/broadcast; remove projection rules from byte-stream path |
| `ActivityBuffer.Append` | assigns sequence, merges adjacent output, strips ANSI again | retain temporary v1 read store only; no parsing or merging in v2 store |
| cmux delta logic | snapshot delta extraction framed with internal sentinels | isolate behind a snapshot-only projector; never run for byte streams |

`controlled_pty`, `tmux`, and `localpty` are incremental byte streams.
`cmux` is viewport-dependent `screen_snapshot_delta` and remains degraded.
The two must not share normalisation code.

## Source and authority boundaries

```text
Live Terminal
  source: raw recorder bytes
  authority: terminal state and interaction
  contract: xterm-compatible; never modified for Transcript

Transcript
  source: projector output
  authority: readable historical document only
  contract: may omit unsafe TUI regions; never controls lifecycle/input/approval

RuntimeEvent enrichment
  source: daemon lifecycle or provider-native evidence
  authority: only according to source confidence/provenance
  contract: additive to Transcript, not a replacement for generic shell output
```

R1 consequence: Claude hooks and Codex app-server signals are future optional
enrichers. Their correlation to an ordinary interactive `pokit run` TUI was
not proven. T0/T1 therefore cannot depend on them.

## T0 v2 event contract

T0 introduces a deliberately small internal event shape. It is not yet a
provider semantic model.

```text
TranscriptEventV2
  version: 2
  id: immutable event identifier
  sessionId
  seq: strictly increasing per session; never rewritten by merge
  kind: text | input_boundary | system_notice |
        terminal_ui_omitted | projection_gap
  text?: readable, redacted projection only
  source: byte_stream | screen_snapshot | runtime | provider
  observedAt
  byteCount?: input source byte count, not raw bytes
  metadata?: bounded, schema-versioned, non-secret
```

Rules:

- `terminal_input` remains metadata-only: count/timestamp/boundary, never raw
  typed text.
- Sequence is an event ordering contract, not an implementation detail. A
  later merge must create an explicit document segment or preserve the original
  constituent ordering; it may not silently mutate an earlier event.
- `text` is a projection, not replay material for a terminal.
- Provider events use the R1 `RuntimeEvent` envelope and are merged later in
  T3, after a proven controlled-session correlation contract.

## T0 projector contract

Input is a copied raw byte chunk plus session ID, capture mode, monotonic source
offset, and timestamp. The input queue has a fixed byte/event bound.

```text
Recorder read succeeds
  → raw byte broadcast and bootstrap update continue immediately
  → best-effort enqueue projection item
  → queue full: increment metric and enqueue/coalesce one projection_gap marker
```

Projection must not wait on a subscriber, mobile client, disk, provider API, or
the Recorder read loop. A failed projector is observable but must leave Live
Terminal and process lifecycle intact.

## Byte-stream policy for T1

T0 defines, but does not implement, these rules for `controlled_pty`, `tmux`,
and `localpty`:

1. Preserve decoder/parser state across arbitrary PTY chunk boundaries.
2. Commit ordinary printable text only at stable document boundaries, initially
   newline and explicit input boundaries.
3. Treat CR, backspace, erase-line, erase-screen, cursor movement, and
   alternate-screen as terminal operations, not appended document characters.
4. If the projector cannot safely classify a burst, emit one bounded
   `terminal_ui_omitted` marker and resume after the region; never flatten a
   repaint frame into a giant line.
5. Do not globally deduplicate ordinary repeated log lines.

T1 starts only with `pokit run bash`, `pwd`, `ls`, and `echo hello`. Codex and
Claude alternate-screen/TUI validation belongs to T2.

## Snapshot policy

`screen_snapshot_delta` is a separate `SnapshotProjector` input and is not a
T1 acceptance dependency. Its output must carry `source=screen_snapshot` and
best-effort/degraded presentation. The existing `ESC[9998m`/`ESC[9999m`
framing is legacy migration input only; it is not a durable cross-component
protocol. A later snapshot adapter may use an explicit typed frame instead.

## API and migration

Keep the existing `?activity=` read path intact during T0/T1.

1. Add v2 storage and a shadow read path behind a feature flag.
2. Feed the same controlled byte-stream fixture to legacy and v2 paths.
3. Compare ordering, simple shell readability, and absence of raw input. Do not
   require byte-for-byte equality because v2 intentionally handles CR/ANSI
   differently.
4. Expose a versioned/additive transcript DTO only after T1 acceptance.
5. Mobile keeps the v1 Activity view until the v2 endpoint is accepted; no
   Live Terminal fallback may consume v2 projected text.
6. Roll back by disabling the projector/read feature flag. Never roll back by
   changing Recorder subscriber broadcast or PTY ownership.

Existing in-memory Activity history may remain v1-only and expire naturally in
the MVP. Migration must not reinterpret or mutate existing stored events.

## T0 deliverables

- code-path audit naming every projection/merge heuristic and its disposition:
  retain, move to byte-stream projector, move to snapshot projector, or delete;
- v2 event schema and compatibility mapping;
- queue capacity, overflow metric, and `projection_gap` behavior decision;
- redacted fixtures for bash simple output, CR progress, split ANSI, and one
  alternate-screen/TUI sample each for Codex and Claude where available;
- T1/T2 execution handoff and acceptance matrix.

## T0 acceptance

T0 is accepted when all of the following are true:

- no production Recorder, ActivityBuffer, mobile, pairing, or lifecycle code
  changes are made merely to complete T0;
- every current heuristic is classified with an adapter/capture-mode boundary;
- v2 events have immutable ordering, provenance, and raw-input restrictions;
- queue overflow and projector failure have explicit non-blocking behavior;
- R1 provider signals are recorded as optional future enrichment only;
- T1 is constrained to bash byte-stream evidence and T2 owns Codex/Claude TUI
  safety.

## Explicit non-goals

- no custom terminal emulator or VT100 screen implementation;
- no change to xterm raw Live Terminal behavior;
- no semantic agent parsing, approval detection, or notification logic;
- no cmux reliability claim;
- no transcript persistence across daemon restart in this MVP;
- no provider hook, app-server, JSONL, or prompt-marker production integration.
