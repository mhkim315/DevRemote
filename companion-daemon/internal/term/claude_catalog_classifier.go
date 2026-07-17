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
		Version:         "2.1.209",
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

// catalogSummary returns the Pokit-owned static summary for a catalog
// action ID and provider/version tuple, or the generic fallback.
func catalogSummary(provider, version, catalogActionID string) string {
	for i := range certifiedClaudeCatalog {
		e := &certifiedClaudeCatalog[i]
		if e.Provider == provider && e.Version == version && e.CatalogActionID == catalogActionID {
			return e.Summary
		}
	}
	return "" // caller uses generic fallback
}

// validateCatalog reports whether the compiled catalog satisfies the
// uniqueness invariant. Called from init() or tests.
func validateCatalog() error {
	seen := make(map[string]bool)
	for i := range certifiedClaudeCatalog {
		e := &certifiedClaudeCatalog[i]
		if e.CatalogActionID == "" {
			return fmt.Errorf("catalog entry %d: empty CatalogActionID", i)
		}
		if e.Provider == "" || e.Version == "" || e.ToolName == "" || e.Command == "" || e.Summary == "" {
			return fmt.Errorf("catalog entry %d: empty required field", i)
		}
		// CatalogActionID must be unique.
		if seen[e.CatalogActionID] {
			return fmt.Errorf("catalog entry %d: duplicate CatalogActionID %q", i, e.CatalogActionID)
		}
		seen[e.CatalogActionID] = true
	}
	return nil
}
