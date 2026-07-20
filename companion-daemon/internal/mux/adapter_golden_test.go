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
		// cmux sessions
		{input: "cmux:ai", adapter: "cmux", localID: "ai", migrated: "cmux:ai"},
		{input: "cmux:aider", adapter: "cmux", localID: "aider", migrated: "cmux:aider"},
		{input: "cmux:cmux-1", adapter: "cmux", localID: "cmux-1", migrated: "cmux:cmux-1"},
		{input: "cmux:____", adapter: "cmux", localID: "____", migrated: "cmux:____"},

		// Local ID containing ':' (adapter=cmux, localID="cmux:aider")
		{input: "cmux:cmux:aider", adapter: "cmux", localID: "cmux:aider", migrated: "cmux:cmux:aider"},
		{input: "cmux:session:with:colons", adapter: "cmux", localID: "session:with:colons", migrated: "cmux:session:with:colons"},

		// Unicode local ID
		{input: "cmux:한글", adapter: "cmux", localID: "한글", migrated: "cmux:한글"},
		{input: "cmux:セッション", adapter: "cmux", localID: "セッション", migrated: "cmux:セッション"},

		// cmux sessions (canonical)
		{input: "cmux:surface:1", adapter: "cmux", localID: "surface:1", migrated: "cmux:surface:1"},
		{input: "cmux:surface:42", adapter: "cmux", localID: "surface:42", migrated: "cmux:surface:42"},

		// Legacy cmux — ParseSessionID returns numeric localID; MigrateLegacyID canonicalizes
		{input: "cmux:42", adapter: "cmux", localID: "42", migrated: "cmux:surface:42"},
		{input: "cmux:1", adapter: "cmux", localID: "1", migrated: "cmux:surface:1"},
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
		{"cmux:aider", "cmux", "aider"},
		{"cmux:cmux-1", "cmux", "cmux-1"},
		{"cmux:session:with:colons", "cmux", "session:with:colons"},
		{"cmux:cmux:aider", "cmux", "cmux:aider"}, // local part itself has ':'
		{"cmux:surface:42", "cmux", "surface:42"},
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
		"cmux:한글",
		"cmux:セッション",
		"cmux:中文",
		"cmux:emoji_🎉",
	}
	for _, id := range tests {
		t.Run(id, func(t *testing.T) {
			ref := ParseSessionID(id)
			if ref.LocalID == "" {
				t.Errorf("RawID is empty for %q", id)
			}
			migrated := MigrateLegacyID(id)
			// Unicode IDs are not migrated (not legacy cmux format).
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
