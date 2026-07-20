package term

import (
	"strings"
	"testing"
)

// PA2b focused test 4 (approval regression): the approval boundary validators
// stayed in this package and preserve ALL accepted bounds byte-for-byte after
// delegating their identity primitives to internal/sessionid: valid UTF-8,
// length bounds, non-empty adapter/local id, no control characters, canonical
// round trip, adapter grammar. Provider-origin, authority-version, adapter,
// and launch-generation binding are unchanged and remain covered by the
// existing approval suites (approval_store_gen_test.go,
// approval_delivery_r9a/r9b_test.go, managed_approval_test.go).

func TestPA2b_ApprovalValidSessionID_BoundsPreserved(t *testing.T) {
	accepted := []string{
		"controlled_pty:abc",
		"codex_app_server:sess-1",
		"claude_headless:세션",            // Unicode local id stays accepted
		"legacy:with:inner:colons",      // local id may contain ':'
		"a:" + strings.Repeat("x", 510), // exactly 512 bytes total
	}
	for _, s := range accepted {
		if !validSessionID(s) {
			t.Errorf("validSessionID(%q) = false, want true (accepted identity regressed)", s)
		}
	}

	rejected := []struct {
		name string
		id   string
	}{
		{"empty", ""},
		{"no adapter", "plain"},
		{"empty adapter", ":local"},
		{"empty local", "legacy:"},
		{"control char in local", "legacy:a\x00b"},
		{"newline in adapter", "tm\nux:x"},
		{"invalid UTF-8", "legacy:a\xffb"},
		{"adapter grammar: upper", "Legacy:x"},
		{"adapter grammar: leading digit", "0legacy:x"},
		{"adapter grammar: space", "tm ux:x"},
		{"over 512 bytes", "a:" + strings.Repeat("x", 511)},
	}
	for _, tc := range rejected {
		if validSessionID(tc.id) {
			t.Errorf("validSessionID(%s %q) = true, want false (bound weakened)", tc.name, tc.id)
		}
	}
}

func TestPA2b_ApprovalValidAdapterID_BoundsPreserved(t *testing.T) {
	accepted := []string{"controlled_pty", "codex_app_server", "claude_headless", "a", strings.Repeat("a", 64)}
	for _, s := range accepted {
		if !validAdapterID(s) {
			t.Errorf("validAdapterID(%q) = false, want true (accepted adapter regressed)", s)
		}
	}
	rejected := []string{"", "Legacy", "0x", "-x", "_x", "tm:ux", "tm ux", "tmüx", strings.Repeat("a", 65)}
	for _, s := range rejected {
		if validAdapterID(s) {
			t.Errorf("validAdapterID(%q) = true, want false (bound weakened)", s)
		}
	}
}
