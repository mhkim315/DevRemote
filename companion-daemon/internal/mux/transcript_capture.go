package mux

// TranscriptCaptureMode describes how an adapter produces transcript data.
//
// byte_stream: Real PTY output. Each read returns incremental bytes.
// The Recorder reads raw PTY deltas. Used by tmux, localpty.
//
// screen_snapshot_delta: Screen polling. The adapter captures full
// terminal screens at intervals and extracts new output by comparing
// consecutive snapshots.
//
// IMPORTANT LIMITATIONS (cmux):
//   - Source is viewport-state-dependent: PC-side scrolling changes the
//     snapshot and therefore the transcript. Transcript is NOT a stable
//     output history.
//   - Input echo creates transient duplicates that may appear before
//     stabilizing.
//   - Volatile UI (timers, spinners, status bars) is best-effort filtered
//     but not guaranteed.
//   - This mode is EXPERIMENTAL / DEGRADED. Do not present cmux Transcript
//     as authoritative history equivalent to byte_stream (tmux/localpty).
//
// Identical snapshots produce no transcript events.
// Used by cmux.
//
// unsupported: No transcript capture.
// Transcript tab shows "not available" or equivalent.

type TranscriptCaptureMode int

const (
	CaptureModeUnsupported TranscriptCaptureMode = iota
	CaptureModeByteStream
	CaptureModeScreenSnapshotDelta
)

// TranscriptCaptureProvider is an optional adapter capability.
// Adapters that support transcript capture should implement this
// to declare their capture mode. If not implemented, the adapter
// is treated as CaptureModeUnsupported.
type TranscriptCaptureProvider interface {
	TranscriptCaptureMode() TranscriptCaptureMode
}
