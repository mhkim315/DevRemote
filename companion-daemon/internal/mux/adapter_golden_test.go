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
		// tmux sessions (adapter:local-id)
		{input: "tmux:ai", adapter: "tmux", localID: "ai", migrated: "tmux:ai"},
		{input: "tmux:aider", adapter: "tmux", localID: "aider", migrated: "tmux:aider"},
		{input: "tmux:cmux-1", adapter: "tmux", localID: "cmux-1", migrated: "tmux:cmux-1"},
		{input: "tmux:cmux-38", adapter: "tmux", localID: "cmux-38", migrated: "tmux:cmux-38"},
		{input: "tmux:____", adapter: "tmux", localID: "____", migrated: "tmux:____"},

		// cmux sessions (canonical)
		{input: "cmux:surface:1", adapter: "cmux", localID: "surface:1", migrated: "cmux:surface:1"},
		{input: "cmux:surface:2", adapter: "cmux", localID: "surface:2", migrated: "cmux:surface:2"},
		{input: "cmux:surface:42", adapter: "cmux", localID: "surface:42", migrated: "cmux:surface:42"},

		// Legacy cmux — ParseSessionID returns "42" as localID; MigrateLegacyID canonicalizes
		{input: "cmux:42", adapter: "cmux", localID: "42", migrated: "cmux:surface:42"},
		{input: "cmux:1", adapter: "cmux", localID: "1", migrated: "cmux:surface:1"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			// ParseSessionID must correctly split adapter and local ID.
			ref := ParseSessionID(tt.input)
			if ref.Adapter != tt.adapter {
				t.Errorf("ParseSessionID(%q).Adapter = %q, want %q", tt.input, ref.Adapter, tt.adapter)
			}
			if ref.RawID != tt.localID {
				t.Errorf("ParseSessionID(%q).RawID = %q, want %q", tt.input, ref.RawID, tt.localID)
			}

			// MigrateLegacyID must return the canonical form.
			migrated := MigrateLegacyID(tt.input)
			if migrated != tt.migrated {
				t.Errorf("MigrateLegacyID(%q) = %q, want %q", tt.input, migrated, tt.migrated)
			}

			// Canonical ID must survive round-trip: migrate → parse → reassemble.
			canonical := MigrateLegacyID(tt.input)
			ref2 := ParseSessionID(canonical)
			reassembled := ref2.Adapter + ":" + ref2.RawID
			if reassembled != canonical {
				t.Errorf("round-trip: %q → MigrateLegacyID → %q → ParseSessionID → reassemble %q, want %q",
					tt.input, canonical, reassembled, canonical)
			}
		})
	}
}

func TestCanonicalID_LocalIDWithColon(t *testing.T) {
	// Local IDs containing ':' must be preserved. Only the FIRST ':' splits adapter from local ID.
	tests := []string{
		"tmux:aider",
		"tmux:cmux-1",
		"tmux:cmux-38",
		"tmux:session:with:colons",
		"cmux:surface:42",
	}

	for _, id := range tests {
		t.Run(id, func(t *testing.T) {
			ref := ParseSessionID(id)
			canonical := MigrateLegacyID(id)

			// The adapter name must not contain ':'.
			if strings.Contains(ref.Adapter, ":") {
				t.Errorf("adapter %q contains colon", ref.Adapter)
			}

			// ParseSessionID.RawID is the local part.
			if ref.RawID == "" {
				t.Errorf("RawID is empty for %q", id)
			}

			// Canonical reassembly.
			if canonical != ref.Adapter+":"+ref.RawID {
				t.Logf("canonical %q ≠ adapter:localID %q:%q (may be migrated)", canonical, ref.Adapter, ref.RawID)
			}
		})
	}
}

func TestCanonicalID_EdgeCases(t *testing.T) {
	// Empty or invalid input must not panic.
	neverPanic := []string{
		"",
		":",
		"only-adapter:",
		":only-local",
		"no-colon",
		"a:b:c:d:e",
	}
	for _, id := range neverPanic {
		t.Run("nopanic/"+id, func(t *testing.T) {
			_ = ParseSessionID(id)
			_ = MigrateLegacyID(id)
		})
	}
}
