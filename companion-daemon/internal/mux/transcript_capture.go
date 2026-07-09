package mux

// TranscriptCaptureMode describes how an adapter produces transcript data.
//
// byte_stream: Real PTY output. Each read returns incremental bytes.
// The Recorder reads raw PTY deltas. Used by tmux, localpty.
//
// screen_snapshot_delta: Screen polling. The adapter captures full
// terminal screens at intervals and extracts new output by comparing
// consecutive snapshots. Best-effort — not equivalent to byte_stream.
// Used by cmux. Volatile UI (timers, spinners, status bars) may be
// filtered. Identical snapshots produce no transcript events.
//
// unsupported: No transcript capture. ActivityBuffer receives nothing.
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
