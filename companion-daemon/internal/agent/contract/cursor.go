package contract

// Cursor is an OPAQUE incremental-read position. Callers never parse it; they
// pass the previous NextCursor back unchanged. An empty cursor means "from the
// beginning". Adapters typically derive it from a content hash or byte offset so
// that re-reading the same input with the same cursor yields zero new events.
type Cursor string

// IsEmpty reports whether the cursor is the start-of-stream position.
func (c Cursor) IsEmpty() bool { return c == "" }

// Bounds. Every unbounded read/collection in the contract is capped so a
// malformed or hostile provider cannot cause unbounded replay, memory growth, or
// diagnostic flooding. Adapters MUST honor these even if a caller passes a larger
// limit; the harness verifies truncation is reported as degraded, not silent.
const (
	// MaxEventsPerRead caps a single ReadEvents batch.
	MaxEventsPerRead = 1000
	// MaxDiscoveredSessions caps a single DiscoverSessions result.
	MaxDiscoveredSessions = 256
	// MaxMetadataEntries / MaxMetadataValueBytes bound per-event agent metadata.
	MaxMetadataEntries    = 32
	MaxMetadataValueBytes = 512
	// MaxDiagnostics / MaxDiagnosticBytes bound degraded diagnostics.
	MaxDiagnostics     = 32
	MaxDiagnosticBytes = 256
)

// effectiveLimit resolves a caller-supplied limit against a hard cap. A
// non-positive caller limit means "use the cap".
func effectiveLimit(callerLimit, cap int) int {
	if callerLimit <= 0 || callerLimit > cap {
		return cap
	}
	return callerLimit
}

// EffectiveReadLimit is the number of events a ReadEvents call may return.
func EffectiveReadLimit(callerMax int) int { return effectiveLimit(callerMax, MaxEventsPerRead) }

// EffectiveDiscoveryLimit is the number of sessions a DiscoverSessions call may return.
func EffectiveDiscoveryLimit(callerLimit int) int {
	return effectiveLimit(callerLimit, MaxDiscoveredSessions)
}
