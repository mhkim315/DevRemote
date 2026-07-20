package mux

// TranscriptCaptureMode describes how an adapter produces transcript data.
//
// byte_stream: Real PTY output. Each read returns incremental bytes.
// The Recorder reads raw PTY deltas. Used by adapters.
//
// screen_snapshot_delta: Screen polling. The adapter captures full
// terminal screens at intervals and extracts new output by comparing
// consecutive snapshots.
//
//
// unsupported: No transcript capture.
// Transcript tab shows "not available" or equivalent.

type TranscriptCaptureMode int

const (
	CaptureModeUnsupported TranscriptCaptureMode = iota
	CaptureModeByteStream
)

// TranscriptCaptureProvider is an optional adapter capability.
// Adapters that support transcript capture should implement this
// to declare their capture mode. If not implemented, the adapter
// is treated as CaptureModeUnsupported.
type TranscriptCaptureProvider interface {
	TranscriptCaptureMode() TranscriptCaptureMode
}
