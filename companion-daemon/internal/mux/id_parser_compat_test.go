package mux

import (
	"errors"
	"os"
	"strings"
	"testing"

	"devremote/companion-daemon/internal/sessionid"
)

// PA2b focused test 2: existing mux callers receive IDENTICAL results through
// the thin aliases/wrappers in id_parser.go.

func TestPA2b_WrapperCompat_IdenticalResults(t *testing.T) {
	ids := []string{
		"cmux:devremote",
		"cmux:surface:1",
		"controlled_pty:with:many:colons",
		"claude_headless:세션",
		"plain",
		"",
		":",
		"cmux:",
		":local",
		"tm\nux:x",
		"cmux:a\x00b",
	}
	for _, id := range ids {
		viaMux := ParseSessionID(id)
		direct := sessionid.ParseSessionID(id)
		if viaMux != direct {
			t.Errorf("ParseSessionID(%q): mux %+v != sessionid %+v", id, viaMux, direct)
		}
		if viaMux.Canonical() != direct.Canonical() || viaMux.String() != direct.String() {
			t.Errorf("serialization mismatch for %q", id)
		}
		mErr, sErr := viaMux.Validate(), direct.Validate()
		if (mErr == nil) != (sErr == nil) {
			t.Errorf("Validate(%q): mux err %v, sessionid err %v", id, mErr, sErr)
		}
		if mErr != nil && sErr != nil && mErr.Error() != sErr.Error() {
			t.Errorf("Validate(%q) message drift: %q vs %q", id, mErr, sErr)
		}
	}

	names := []string{"cmux", "controlled_pty", "a-b_c9", "", "Cmux", "0x", "cm:ux", "cmüx"}
	for _, name := range names {
		mErr, sErr := ValidateAdapterName(name), sessionid.ValidateAdapterName(name)
		if (mErr == nil) != (sErr == nil) {
			t.Errorf("ValidateAdapterName(%q): mux err %v, sessionid err %v", name, mErr, sErr)
		}
		if mErr != nil && sErr != nil && mErr.Error() != sErr.Error() {
			t.Errorf("ValidateAdapterName(%q) message drift: %q vs %q", name, mErr, sErr)
		}
	}
}

// PA2b: the sentinel is ONE value observable through either package name, so
// errors.Is matches across the boundary in both directions.
func TestPA2b_WrapperCompat_SentinelIdentity(t *testing.T) {
	if ErrInvalidSessionID != sessionid.ErrInvalidSessionID {
		t.Fatal("mux.ErrInvalidSessionID is not the sessionid sentinel value")
	}
	err := SessionRef{}.Validate()
	if !errors.Is(err, ErrInvalidSessionID) || !errors.Is(err, sessionid.ErrInvalidSessionID) {
		t.Fatalf("Validate error does not match sentinel through both names: %v", err)
	}
	gErr := ValidateAdapterName("")
	if !errors.Is(gErr, ErrInvalidSessionID) || !errors.Is(gErr, sessionid.ErrInvalidSessionID) {
		t.Fatalf("grammar error does not match sentinel through both names: %v", gErr)
	}
}

// PA2b: mux.SessionRef is a TYPE ALIAS of sessionid.SessionRef (not a copy),
// so values are interchangeable with zero conversion.
func TestPA2b_WrapperCompat_TypeAlias(t *testing.T) {
	var viaMux SessionRef = sessionid.SessionRef{Adapter: "cmux", LocalID: "x"}
	var direct sessionid.SessionRef = SessionRef{Adapter: "cmux", LocalID: "x"}
	if viaMux != direct {
		t.Fatal("alias values differ")
	}
}

// PA2b focused test 3: MigrateLegacyID behavior unchanged AND remains in
// internal/mux (registry.go), not moved to the neutral package.
func TestPA2b_MigrateLegacyID_UnchangedAndStaysInMux(t *testing.T) {
	cases := []struct{ in, want string }{
		{"cmux:3", "cmux:surface:3"},         // legacy bare-numeric cmux id migrates
		{"cmux:12", "cmux:surface:12"},       // multi-digit
		{"cmux:surface:3", "cmux:surface:3"}, // already canonical: unchanged
		{"cmux:abc", "cmux:abc"},             // non-numeric local id: unchanged
		{"cmux:3", "cmux:surface:3"},                 // other adapters: unchanged
		{"plain", "plain"},                   // no adapter: unchanged
		{"", ""},
	}
	for _, tc := range cases {
		if got := MigrateLegacyID(tc.in); got != tc.want {
			t.Errorf("MigrateLegacyID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	// Location proof: the implementation stays in internal/mux/registry.go
	// (tests run with the package directory as CWD).
	src, err := os.ReadFile("registry.go")
	if err != nil {
		t.Fatalf("read registry.go: %v", err)
	}
	if !strings.Contains(string(src), "func MigrateLegacyID(") {
		t.Fatal("MigrateLegacyID no longer defined in internal/mux/registry.go")
	}
	if sidSrc, err := os.ReadFile("../sessionid/sessionid.go"); err == nil {
		if strings.Contains(string(sidSrc), "func MigrateLegacyID(") {
			t.Fatal("MigrateLegacyID leaked into internal/sessionid (must stay in mux)")
		}
	} else {
		t.Fatalf("read ../sessionid/sessionid.go: %v", err)
	}
}
