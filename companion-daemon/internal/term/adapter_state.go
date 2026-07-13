package term

import (
	"devremote/companion-daemon/internal/agent/contract"
	"devremote/companion-daemon/internal/transcript"
)

// Bounds for the immutable full-prefix ingestion epoch.
// When either bound is exceeded, the adapter state marks overflow and
// stops calling the accepted adapter — no trimming, no rebasing, no
// fabricated continuation.
const (
	// maxPrefixRecords is the maximum number of records retained in the
	// full prefix. Exceeding this triggers overflow.
	maxPrefixRecords = 2000

	// maxPrefixBytes is the maximum total byte size of the retained prefix.
	// Exceeding this triggers overflow.
	maxPrefixBytes = 4 * 1024 * 1024 // 4 MiB
)

// adapterState manages the session-scoped state for the accepted T1/T2
// adapter ingestion path. It maintains a bounded IMMUTABLE full prefix
// (records are never trimmed from the beginning) and stores the adapter's
// opaque cursor unchanged.
//
// Design constraints (REMEDIATION §5):
//   - Full prefix from position 0 — never trim, rebase, or reindex.
//   - Opaque cursor — store, pass, return; never parse or rewrite.
//   - Bounded overflow — when records or bytes exceed limits, stop
//     calling the adapter and emit at most one degraded marker.
//   - Generation reset — all state cleared on inode change, truncation,
//     or path change.
type adapterState struct {
	// Stream identity.
	streamGen int    // incremented on generation change
	path      string // current source path

	// Bounded immutable full prefix.
	records    [][]byte // full prefix from position 0 (never trimmed)
	totalBytes int64    // running byte count of all records

	// Adapter continuation — opaque cursor stored unchanged.
	adapterCursor string

	// Overflow — when true, the bridge must stop calling the adapter.
	overflowed    bool
	overflowReason string // bounded diagnostic

	// Version authority — extracted by the ADAPTER, not by parallel parsing.
	version      string // adapter-validated version string
	versionValid bool   // true when adapter confirmed version OK
}

// newAdapterState creates a fresh adapter state.
func newAdapterState() *adapterState {
	return &adapterState{}
}

// resetForGeneration discards all stream-derived state for a new generation.
func (a *adapterState) resetForGeneration(path string) {
	a.streamGen++
	a.path = path
	a.records = nil
	a.totalBytes = 0
	a.adapterCursor = ""
	a.overflowed = false
	a.overflowReason = ""
	a.version = ""
	a.versionValid = false
}

// appendRecords adds newly read raw lines to the full prefix.
// Checks record count and byte bounds; sets overflow if exceeded.
func (a *adapterState) appendRecords(rawLines [][]byte) {
	for _, line := range rawLines {
		lineLen := int64(len(line))
		// Check bounds before appending.
		if len(a.records) >= maxPrefixRecords || a.totalBytes+lineLen > maxPrefixBytes {
			a.overflowed = true
			if a.overflowReason == "" {
				a.overflowReason = "semantic ingestion overflowed: prefix bounds exceeded"
			}
			return // stop appending — overflow is sticky
		}
		a.records = append(a.records, line)
		a.totalBytes += lineLen
	}
}

// buildAdapterInput returns the full prefix (never sliced), the opaque
// adapter cursor (unchanged), and whether the state has overflowed.
// The caller must check overflowed before calling the adapter.
func (a *adapterState) buildAdapterInput() (records [][]byte, cursor string, overflowed bool) {
	return a.records, a.adapterCursor, a.overflowed
}

// setCursor stores the adapter's opaque NextCursor unchanged.
func (a *adapterState) setCursor(cursor string) {
	a.adapterCursor = cursor
}

// cursor returns the stored opaque adapter cursor.
func (a *adapterState) cursor() string {
	return a.adapterCursor
}

// updateVersion records the adapter-validated version. This is a simple
// setter — version extraction and validation happens inside the adapter.
func (a *adapterState) updateVersion(version string) {
	if version != "" {
		a.version = version
		a.versionValid = true
	}
}

// launchCorrelation determines the correlation state using validated state.
func (a *adapterState) launchCorrelation(binding *transcript.LaunchBinding, kind string, discoveredPID int) contract.Correlation {
	if binding == nil {
		return contract.CorrelationUnavailable
	}
	if !a.versionValid {
		return contract.CorrelationUnavailable
	}
	return transcript.LaunchCorrelation(binding, kind, a.version, discoveredPID)
}

// clear resets all state (called on session delete).
func (a *adapterState) clear() {
	*a = adapterState{}
}
