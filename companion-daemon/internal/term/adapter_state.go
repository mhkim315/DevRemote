package term

import (
	"time"

	"devremote/companion-daemon/internal/agent/contract"
	"devremote/companion-daemon/internal/transcript"
)

// Bounds for the immutable full-prefix ingestion epoch.
const (
	maxPrefixRecords = 2000
	maxPrefixBytes   = 4 * 1024 * 1024 // 4 MiB
)

// adapterState manages the session-scoped state for the accepted T1/T2
// adapter ingestion path. It maintains a bounded IMMUTABLE full prefix
// (records are never trimmed from the beginning) and stores the adapter's
// opaque cursor unchanged.
type adapterState struct {
	streamGen int
	path      string

	records    [][]byte
	totalBytes int64

	adapterCursor string

	overflowed     bool
	overflowReason string
	markerEmitted  bool // B3: at most one degraded marker per generation

	version         string
	versionValid    bool
	versionConflict bool // B2: true when adapter reports degraded/conflict
}

func newAdapterState() *adapterState {
	return &adapterState{}
}

func (a *adapterState) resetForGeneration(path string) {
	a.streamGen++
	a.path = path
	a.records = nil
	a.totalBytes = 0
	a.adapterCursor = ""
	a.overflowed = false
	a.overflowReason = ""
	a.markerEmitted = false
	a.version = ""
	a.versionValid = false
	a.versionConflict = false
}

func (a *adapterState) appendRecords(rawLines [][]byte) {
	for _, line := range rawLines {
		lineLen := int64(len(line))
		if len(a.records) >= maxPrefixRecords || a.totalBytes+lineLen > maxPrefixBytes {
			a.overflowed = true
			if a.overflowReason == "" {
				a.overflowReason = "semantic ingestion overflowed: prefix bounds exceeded"
			}
			return
		}
		a.records = append(a.records, line)
		a.totalBytes += lineLen
	}
}

func (a *adapterState) buildAdapterInput() (records [][]byte, cursor string, overflowed bool) {
	return a.records, a.adapterCursor, a.overflowed
}

func (a *adapterState) setCursor(cursor string) {
	a.adapterCursor = cursor
}

func (a *adapterState) cursor() string {
	return a.adapterCursor
}

// updateVersion records the adapter-validated version. Returns true if
// version was accepted (matches isAcceptedVersion for this kind).
func (a *adapterState) updateVersion(version string, kind string) bool {
	if version == "" {
		return false
	}
	if !isAcceptedVersion(kind, version) {
		a.versionConflict = true
		a.versionValid = false
		return false
	}
	if a.versionValid && a.version != version {
		// Conflicting version across polls → conflict.
		a.versionConflict = true
		a.versionValid = false
		return false
	}
	a.version = version
	a.versionValid = true
	return true
}

// markVersionConflict records a version conflict from adapter degradation.
func (a *adapterState) markVersionConflict() {
	a.versionConflict = true
	a.versionValid = false
}

// shouldEmitOverflowMarker returns true only for the first overflow poll.
func (a *adapterState) shouldEmitOverflowMarker() bool {
	if !a.overflowed || a.markerEmitted {
		return false
	}
	a.markerEmitted = true
	return true
}

func (a *adapterState) launchCorrelation(binding *transcript.LaunchBinding, discoveredAdapter, kind string, discoveredPID int, discoveredStart time.Time) contract.Correlation {
	if binding == nil {
		return contract.CorrelationUnavailable
	}
	if a.versionConflict || !a.versionValid {
		return contract.CorrelationUnavailable
	}
	return transcript.LaunchCorrelation(binding, discoveredAdapter, kind, a.version, discoveredPID, discoveredStart)
}

func (a *adapterState) clear() {
	*a = adapterState{}
}
