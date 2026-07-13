package term

import (
	"encoding/json"

	"devremote/companion-daemon/internal/agent/contract"
	"devremote/companion-daemon/internal/transcript"
)

// PositionedRecord is a raw provider record with its immutable absolute
// record index. The index is assigned when the record is first read and
// never changes.
type PositionedRecord struct {
	Position int64 // absolute record index in source stream
	Raw      []byte
}

// adapterState manages the session-scoped state for the accepted T1/T2
// adapter ingestion path. It preserves absolute positions, bounded
// window, adapter cursor, and validated version authority.
type adapterState struct {
	// Stream identity.
	streamGen int    // incremented on generation change
	path      string // current source path

	// Bounded positioned window.
	records []PositionedRecord // retained positioned records
	basePos int64              // absolute position of records[0], or 0 if empty
	nextPos int64              // next absolute position to assign

	// Adapter continuation.
	adapterCursor string // from ReadResult.NextCursor

	// Validated version authority.
	version          string // discovered version string
	versionConfirmed bool   // true when current stream validated
	versionKind      string // "codex" or "claude"
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
	a.basePos = 0
	a.nextPos = 0
	a.adapterCursor = ""
	a.version = ""
	a.versionConfirmed = false
}

// appendRecords adds newly read positioned records to the window.
func (a *adapterState) appendRecords(rawLines [][]byte) {
	for _, line := range rawLines {
		a.records = append(a.records, PositionedRecord{
			Position: a.nextPos,
			Raw:      line,
		})
		a.nextPos++
	}
}

// trimIfNeeded removes oldest records when the window exceeds maxRecords.
// Returns true if trimming occurred. The base position is advanced, but
// the adapter cursor is NOT reset — it remains valid for the retained
// suffix because positions are absolute.
func (a *adapterState) trimIfNeeded(maxRecords int) bool {
	if len(a.records) <= maxRecords {
		return false
	}
	removed := len(a.records) - maxRecords
	a.records = a.records[removed:]
	a.basePos = a.records[0].Position
	return true
}

// buildAdapterInput returns the suffix of the window starting from the
// adapter cursor position. Translates absolute cursor to window index
// using basePos. If the cursor is behind the window (before trim),
// returns the full window. If the cursor is ahead of all records,
// returns empty. An empty cursor means start from position 0.
func (a *adapterState) buildAdapterInput() [][]byte {
	if a.adapterCursor == "" {
		// No cursor: start from position 0 (fresh validation).
		return a.rawSlice()
	}
	// Translate absolute cursor position to window index.
	cursorPos := parseAdapterCursor(a.adapterCursor)
	idx := int(cursorPos - a.basePos)
	if idx < 0 {
		// Cursor is before window (records were trimmed past it).
		// Reset: revalidate from current window start.
		a.adapterCursor = ""
		return a.rawSlice()
	}
	if idx >= len(a.records) {
		return nil // cursor ahead of all records
	}
	return a.rawSlice()[idx:]
}

// rawSlice returns all records as raw bytes without position translation.
func (a *adapterState) rawSlice() [][]byte {
	out := make([][]byte, len(a.records))
	for i, r := range a.records {
		out[i] = r.Raw
	}
	return out
}

// parseAdapterCursor extracts the absolute position from an adapter cursor
// string. The T1/T2 adapters use "<pos>:<anchor>" format.
func parseAdapterCursor(cursor string) int64 {
	if cursor == "" {
		return 0
	}
	// Format: "1234:..." or just "1234"
	var pos int64
	for i := 0; i < len(cursor); i++ {
		if cursor[i] == ':' {
			break
		}
		if cursor[i] >= '0' && cursor[i] <= '9' {
			pos = pos*10 + int64(cursor[i]-'0')
		}
	}
	return pos
}

// tryExtractVersion attempts to parse version authority from a raw record.
// Supports Codex (session_meta.cli_version) and Claude (top-level version).
func (a *adapterState) tryExtractVersion(raw []byte, kind string) (version string, ok bool) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", false
	}
	switch kind {
	case "codex":
		// Codex: record type must be "session_meta" with payload.cli_version
		if typ, _ := m["type"]; typ != nil {
			var t string
			if err := json.Unmarshal(typ, &t); err != nil || t != "session_meta" {
				return "", false
			}
		}
		if payload, _ := m["payload"]; payload != nil {
			var p map[string]json.RawMessage
			if err := json.Unmarshal(payload, &p); err == nil {
				if cliVer, _ := p["cli_version"]; cliVer != nil {
					var v string
					if err := json.Unmarshal(cliVer, &v); err == nil && v != "" {
						return v, true
					}
				}
			}
		}
		return "", false
	case "claude":
		// Claude: top-level "version" field
		if ver, _ := m["version"]; ver != nil {
			var v string
			if err := json.Unmarshal(ver, &v); err == nil && v != "" {
				return v, true
			}
		}
		return "", false
	}
	return "", false
}

// updateVersion extracts version from records and validates consistency.
// Detects conflicting versions and rejects authority on conflict.
func (a *adapterState) updateVersion(kind string) {
	if len(a.records) == 0 {
		return
	}
	for _, r := range a.records {
		v, ok := a.tryExtractVersion(r.Raw, kind)
		if !ok {
			continue
		}
		if !isAcceptedVersion(kind, v) {
			// Unsupported version: clear authority.
			a.version = ""
			a.versionConfirmed = false
			return
		}
		if a.versionConfirmed && a.version != v {
			// Conflicting version: clear authority.
			a.version = ""
			a.versionConfirmed = false
			return
		}
		a.version = v
		a.versionConfirmed = true
	}
}

// updatePID tracks the discovered process PID. Not yet bound to
// launch binding (requires PID from process snapshots at call site).
var _ = 0 // keep for future PID binding

// setCursor stores the adapter continuation cursor.
func (a *adapterState) setCursor(cursor string) {
	a.adapterCursor = cursor
}

// cursor returns the stored adapter cursor.
func (a *adapterState) cursor() string {
	return a.adapterCursor
}

// launchCorrelation determines the correlation state using validated state.
func (a *adapterState) launchCorrelation(binding *transcript.LaunchBinding, kind string, discoveredPID int) contract.Correlation {
	if binding == nil {
		return contract.CorrelationUnavailable
	}
	if !a.versionConfirmed {
		return contract.CorrelationUnavailable
	}
	return transcript.LaunchCorrelation(binding, kind, a.version, discoveredPID)
}

// clear resets all state (called on session delete).
func (a *adapterState) clear() {
	*a = adapterState{}
}
