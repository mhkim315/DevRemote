package mux

// AdapterCapability is a granular capability flag for terminal adapters.
// Replaces the old "live_stream"/"screen"/"history" capability strings
// with explicit control/observe/transcript semantics.
type AdapterCapability string

const (
	// CapObserve: the adapter can list and view sessions.
	CapObserve AdapterCapability = "observe"

	// CapControl: the adapter can send input / drive the session. This is
	// interactive control (keystrokes, session driving) — it is NOT
	// process-lifecycle ownership. An externally owned runtime can
	// accept control while its process lifecycle is owned elsewhere. Lifecycle
	// ownership is CapManagedLifecycle, a separate dimension.
	CapControl AdapterCapability = "control"

	// CapInput: the adapter supports WriteInput / keystroke delivery.
	// Distinct from CapControl: control = session driving, input = keystrokes.
	CapInput AdapterCapability = "input"

	// CapLiveTerminal: the adapter supports live xterm WebSocket stream.
	// Only byte_stream adapters have this.
	CapLiveTerminal AdapterCapability = "liveTerminal"

	// CapReliableTranscript: PTY byte-stream-based transcript.
	// Recorder-owned capture from real PTY deltas.
	// byte_stream adapters have this.
	CapReliableTranscript AdapterCapability = "reliableTranscript"

	// CapBestEffortTranscript: screen-snapshot-derived transcript.
	// Best-effort, not authoritative.
	// Viewport-dependent source. May include duplicates.
	CapBestEffortTranscript AdapterCapability = "bestEffortTranscript"

	// CapManagedLifecycle: Pokit owns the session's process / process group and
	// its Stop/Kill/cleanup lifecycle. Only adapters that CREATE and own the
	// runtime (controlled_pty) declare this. input=true / control=true do NOT
	// imply managedLifecycle=true — external or observer
	// External adapters are never managed.
	CapManagedLifecycle AdapterCapability = "managedLifecycle"
)

// ManagedLifecycleProvider is an optional adapter capability. Adapters whose
// session lifecycle (process, process group, Stop/Kill cleanup) is Pokit-owned
// return true. Adapters that do not implement it default to false — external
// or observer adapters must NOT be treated as managed merely because they
// accept input/control.
type ManagedLifecycleProvider interface {
	ManagedLifecycle() bool
}

// AdapterCapabilities returns the capability set for an adapter.
// Uses TranscriptCaptureMode + known adapter semantics.
func AdapterCapabilities(adapter Adapter) []AdapterCapability {
	caps := []AdapterCapability{CapObserve}

	// Determine transcript + live terminal from declared capture mode.
	if cp, ok := adapter.(TranscriptCaptureProvider); ok {
		switch cp.TranscriptCaptureMode() {
		case CaptureModeByteStream:
			caps = append(caps, CapLiveTerminal, CapReliableTranscript, CapInput, CapControl)

		case CaptureModeUnsupported:
			// observe only.
		}
	}
	// No TranscriptCaptureProvider declared → observe only (plus lifecycle
	// below, which no observe-only adapter opts into).

	// Lifecycle ownership is orthogonal to input/control. Appended last so the
	// existing capability order stays backward-compatible; only adapters that
	// own the runtime opt in (safe false default for everyone else).
	if mp, ok := adapter.(ManagedLifecycleProvider); ok && mp.ManagedLifecycle() {
		caps = append(caps, CapManagedLifecycle)
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
