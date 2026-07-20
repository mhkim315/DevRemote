package mux

import (
	"strings"
	"testing"
)

// Phase 0 golden fixtures: canonical ID format stability.
// These tests lock in the current behavior so that Phase 1-7 changes
// cannot accidentally break the external API contract.

func TestCanonicalID_Golden(t *testing.T) {
	tests := []struct {
		input    string
		adapter  string
		localID  string
		migrated string // MigrateLegacyID result
	}{
		// ext adapter sessions
		{input: "ext:ai", adapter: "ext", localID: "ai", migrated: "ext:ai"},
		{input: "ext:aider", adapter: "ext", localID: "aider", migrated: "ext:aider"},
		{input: "ext:ext-1", adapter: "ext", localID: "ext-1", migrated: "ext:ext-1"},
		{input: "ext:____", adapter: "ext", localID: "____", migrated: "ext:____"},

		// Local ID containing ':' (adapter=ext, localID="ext:aider")
		{input: "ext:ext:aider", adapter: "ext", localID: "ext:aider", migrated: "ext:ext:aider"},
		{input: "ext:session:with:colons", adapter: "ext", localID: "session:with:colons", migrated: "ext:session:with:colons"},

		// Unicode local ID
		{input: "ext:한글", adapter: "ext", localID: "한글", migrated: "ext:한글"},
		{input: "ext:セッション", adapter: "ext", localID: "セッション", migrated: "ext:セッション"},

		// ext adapter sessions (canonical)
		{input: "ext:1", adapter: "ext", localID: "1", migrated: "ext:1"},
		{input: "ext:42", adapter: "ext", localID: "42", migrated: "ext:42"},

		// Legacy ext — ParseSessionID returns numeric localID; MigrateLegacyID canonicalizes
		{input: "ext:42", adapter: "ext", localID: "42", migrated: "ext:42"},
		{input: "ext:1", adapter: "ext", localID: "1", migrated: "ext:1"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			ref := ParseSessionID(tt.input)
			if ref.Adapter != tt.adapter {
				t.Errorf("ParseSessionID(%q).Adapter = %q, want %q", tt.input, ref.Adapter, tt.adapter)
			}
			if ref.LocalID != tt.localID {
				t.Errorf("ParseSessionID(%q).RawID = %q, want %q", tt.input, ref.LocalID, tt.localID)
			}

			migrated := MigrateLegacyID(tt.input)
			if migrated != tt.migrated {
				t.Errorf("MigrateLegacyID(%q) = %q, want %q", tt.input, migrated, tt.migrated)
			}

			// Canonical ID round-trip: migrate → parse → reassemble.
			canonical := MigrateLegacyID(tt.input)
			ref2 := ParseSessionID(canonical)
			reassembled := ref2.Adapter + ":" + ref2.LocalID
			if reassembled != canonical {
				t.Errorf("round-trip: %q → MigrateLegacyID → %q → ParseSessionID → reassemble %q, want %q",
					tt.input, canonical, reassembled, canonical)
			}
		})
	}
}

func TestCanonicalID_LocalIDWithColon(t *testing.T) {
	// Verify local IDs containing ':' are correctly identified.
	// Only the FIRST ':' splits adapter from local ID.
	tests := []struct {
		input   string
		adapter string
		localID string
	}{
		{"ext:aider", "ext", "aider"},
		{"ext:ext-1", "ext", "ext-1"},
		{"ext:session:with:colons", "ext", "session:with:colons"},
		{"ext:ext:aider", "ext", "ext:aider"}, // local part itself has ':'
		{"ext:42", "ext", "42"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			ref := ParseSessionID(tt.input)
			if ref.Adapter != tt.adapter {
				t.Errorf("adapter = %q, want %q", ref.Adapter, tt.adapter)
			}
			if ref.LocalID != tt.localID {
				t.Errorf("localID = %q, want %q", ref.LocalID, tt.localID)
			}
			if strings.Contains(ref.Adapter, ":") {
				t.Errorf("adapter %q contains colon", ref.Adapter)
			}
		})
	}
}

func TestCanonicalID_Unicode(t *testing.T) {
	tests := []string{
		"ext:한글",
		"ext:セッション",
		"ext:中文",
		"ext:emoji_🎉",
	}
	for _, id := range tests {
		t.Run(id, func(t *testing.T) {
			ref := ParseSessionID(id)
			if ref.LocalID == "" {
				t.Errorf("RawID is empty for %q", id)
			}
			migrated := MigrateLegacyID(id)
			// Unicode IDs are not migrated (not legacy ext format).
			if migrated != id {
				t.Errorf("Unicode ID %q was unexpectedly migrated to %q", id, migrated)
			}
		})
	}
}

func TestCanonicalID_EdgeCases(t *testing.T) {
	neverPanic := []string{
		"", ":", "only-adapter:", ":only-local",
		"no-colon", "a:b:c:d:e",
	}
	for _, id := range neverPanic {
		t.Run(id, func(t *testing.T) {
			_ = ParseSessionID(id)
			_ = MigrateLegacyID(id)
		})
	}
}
