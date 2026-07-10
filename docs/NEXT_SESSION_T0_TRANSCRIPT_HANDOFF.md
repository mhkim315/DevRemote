# Next Session Handoff — T0 Transcript Contract Reset

Status: ready only after M3 and verifier acceptance of its runtime/security
dependencies

Read first:

- `docs/T0_TRANSCRIPT_CONTRACT_RESET_PLAN.md`
- `docs/R1_RUNTIME_SIGNAL_MATRIX.md`
- `docs/MOBILE_SESSION_LIFECYCLE_AND_TRANSCRIPT_PLAN.md`
- `companion-daemon/internal/term/recorder.go`
- `companion-daemon/internal/term/activity.go`
- `companion-daemon/internal/mux/transcript_capture.go`

## Mission

Audit and specify the reset from legacy Activity capture to an asynchronous
Transcript projection boundary. Do not implement the projector in T0.

The current bug class is architectural: `Recorder` currently combines raw PTY
capture, cmux sentinel handling, ANSI/CR cleanup, and Activity appends, while
`ActivityBuffer` merges output and strips ANSI again. Do not repair it with
another global string heuristic.

## Hard invariants

```text
one session → one Recorder → one PTY reader
raw PTY bytes → Live Terminal unchanged
projection failure/overflow → never blocks or corrupts Live Terminal
raw input text → never stored
cmux snapshot rules → never affect byte_stream adapters
```

## Required audit table

For each item below, record its current owner, adapters affected, observed
reason, T0 disposition, and the future owning component.

- `stripANSI` in Recorder;
- CR-to-LF replacement;
- `isANSIControlOnly` filtering;
- adjacent output merge and re-strip in ActivityBuffer;
- `ESC[9998m` delta framing;
- `ESC[9999m` snapshot framing;
- clear-screen snapshot drain and one-second timeout;
- 32 KB text truncation;
- sequence/event ID assignment;
- Activity API/mobile compatibility.

Allowed dispositions: `retain in raw path`, `move to ByteStreamProjector`,
`move to SnapshotProjector`, `legacy migration only`, or `delete`.

## Required fixtures

Capture redacted, reproducible fixtures without touching user configuration:

1. bash: `pwd`, `ls`, `echo hello`;
2. PTY output split across arbitrary chunks, including a split ANSI sequence;
3. carriage-return progress ending in a stable final line;
4. one alternate-screen/TUI sample for Codex and Claude when locally available.

Fixtures must identify source, capture mode, chunk boundaries, expected v2
document result, and redaction. They must not contain repository content,
prompts, tokens, home path, or raw terminal input.

## Do not do

- do not edit `Recorder`, `ActivityBuffer`, mobile UI, WebSocket, lifecycle, or
  pairing code;
- do not create a virtual terminal/screen emulator;
- do not promise semantic Claude/Codex messages from PTY text;
- do not add a provider parser, hook, JSONL reader, or app-server integration;
- do not alter cmux behavior while auditing byte-stream Transcript.

## Completion report

Return:

```text
T0 status: ACCEPT / NEEDS DECISION
Commit: <hash>

Legacy heuristic disposition:
- <heuristic> → <future owner or deletion>

V2 contract:
- event fields, ordering, source/provenance
- raw-input policy
- queue overflow/failure behavior

Fixture matrix:
- bash
- split ANSI
- CR progress
- Codex TUI
- Claude TUI

T1 boundary:
- exact first implementation scope
- explicit acceptance tests

No production code changed: yes/no
```
