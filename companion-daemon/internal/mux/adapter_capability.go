package mux

// AdapterCapability is a granular capability flag for terminal adapters.
// Replaces the old "live_stream"/"screen"/"history" capability strings
// with explicit control/observe/transcript semantics.
type AdapterCapability string

const (
	// CapObserve: the adapter can list and view sessions.
	CapObserve AdapterCapability = "observe"

	// CapControl: the adapter can send input/control the session.
	// tmux/localpty have this. cmux does not.
	CapControl AdapterCapability = "control"

	// CapInput: the adapter supports WriteInput / keystroke delivery.
	// Distinct from CapControl: control = session lifecycle, input = keystrokes.
	CapInput AdapterCapability = "input"

	// CapLiveTerminal: the adapter supports live xterm WebSocket stream.
	// Only byte_stream adapters (tmux/localpty) have this.
	// screen_snapshot_delta adapters (cmux) do NOT.
	CapLiveTerminal AdapterCapability = "liveTerminal"

	// CapReliableTranscript: PTY byte-stream-based transcript.
	// Recorder-owned capture from real PTY deltas.
	// tmux/localpty have this.
	CapReliableTranscript AdapterCapability = "reliableTranscript"

	// CapBestEffortTranscript: screen-snapshot-derived transcript.
	// Best-effort, not authoritative. cmux has this.
	// Viewport-dependent source. May include duplicates.
	CapBestEffortTranscript AdapterCapability = "bestEffortTranscript"
)

// AdapterCapabilities returns the capability set for an adapter.
// Uses TranscriptCaptureMode + known adapter semantics.
func AdapterCapabilities(adapter Adapter) []AdapterCapability {
	name := adapter.Name()
	caps := []AdapterCapability{CapObserve}

	// Determine control capability.
	_, hasCreator := adapter.(SessionCreator)
	_, hasTerminator := adapter.(SessionTerminator)
	if hasCreator || hasTerminator {
		caps = append(caps, CapControl)
	}

	// Determine transcript + live terminal from declared capture mode.
	if cp, ok := adapter.(TranscriptCaptureProvider); ok {
		switch cp.TranscriptCaptureMode() {
		case CaptureModeByteStream:
			caps = append(caps, CapLiveTerminal, CapReliableTranscript, CapInput)
		case CaptureModeScreenSnapshotDelta:
			caps = append(caps, CapBestEffortTranscript)
			// No CapLiveTerminal, no CapReliableTranscript.
		case CaptureModeUnsupported:
			// observe only.
		}
	} else {
		// Legacy: no mode declared → assume byte_stream (tmux/localpty path).
		caps = append(caps, CapLiveTerminal, CapReliableTranscript, CapInput)
	}

	// Specific adapters may have additional capabilities.
	switch name {
	case "tmux", "localpty":
		// Already covered by CaptureModeByteStream branch above.
		// Ensure CapControl is explicitly present.
		if !hasCap(caps, CapControl) {
			caps = append(caps, CapControl)
		}
	case "cmux":
		// cmux does not support live terminal or reliable transcript.
		// Already excluded by CaptureModeScreenSnapshotDelta branch.
	}

	return caps
}

func hasCap(caps []AdapterCapability, target AdapterCapability) bool {
	for _, c := range caps {
		if c == target {
			return true
		}
	}
	return false
}
