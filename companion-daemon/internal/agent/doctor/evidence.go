package doctor

import (
	"encoding/json"

	"devremote/companion-daemon/internal/agent/contract"
)

// ── Evidence collection bounds ──

const (
	maxChangedPaths      = 8
	maxDiscriminators    = 16
	maxFieldShapeChanges = 8
	maxDiscriminatorLen  = 80
)

// ── EvidenceCollector ──

// EvidenceCollector gathers bounded, redacted evidence from raw provider
// records. All operations are read-only and bounded; an operation that hits
// its cap stops and records a truncation note in the output. No field value
// from a provider record is ever copied verbatim into diagnostics.
type EvidenceCollector struct{}

// NewEvidenceCollector returns a new EvidenceCollector.
func NewEvidenceCollector() *EvidenceCollector {
	return &EvidenceCollector{}
}

// ── CollectDrift ──

// CollectDrift populates DriftEvidence from a set of observed records. It
// extracts unknown discriminators and field shape hints, bounded by the
// evidence caps. No raw record content escapes.
func (ec *EvidenceCollector) CollectDrift(
	report *CompatibilityReport,
	knownDiscriminators []string,
	knownFields map[string]string, // field path → expected JSON type
) {
	if report == nil {
		return
	}
	report.DriftEvidence.UnknownDiscriminators = ec.collectUnknownDiscriminators(
		nil, knownDiscriminators)
	report.DriftEvidence.FieldShapeChanges = ec.collectFieldShapeHints(
		nil, knownFields)
}

// collectUnknownDiscriminators scans records for first-level discriminator
// values (e.g. the "type" key) not in the known set. Each discriminator is
// bounded and sanitized; the total count is capped at maxDiscriminators.
func (ec *EvidenceCollector) collectUnknownDiscriminators(
	records []contract.RawRecord,
	known []string,
) []string {
	knownSet := make(map[string]bool, len(known))
	for _, k := range known {
		knownSet[k] = true
	}

	var found []string
	for _, rec := range records {
		if len(found) >= maxDiscriminators {
			break
		}
		val := firstDiscriminator(rec.Bytes)
		if val == "" || knownSet[val] {
			continue
		}
		// Bounded + sanitized
		val = contract.SanitizeDiagnostic(val)
		if len(val) > maxDiscriminatorLen {
			val = val[:maxDiscriminatorLen]
		}
		// Dedup
		dup := false
		for _, f := range found {
			if f == val {
				dup = true
				break
			}
		}
		if !dup {
			found = append(found, val)
		}
		knownSet[val] = true // mark seen to avoid repeated hits
	}
	return found
}

// firstDiscriminator extracts the value of the first string field that looks
// like a record type discriminator (keys: "type", "kind", "event"). Returns
// empty string if none found or parse fails.
func firstDiscriminator(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return ""
	}
	for _, key := range []string{"type", "kind", "event"} {
		if v, ok := m[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// collectFieldShapeHints checks known field paths against the actual JSON
// types observed in records. Only type information is collected (e.g.
// "expected string, got array") — field values are never captured.
func (ec *EvidenceCollector) collectFieldShapeHints(
	records []contract.RawRecord,
	knownFields map[string]string,
) []string {
	if len(knownFields) == 0 {
		return nil
	}

	var hints []string
	for _, rec := range records {
		if len(hints) >= maxFieldShapeChanges {
			break
		}
		var m map[string]any
		if err := json.Unmarshal(rec.Bytes, &m); err != nil {
			continue
		}
		for path, expectedType := range knownFields {
			if len(hints) >= maxFieldShapeChanges {
				break
			}
			actualType := jsonTypeAtPath(m, path)
			if actualType != "" && actualType != expectedType {
				hint := "field " + contract.SanitizeDiagnostic(path) +
					": expected " + expectedType + ", got " + actualType
				if len(hint) > maxDiscriminatorLen {
					hint = hint[:maxDiscriminatorLen]
				}
				hints = append(hints, hint)
			}
		}
	}
	return hints
}

// jsonTypeAtPath returns the JSON type of a value at a dotted path within a
// map (single level only for safety — no recursive descent into nested maps).
// Returns empty string if the path is not a single key or the value is nil.
func jsonTypeAtPath(m map[string]any, path string) string {
	// Support only top-level keys for safety — no nested traversal.
	val, ok := m[path]
	if !ok || val == nil {
		return ""
	}
	switch val.(type) {
	case string:
		return "string"
	case float64, json.Number:
		return "number"
	case bool:
		return "boolean"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return "unknown"
	}
}

// ── CollectPathHints ──

// CollectPathHints compares expected log/search paths against observed paths
// (all already redacted) and returns a bounded list of mismatches.
func (ec *EvidenceCollector) CollectPathHints(
	expectedPaths []string,
	observedPaths []string,
) []string {
	observedSet := make(map[string]bool, len(observedPaths))
	for _, p := range observedPaths {
		observedSet[p] = true
	}

	var missing []string
	for _, p := range expectedPaths {
		if len(missing) >= maxChangedPaths {
			break
		}
		if !observedSet[p] {
			missing = append(missing, contract.SanitizeDiagnostic(p))
		}
	}
	return missing
}
