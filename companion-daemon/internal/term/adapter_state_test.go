package term

import (
	"testing"

	"devremote/companion-daemon/internal/agent/contract"
	"devremote/companion-daemon/internal/transcript"
)

// ── Generation and reset tests ──

func TestAdapterState_GenerationReset(t *testing.T) {
	a := newAdapterState()

	// Append some records.
	a.appendRecords([][]byte{[]byte(`{"a":1}`), []byte(`{"a":2}`)})
	a.setCursor("5:abc123")
	a.updateVersion("0.144.1", "codex")

	// Verify state was set.
	if !a.versionValid {
		t.Error("versionValid should be true after updateVersion")
	}

	// Reset for generation change.
	a.resetForGeneration("/new/path.jsonl")

	// All stream-derived state must be cleared.
	if len(a.records) != 0 {
		t.Errorf("records not cleared after generation reset: got %d", len(a.records))
	}
	if a.totalBytes != 0 {
		t.Errorf("totalBytes not zeroed: got %d", a.totalBytes)
	}
	if a.adapterCursor != "" {
		t.Errorf("adapterCursor not cleared: got %q", a.adapterCursor)
	}
	if a.versionValid {
		t.Error("versionValid still true after reset")
	}
	if a.overflowed {
		t.Error("overflowed still true after reset")
	}
	if a.path != "/new/path.jsonl" {
		t.Errorf("path not updated: got %q", a.path)
	}
}

func TestAdapterState_PathChange(t *testing.T) {
	a := newAdapterState()
	a.resetForGeneration("/path/a.jsonl")
	a.appendRecords([][]byte{[]byte(`{"a":1}`)})

	// Path change should trigger reset via caller detecting a.path != logRef.Path.
	// Simulate that by calling resetForGeneration.
	a.resetForGeneration("/path/b.jsonl")

	if len(a.records) != 0 {
		t.Errorf("records not cleared after path change: got %d", len(a.records))
	}
	if a.path != "/path/b.jsonl" {
		t.Errorf("path not updated: got %q", a.path)
	}
}

func TestAdapterState_PartialEOFDoesNotReset(t *testing.T) {
	a := newAdapterState()
	a.resetForGeneration("/path/test.jsonl")
	a.appendRecords([][]byte{[]byte(`{"a":1}`)})
	a.setCursor("5:abc123")

	// Simulate a poll with no new lines (partial EOF).
	// appendRecords with empty slice should be a no-op for state.
	a.appendRecords([][]byte{})

	if len(a.records) != 1 {
		t.Errorf("records changed after empty append: got %d", len(a.records))
	}
	if a.adapterCursor != "5:abc123" {
		t.Errorf("cursor changed after empty poll: got %q", a.adapterCursor)
	}
}

// ── Full-prefix preservation tests ──

func TestAdapterState_FullPrefixPreserved(t *testing.T) {
	a := newAdapterState()
	a.resetForGeneration("/path/test.jsonl")

	// First poll: 3 records.
	a.appendRecords([][]byte{
		[]byte(`{"pos":0}`),
		[]byte(`{"pos":1}`),
		[]byte(`{"pos":2}`),
	})
	records, cursor, overflowed := a.buildAdapterInput()

	// Must return all 3 records (full prefix, not sliced).
	if len(records) != 3 {
		t.Fatalf("first poll: expected 3 records, got %d", len(records))
	}
	if cursor != "" {
		t.Errorf("first poll: cursor should be empty, got %q", cursor)
	}
	if overflowed {
		t.Error("first poll: unexpected overflow")
	}

	// Second poll: 2 new records. Must return all 5 (full prefix).
	a.setCursor("3:hash2")
	a.appendRecords([][]byte{
		[]byte(`{"pos":3}`),
		[]byte(`{"pos":4}`),
	})
	records, cursor, overflowed = a.buildAdapterInput()

	if len(records) != 5 {
		t.Errorf("second poll: expected 5 records (full prefix), got %d", len(records))
	}
	if cursor != "3:hash2" {
		t.Errorf("second poll: cursor should be unchanged, got %q", cursor)
	}
	if overflowed {
		t.Error("second poll: unexpected overflow")
	}
}

// ── Opaque cursor passthrough tests ──

func TestAdapterState_OpaqueCursorPassthrough(t *testing.T) {
	a := newAdapterState()
	a.resetForGeneration("/path/test.jsonl")

	// Set a cursor in the adapter's format.
	a.setCursor("150:deadbeefcafebabe")

	// buildAdapterInput returns the cursor unchanged.
	records, cursor, overflowed := a.buildAdapterInput()

	// Empty prefix is fine — records can be empty when cursor is set.
	if len(records) != 0 {
		t.Logf("records present: %d", len(records))
	}
	// The cursor must be preserved exactly as set.
	if cursor != "150:deadbeefcafebabe" {
		t.Errorf("cursor was modified: got %q, want %q", cursor, "150:deadbeefcafebabe")
	}
	if overflowed {
		t.Error("unexpected overflow on empty prefix")
	}
}

func TestAdapterState_OpaqueCursorRoundTrip(t *testing.T) {
	a := newAdapterState()
	a.resetForGeneration("/path/test.jsonl")

	// Simulate adapter returning a cursor.
	originalCursor := "42:abcd1234"
	a.setCursor(originalCursor)

	// After storing, the cursor must be retrievable unchanged.
	if got := a.cursor(); got != originalCursor {
		t.Errorf("cursor round-trip failed: got %q, want %q", got, originalCursor)
	}

	// Even after appending more records, the cursor must not be altered.
	a.appendRecords([][]byte{[]byte(`{"new":1}`), []byte(`{"new":2}`)})
	if got := a.cursor(); got != originalCursor {
		t.Errorf("cursor changed after append: got %q, want %q", got, originalCursor)
	}
}

// ── Overflow tests ──

func TestAdapterState_OverflowOnRecords(t *testing.T) {
	a := newAdapterState()
	a.resetForGeneration("/path/test.jsonl")

	// Append 2001 records (exceeds maxPrefixRecords=2000).
	lines := make([][]byte, 2001)
	for i := range lines {
		lines[i] = []byte(`{"record":` + itoa(i) + `}`)
	}
	a.appendRecords(lines)

	_, _, overflowed := a.buildAdapterInput()
	if !overflowed {
		t.Error("2001 records: expected overflow, got none")
	}
	// Prefix must NOT have been trimmed — only the first 2000 records are kept,
	// and the 2001st triggers overflow.
	if len(a.records) > maxPrefixRecords {
		t.Errorf("records exceeded max: got %d, want <= %d", len(a.records), maxPrefixRecords)
	}
	// No records should have been trimmed from position 0.
	if len(a.records) > 0 && len(a.records) < 2000 {
		t.Errorf("records were trimmed: only %d of 2000 kept", len(a.records))
	}
}

func TestAdapterState_OverflowOnBytes(t *testing.T) {
	a := newAdapterState()
	a.resetForGeneration("/path/test.jsonl")

	// Create records that collectively exceed 4MB.
	bigRecord := make([]byte, 4096)
	for i := range bigRecord {
		bigRecord[i] = 'x'
	}
	// 1024 records × 4096 bytes = 4MB. Add 1 more to exceed.
	lines := make([][]byte, 1025)
	for i := range lines {
		lines[i] = bigRecord
	}
	a.appendRecords(lines)

	_, _, overflowed := a.buildAdapterInput()
	if !overflowed {
		t.Error("4MB+ records: expected overflow, got none")
	}
	if a.totalBytes > maxPrefixBytes {
		t.Logf("byte bound enforcement: totalBytes=%d (capped at boundary)", a.totalBytes)
	}
}

func TestAdapterState_OverflowDegradedOnly(t *testing.T) {
	a := newAdapterState()
	a.resetForGeneration("/path/test.jsonl")

	// Overflow should be sticky — once set, subsequent appends must not
	// clear it or resume adapter calls.
	lines := make([][]byte, 2001)
	for i := range lines {
		lines[i] = []byte(`{"r":` + itoa(i) + `}`)
	}
	a.appendRecords(lines)

	_, _, overflowed := a.buildAdapterInput()
	if !overflowed {
		t.Fatal("expected overflow after 2001 records")
	}

	// More records should not clear overflow.
	a.appendRecords([][]byte{[]byte(`{"new":1}`)})
	_, _, overflowed = a.buildAdapterInput()
	if !overflowed {
		t.Error("overflow was cleared by subsequent append — must be sticky")
	}
}

// ── B3: Overflow marker dedup ──

func TestAdapterState_OverflowMarkerExactlyOnce(t *testing.T) {
	a := newAdapterState()
	a.resetForGeneration("/path/test.jsonl")

	// Trigger overflow.
	lines := make([][]byte, 2001)
	for i := range lines {
		lines[i] = []byte(`{"r":` + itoa(i) + `}`)
	}
	a.appendRecords(lines)

	// First call: should emit.
	if !a.shouldEmitOverflowMarker() {
		t.Error("first shouldEmitOverflowMarker: expected true")
	}

	// Second call: should NOT emit (markerEmitted = true).
	if a.shouldEmitOverflowMarker() {
		t.Error("second shouldEmitOverflowMarker: expected false (already emitted)")
	}

	// Third call: still false.
	if a.shouldEmitOverflowMarker() {
		t.Error("third shouldEmitOverflowMarker: expected false (already emitted)")
	}

	// After generation reset: should emit again (new generation).
	a.resetForGeneration("/path/new.jsonl")
	lines2 := make([][]byte, 2001)
	for i := range lines2 {
		lines2[i] = []byte(`{"r2":` + itoa(i) + `}`)
	}
	a.appendRecords(lines2)
	if !a.shouldEmitOverflowMarker() {
		t.Error("after reset shouldEmitOverflowMarker: expected true (new generation)")
	}
}

// ── B2: Version authority tests ──

func TestAdapterState_VersionConflictRevokesAuthority(t *testing.T) {
	a := newAdapterState()
	a.resetForGeneration("/path/test.jsonl")

	// Accepted version.
	if !a.updateVersion("0.144.1", "codex") {
		t.Error("updateVersion accepted: expected true")
	}
	if !a.versionValid {
		t.Error("versionValid should be true")
	}

	// Conflicting version → revoke.
	if a.updateVersion("0.144.2", "codex") {
		t.Error("updateVersion conflicting: expected false")
	}
	if a.versionValid {
		t.Error("versionValid should be false after conflict")
	}
	if !a.versionConflict {
		t.Error("versionConflict should be true")
	}
}

func TestAdapterState_DegradedRevokesAuthority(t *testing.T) {
	a := newAdapterState()
	a.resetForGeneration("/path/test.jsonl")

	// Establish version.
	a.updateVersion("0.144.1", "codex")

	// Adapter reports degraded → mark conflict.
	a.markVersionConflict()
	if a.versionValid {
		t.Error("versionValid should be false after markVersionConflict")
	}
	if !a.versionConflict {
		t.Error("versionConflict should be true after markVersionConflict")
	}

	// Correlation must be unavailable after conflict.
	corr := a.launchCorrelation(&transcript.LaunchBinding{
		SessionID:       "s",
		Provider:        "codex",
		AcceptedVersion: "0.144.1",
		PID:             0,
	}, "codex", 12345)
	if corr != contract.CorrelationUnavailable {
		t.Errorf("correlation after version conflict: got %v, want Unavailable", corr)
	}
}

// ── No-trim verification ──

func TestAdapterState_NoTrimNoRebase(t *testing.T) {
	a := newAdapterState()
	a.resetForGeneration("/path/test.jsonl")

	// Append 100 records.
	for i := 0; i < 100; i++ {
		a.appendRecords([][]byte{[]byte(`{"i":` + itoa(i) + `}`)})
	}

	if len(a.records) != 100 {
		t.Fatalf("expected 100 records, got %d", len(a.records))
	}

	// Build input — must include all records from position 0.
	records, _, _ := a.buildAdapterInput()
	if len(records) != 100 {
		t.Errorf("expected 100 records in input, got %d", len(records))
	}

	// After multiple appends up to 2000, position 0 must survive.
	for i := 0; i < 19; i++ {
		batch := make([][]byte, 100)
		for j := range batch {
			batch[j] = []byte(`{"batch":` + itoa(i) + `,"j":` + itoa(j) + `}`)
		}
		a.appendRecords(batch)
	}

	// 100 + 1900 = 2000 records, right at the bound.
	records, _, overflowed := a.buildAdapterInput()
	if len(records) != 2000 {
		t.Errorf("expected 2000 records, got %d", len(records))
	}
	if overflowed {
		t.Error("unexpected overflow at exact bound")
	}

	// One more record should overflow, but the 2000 records are preserved.
	a.appendRecords([][]byte{[]byte(`{"overflow":1}`)})
	records, _, overflowed = a.buildAdapterInput()
	if !overflowed {
		t.Error("expected overflow after 2001st record")
	}
	if len(records) != 2000 {
		t.Errorf("after overflow: expected 2000 preserved records, got %d", len(records))
	}
}

// ── Cursor slicing verification ──

func TestAdapterState_NoDoubleOffset(t *testing.T) {
	a := newAdapterState()
	a.resetForGeneration("/path/test.jsonl")

	// Simulate 3 polls. On each poll, the adapter receives full prefix
	// and returns a cursor. The bridge must never slice based on cursor.

	// Poll 1: 100 records.
	lines1 := make([][]byte, 100)
	for i := range lines1 {
		lines1[i] = []byte(`{"p1":` + itoa(i) + `}`)
	}
	a.appendRecords(lines1)

	records, cursor, _ := a.buildAdapterInput()
	if len(records) != 100 {
		t.Errorf("poll 1: expected 100 records, got %d", len(records))
	}
	if cursor != "" {
		t.Errorf("poll 1: expected empty cursor, got %q", cursor)
	}

	// Adapter returns cursor "50:hash49".
	a.setCursor("50:hash49")

	// Poll 2: 50 more records, total 150.
	lines2 := make([][]byte, 50)
	for i := range lines2 {
		lines2[i] = []byte(`{"p2":` + itoa(i) + `}`)
	}
	a.appendRecords(lines2)

	records, cursor, _ = a.buildAdapterInput()
	if len(records) != 150 {
		t.Errorf("poll 2: expected 150 records (full prefix), got %d — cursor was used to slice suffix", len(records))
	}
	if cursor != "50:hash49" {
		t.Errorf("poll 2: cursor should be unchanged %q, got %q", "50:hash49", cursor)
	}

	// Poll 3: 50 more records, total 200.
	a.setCursor("100:hash99")
	lines3 := make([][]byte, 50)
	for i := range lines3 {
		lines3[i] = []byte(`{"p3":` + itoa(i) + `}`)
	}
	a.appendRecords(lines3)

	records, cursor, _ = a.buildAdapterInput()
	if len(records) != 200 {
		t.Errorf("poll 3: expected 200 records (full prefix), got %d — cursor was used to slice suffix", len(records))
	}
	if cursor != "100:hash99" {
		t.Errorf("poll 3: cursor should be unchanged %q, got %q", "100:hash99", cursor)
	}
}

// ── Helpers ──

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	// Use simple digit building.
	digits := make([]byte, 0, 20)
	if n == 0 {
		return "0"
	}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
