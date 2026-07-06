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
		// tmux sessions
		{input: "tmux:ai", adapter: "tmux", localID: "ai", migrated: "tmux:ai"},
		{input: "tmux:aider", adapter: "tmux", localID: "aider", migrated: "tmux:aider"},
		{input: "tmux:cmux-1", adapter: "tmux", localID: "cmux-1", migrated: "tmux:cmux-1"},
		{input: "tmux:____", adapter: "tmux", localID: "____", migrated: "tmux:____"},

		// Local ID containing ':' (adapter=tmux, localID="tmux:aider")
		{input: "tmux:tmux:aider", adapter: "tmux", localID: "tmux:aider", migrated: "tmux:tmux:aider"},
		{input: "tmux:session:with:colons", adapter: "tmux", localID: "session:with:colons", migrated: "tmux:session:with:colons"},

		// Unicode local ID
		{input: "tmux:한글", adapter: "tmux", localID: "한글", migrated: "tmux:한글"},
		{input: "tmux:セッション", adapter: "tmux", localID: "セッション", migrated: "tmux:セッション"},

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
		{"tmux:aider", "tmux", "aider"},
		{"tmux:cmux-1", "tmux", "cmux-1"},
		{"tmux:session:with:colons", "tmux", "session:with:colons"},
		{"tmux:tmux:aider", "tmux", "tmux:aider"}, // local part itself has ':'
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
		"tmux:한글",
		"tmux:セッション",
		"tmux:中文",
		"tmux:emoji_🎉",
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
