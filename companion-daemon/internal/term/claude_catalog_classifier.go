// Package term — C2D Catalog P1: strict nested tool_input decoder and
// single-entry catalog classifier for the frozen Claude Bash probe action.
//
// The classifier is a pure provider-private function. It receives the raw
// bounded tool_input JSON, provider, version and tool name. It returns
// either the exact catalog action ID plus the canonical input digest, or
// no match. It never returns the command, description, summary label, or
// partially decoded material.
//
// P1 is test-only: no runtime observation, Store, coordinator, or DTO is
// wired. P2 will connect this at the Claude provider boundary where
// tool_input is already in hand.
package term

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf8"
)

// catalogEntry is one frozen server-owned action entry in the Claude Bash
// approval catalog. The catalog is compiled at build time; every entry must
// have a unique CatalogActionID, Provider+Version+ToolName+Command tuple,
// and Summary.
type catalogEntry struct {
	CatalogActionID string
	Provider        string
	Version         string
	ToolName        string
	Command         string
	Summary         string
}

// certifiedClaudeCatalog is the frozen build-time catalog. The initial
// slice contains exactly one entry: the approval verification probe.
// Adding an entry requires an independent contract review.
var certifiedClaudeCatalog = []catalogEntry{
	{
		CatalogActionID: "claude.bash.approval_probe.v1",
		Provider:        "claude_headless",
		Version:         CertifiedClaudeVersion,
		ToolName:        "Bash",
		Command:         "echo pokitclaudeapprovalprobe",
		Summary:         "Run Claude approval verification probe",
	},
}

// strictDecodeToolInput decodes raw as a JSON object admitting only the
// frozen keys "command" (required, exactly once) and "description"
// (optional, at most once). It uses token-walking (dec.Token()), not
// json.Unmarshal into a map, so duplicate keys are rejected and unknown
// keys are rejected.
//
// On success it returns the extracted command and description strings.
// The raw bytes never escape the decoder.
func strictDecodeToolInput(raw []byte) (command, description string, ok bool) {
	if len(raw) == 0 || len(raw) > maxToolInputBytes {
		return "", "", false
	}
	// Reject invalid UTF-8 before the JSON decoder can substitute
	// replacement characters (U+FFFD) for ill-formed sequences.
	if !utf8.Valid(raw) {
		return "", "", false
	}

	dec := json.NewDecoder(bytes.NewReader(raw))

	// Opening '{'
	tok, err := dec.Token()
	if err != nil {
		return "", "", false
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return "", "", false
	}

	var sawCommand, sawDescription bool

	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return "", "", false
		}
		key, ok := kt.(string)
		if !ok {
			return "", "", false
		}

		switch key {
		case "command":
			if sawCommand {
				return "", "", false // duplicate
			}
			sawCommand = true

			var rawVal json.RawMessage
			if err := dec.Decode(&rawVal); err != nil {
				return "", "", false
			}
			s, ok := decodeStringValue(rawVal)
			if !ok || s == "" {
				return "", "", false
			}
			if !utf8.ValidString(s) {
				return "", "", false
			}
			command = s

		case "description":
			if sawDescription {
				return "", "", false // duplicate
			}
			sawDescription = true

			var rawVal json.RawMessage
			if err := dec.Decode(&rawVal); err != nil {
				return "", "", false
			}
			s, ok := decodeStringValue(rawVal)
			if !ok {
				return "", "", false
			}
			if len(s) > 512 {
				return "", "", false
			}
			if !utf8.ValidString(s) {
				return "", "", false
			}
			for _, r := range s {
				if r <= 0x1F || (r >= 0x7F && r <= 0x9F) {
					return "", "", false
				}
			}
			description = s

		default:
			return "", "", false // unknown key
		}
	}

	// Consume closing '}'
	if _, err := dec.Token(); err != nil {
		return "", "", false
	}

	// Reject trailing content
	if _, err := dec.Token(); err != io.EOF {
		return "", "", false
	}

	if !sawCommand {
		return "", "", false
	}

	return command, description, true
}

// decodeStringValue decodes raw as a JSON string. Rejects any other JSON
// type (number, boolean, array, object, null).
func decodeStringValue(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	// A JSON string always starts with '"'.
	if raw[0] != '"' {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

// classifyCatalogAction classifies one tool_input against the frozen
// catalog. It returns the catalog action ID and the canonical input digest
// on match, or zero values on no-match/rejection.
//
// The returned catalogActionID and inputDigest carry no raw provider text.
// The caller receives only enough identity to select a Pokit-owned static
// summary label.
func classifyCatalogAction(toolInput []byte, provider, version, toolName string) (catalogActionID string, inputDigest string, ok bool) {
	command, _, strictOK := strictDecodeToolInput(toolInput)
	if !strictOK {
		return "", "", false
	}

	canon, err := canonicalJSON(json.RawMessage(toolInput))
	if err != nil || len(canon) > maxToolInputBytes {
		return "", "", false
	}
	inputDigest = sha256Hex(canon)

	for i := range certifiedClaudeCatalog {
		e := &certifiedClaudeCatalog[i]
		if e.Provider == provider && e.Version == version && e.ToolName == toolName && e.Command == command {
			return e.CatalogActionID, inputDigest, true
		}
	}

	return "", "", false
}

// validateCatalog reports whether a catalog slice satisfies every uniqueness
// invariant required by the planner contract:
//
//   - CatalogActionID must be non-empty and unique.
//   - Summary must be non-empty and unique (no two entries share the same
//     public label).
//   - The provider+version+tool+command tuple must be unique (no two
//     entries resolve to the same concrete action).
//   - No required field may be empty.
//
// The function accepts an explicit slice so tests can inject known-bad
// duplicates.
func validateCatalog(entries []catalogEntry) error {
	seenID := make(map[string]int)      // CatalogActionID → index
	seenSummary := make(map[string]int) // Summary → index
	seenTuple := make(map[string]int)   // provider|version|tool|command → index

	for i := range entries {
		e := &entries[i]

		if e.CatalogActionID == "" {
			return fmt.Errorf("catalog entry %d: empty CatalogActionID", i)
		}
		if e.Provider == "" || e.Version == "" || e.ToolName == "" || e.Command == "" || e.Summary == "" {
			return fmt.Errorf("catalog entry %d: empty required field", i)
		}

		if prev, dup := seenID[e.CatalogActionID]; dup {
			return fmt.Errorf("catalog entry %d: duplicate CatalogActionID %q (first at %d)", i, e.CatalogActionID, prev)
		}
		seenID[e.CatalogActionID] = i

		if prev, dup := seenSummary[e.Summary]; dup {
			return fmt.Errorf("catalog entry %d: duplicate Summary %q (first at %d)", i, e.Summary, prev)
		}
		seenSummary[e.Summary] = i

		tuple := e.Provider + "|" + e.Version + "|" + e.ToolName + "|" + e.Command
		if prev, dup := seenTuple[tuple]; dup {
			return fmt.Errorf("catalog entry %d: duplicate provider/version/tool/command tuple %q (first at %d)", i, tuple, prev)
		}
		seenTuple[tuple] = i
	}
	return nil
}

// selectCatalogActionID is the pure digest cross-check called by handleHook.
// It returns the catalog action ID only when the classifier matched AND the
// classifier's canonical digest equals the bridge's independently computed
// canonical digest. This prevents a tampered classifier output from injecting
// a false catalog ID.
func selectCatalogActionID(catalogActionID, classifierDigest, bridgeDigest string, matched bool) string {
	if !matched || catalogActionID == "" {
		return ""
	}
	if classifierDigest != bridgeDigest {
		return ""
	}
	return catalogActionID
}

// validCatalogBinding reports whether a non-empty catalogActionID is
// consistent with the given provider and version. Returns false when the
// ID is unknown or the entry's Provider/Version do not match. An empty
// catalogActionID is always valid (non-catalog observation).
func validCatalogBinding(catalogActionID, provider, version string) bool {
	if catalogActionID == "" {
		return true
	}
	entry, found := lookupCatalogEntry(catalogActionID)
	if !found {
		return false
	}
	return entry.Provider == provider && entry.Version == version
}

// catalogSummary returns the Pokit-owned static summary for a catalog
// action ID, or empty string if the ID is unknown. Used by the safe-DTO
// projector (P2B) to select a display label without exposing raw provider text.
func catalogSummary(catalogActionID string) string {
	entry, found := lookupCatalogEntry(catalogActionID)
	if !found {
		return ""
	}
	return entry.Summary
}

// lookupCatalogEntry returns a value copy of the catalog entry for a given
// action ID. The copy prevents callers from mutating the frozen compiled
// catalog through a pointer. Returns false if not found.
func lookupCatalogEntry(catalogActionID string) (catalogEntry, bool) {
	for i := range certifiedClaudeCatalog {
		if certifiedClaudeCatalog[i].CatalogActionID == catalogActionID {
			return certifiedClaudeCatalog[i], true
		}
	}
	return catalogEntry{}, false
}

func init() {
	if err := validateCatalog(certifiedClaudeCatalog); err != nil {
		panic("certifiedClaudeCatalog: " + err.Error())
	}
}
