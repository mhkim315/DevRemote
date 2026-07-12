package contract

import (
	"errors"
	"unicode/utf8"
)

// Cursor is an OPAQUE incremental-read position. Callers never parse it; they
// pass the previous NextCursor back unchanged. An empty cursor means "from the
// beginning". Adapters MUST keep it bounded (see MaxCursorBytes) — a cursor that
// accumulates unbounded history is a contract violation, because it defeats the
// bounded-read guarantee. Prefer a compact high-water-mark (e.g. the last Seq)
// over an ever-growing id set.
type Cursor string

// IsEmpty reports whether the cursor is the start-of-stream position.
func (c Cursor) IsEmpty() bool { return c == "" }

// Bounds. Every unbounded read/collection in the contract is capped so a
// malformed or hostile provider cannot cause unbounded replay, memory growth, or
// diagnostic flooding. Adapters MUST honor these even if a caller passes a larger
// limit; the fixed harness verifies truncation is reported as degraded, not silent.
const (
	// MaxEventsPerRead caps a single ReadEvents batch (events returned).
	MaxEventsPerRead = 1000
	// MaxDiscoveredSessions caps a single DiscoverSessions result.
	MaxDiscoveredSessions = 256
	// MaxMetadataEntries / MaxMetadataKeyBytes / MaxMetadataValueBytes bound
	// per-event agent metadata. Every key AND value is bounded, on every entry.
	MaxMetadataEntries    = 32
	MaxMetadataKeyBytes   = 128
	MaxMetadataValueBytes = 512
	// MaxDiagnostics / MaxDiagnosticBytes bound degraded diagnostics.
	MaxDiagnostics     = 32
	MaxDiagnosticBytes = 256
	// MaxCursorBytes bounds an opaque cursor token.
	MaxCursorBytes = 4096
	// MaxRecordBytes bounds a single raw provider record handed to the adapter.
	MaxRecordBytes = 1 << 20 // 1 MiB
	// MaxBatchRecords / MaxBatchBytes bound one ReadEvents input batch.
	MaxBatchRecords = 5000
	MaxBatchBytes   = 8 << 20 // 8 MiB
)

// Cursor validation errors.
var (
	ErrCursorTooLarge = errors.New("cursor exceeds MaxCursorBytes")
	ErrCursorInvalid  = errors.New("cursor is not valid UTF-8")
)

// ValidateCursor rejects an oversized or malformed cursor. An empty cursor is
// always valid (start of stream). Adapters call this on the incoming cursor and
// on the NextCursor they emit; the fixed harness asserts NextCursor validity.
func ValidateCursor(c Cursor) error {
	if len(c) > MaxCursorBytes {
		return ErrCursorTooLarge
	}
	if !utf8.ValidString(string(c)) {
		return ErrCursorInvalid
	}
	return nil
}

// effectiveLimit resolves a caller-supplied limit against a hard cap. A
// non-positive caller limit means "use the cap"; a larger one is clamped.
func effectiveLimit(callerLimit, hardCap int) int {
	if callerLimit <= 0 || callerLimit > hardCap {
		return hardCap
	}
	return callerLimit
}

// EffectiveReadLimit is the number of events a ReadEvents call may return.
func EffectiveReadLimit(callerMax int) int { return effectiveLimit(callerMax, MaxEventsPerRead) }

// EffectiveDiscoveryLimit is the number of sessions a DiscoverSessions call may return.
func EffectiveDiscoveryLimit(callerLimit int) int {
	return effectiveLimit(callerLimit, MaxDiscoveredSessions)
}

// AcceptRecord reports whether a raw record is within per-record size bounds.
// An oversized record must be skipped (and the batch degraded), never processed.
func AcceptRecord(rec RawRecord) bool { return len(rec.Bytes) <= MaxRecordBytes }

// BoundBatch caps an input batch to (MaxBatchRecords, MaxBatchBytes) and reports
// whether truncation occurred so the caller can surface a degraded result.
func BoundBatch(records []RawRecord) (bounded []RawRecord, truncated bool) {
	if len(records) > MaxBatchRecords {
		records, truncated = records[:MaxBatchRecords], true
	}
	total := 0
	for i, r := range records {
		total += len(r.Bytes)
		if total > MaxBatchBytes {
			return records[:i], true
		}
	}
	return records, truncated
}
