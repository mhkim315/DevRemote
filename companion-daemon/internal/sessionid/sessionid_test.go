package sessionid

import (
	"errors"
	"strings"
	"testing"
)

// ── PA2b focused test 1: canonical positive cases ──

func TestParseSessionID_CanonicalPositive(t *testing.T) {
	cases := []struct {
		id      string
		adapter string
		localID string
	}{
		{"cmux:devremote", "cmux", "devremote"},
		{"cmux:surface:1", "cmux", "surface:1"}, // local IDs may contain ':'
		{"controlled_pty:sess-1", "controlled_pty", "sess-1"},
		{"codex_app_server:abc", "codex_app_server", "abc"},
		{"claude_headless:세션", "claude_headless", "세션"}, // Unicode local ID
		{"cmux: spaced ", "cmux", " spaced "},
	}
	for _, tc := range cases {
		ref := ParseSessionID(tc.id)
		if ref.Adapter != tc.adapter || ref.LocalID != tc.localID {
			t.Errorf("ParseSessionID(%q) = %+v, want {%s %s}", tc.id, ref, tc.adapter, tc.localID)
		}
		if err := ref.Validate(); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", tc.id, err)
		}
	}
}

// ── PA2b focused test 1: malformed IDs, empty adapter/local ID ──

func TestValidate_MalformedAndEmpty(t *testing.T) {
	cases := []struct {
		name string
		id   string
	}{
		{"no colon (empty adapter)", "plain"},
		{"empty string", ""},
		{"leading colon (empty adapter)", ":local"},
		{"trailing colon (empty local)", "cmux:"},
		{"only colon", ":"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ref := ParseSessionID(tc.id)
			err := ref.Validate()
			if err == nil {
				t.Fatalf("Validate(%q) = nil, want error", tc.id)
			}
			if !errors.Is(err, ErrInvalidSessionID) {
				t.Errorf("Validate(%q) error not ErrInvalidSessionID: %v", tc.id, err)
			}
		})
	}
}

// ── PA2b focused test 1: control characters ──

func TestValidate_ControlCharacters(t *testing.T) {
	cases := []SessionRef{
		{Adapter: "tm\nux", LocalID: "x"},
		{Adapter: "cmux", LocalID: "a\tb"},
		{Adapter: "cmux", LocalID: "a\x00b"},
		{Adapter: "\x1b[31m", LocalID: "x"},
		{Adapter: "cmux", LocalID: "del\x7f"},
	}
	for _, ref := range cases {
		err := ref.Validate()
		if err == nil {
			t.Errorf("Validate(%+v) = nil, want control-character error", ref)
			continue
		}
		if !errors.Is(err, ErrInvalidSessionID) {
			t.Errorf("Validate(%+v) error not ErrInvalidSessionID: %v", ref, err)
		}
		if !strings.Contains(err.Error(), "control character") {
			t.Errorf("Validate(%+v) error lacks control-character detail: %v", ref, err)
		}
	}
}

// ── PA2b focused test 1: non-canonical serialization ──

func TestCanonical_NonCanonicalForms(t *testing.T) {
	// Empty adapter serializes to the bare local ID (degenerate form).
	if got := (SessionRef{LocalID: "bare"}).Canonical(); got != "bare" {
		t.Errorf("empty-adapter Canonical() = %q, want %q", got, "bare")
	}
	// A parsed non-canonical input does not round-trip to a DIFFERENT string:
	// parse→canonical of "plain" (no colon) yields "plain" again.
	if got := ParseSessionID("plain").Canonical(); got != "plain" {
		t.Errorf("ParseSessionID(plain).Canonical() = %q, want plain", got)
	}
	// String() is exactly Canonical().
	ref := SessionRef{Adapter: "cmux", LocalID: "a:b"}
	if ref.String() != ref.Canonical() {
		t.Errorf("String() = %q != Canonical() = %q", ref.String(), ref.Canonical())
	}
}

// ── PA2b focused test 1: invalid adapter grammar ──

func TestValidateAdapterName_Grammar(t *testing.T) {
	valid := []string{"cmux", "cmux", "controlled_pty", "codex_app_server", "a", "a0", "a-b_c9"}
	for _, name := range valid {
		if err := ValidateAdapterName(name); err != nil {
			t.Errorf("ValidateAdapterName(%q) = %v, want nil", name, err)
		}
	}
	invalid := []string{"", "Cmux", "0cmux", "-cmux", "_cmux", "tm ux", "tm:ux", "tmüx", "cmux!", "INVALID_UPPER"}
	for _, name := range invalid {
		err := ValidateAdapterName(name)
		if err == nil {
			t.Errorf("ValidateAdapterName(%q) = nil, want error", name)
			continue
		}
		if !errors.Is(err, ErrInvalidSessionID) {
			t.Errorf("ValidateAdapterName(%q) error not ErrInvalidSessionID: %v", name, err)
		}
	}
}

// ── PA2b focused test 1: parse/string round trip ──

func TestParseString_RoundTrip(t *testing.T) {
	ids := []string{
		"cmux:devremote",
		"cmux:surface:1",
		"controlled_pty:with:many:colons",
		"claude_headless:유니코드-로컬",
		"cmux:trailing:",
		"a:b",
	}
	for _, id := range ids {
		ref := ParseSessionID(id)
		if got := ref.String(); got != id {
			t.Errorf("round trip %q → %+v → %q", id, ref, got)
		}
		// Second parse of the serialization is structurally identical.
		if again := ParseSessionID(ref.String()); again != ref {
			t.Errorf("re-parse %q = %+v, want %+v", ref.String(), again, ref)
		}
	}
}
