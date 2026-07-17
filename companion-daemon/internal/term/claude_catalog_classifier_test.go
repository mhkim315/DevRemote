package term

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

// ── Golden tests ──

func TestStrictDecode_ExactCommandOnly(t *testing.T) {
	raw := []byte(`{"command":"echo pokitclaudeapprovalprobe"}`)
	cmd, desc, ok := strictDecodeToolInput(raw)
	if !ok {
		t.Fatal("expected decode success")
	}
	if cmd != "echo pokitclaudeapprovalprobe" {
		t.Fatalf("command = %q", cmd)
	}
	if desc != "" {
		t.Fatalf("description = %q, want empty", desc)
	}
}

func TestStrictDecode_ExactCommandWithDescription(t *testing.T) {
	raw := []byte(`{"command":"echo pokitclaudeapprovalprobe","description":"Verify the approval probe works"}`)
	cmd, desc, ok := strictDecodeToolInput(raw)
	if !ok {
		t.Fatal("expected decode success")
	}
	if cmd != "echo pokitclaudeapprovalprobe" {
		t.Fatalf("command = %q", cmd)
	}
	if desc != "Verify the approval probe works" {
		t.Fatalf("description = %q", desc)
	}
}

func TestStrictDecode_OrderIndependence(t *testing.T) {
	// description before command — still valid.
	raw := []byte(`{"description":"test","command":"echo pokitclaudeapprovalprobe"}`)
	cmd, desc, ok := strictDecodeToolInput(raw)
	if !ok {
		t.Fatal("expected decode success")
	}
	if cmd != "echo pokitclaudeapprovalprobe" {
		t.Fatalf("command = %q", cmd)
	}
	if desc != "test" {
		t.Fatalf("description = %q", desc)
	}
}

func TestStrictDecode_DeterministicRepeat(t *testing.T) {
	raw := []byte(`{"command":"echo pokitclaudeapprovalprobe","description":"test"}`)
	var firstCmd, firstDesc string
	for i := 0; i < 100; i++ {
		cmd, desc, ok := strictDecodeToolInput(raw)
		if !ok {
			t.Fatalf("iteration %d: decode failed", i)
		}
		if i == 0 {
			firstCmd, firstDesc = cmd, desc
		} else {
			if cmd != firstCmd || desc != firstDesc {
				t.Fatalf("iteration %d: non-deterministic: (%q,%q) vs (%q,%q)", i, cmd, desc, firstCmd, firstDesc)
			}
		}
	}
}

// ── Classifier golden tests ──

func TestClassify_ExactMatch(t *testing.T) {
	raw := []byte(`{"command":"echo pokitclaudeapprovalprobe"}`)
	id, digest, ok := classifyCatalogAction(raw, "claude_headless", "2.1.209", "Bash")
	if !ok {
		t.Fatal("expected catalog match")
	}
	if id != "claude.bash.approval_probe.v1" {
		t.Fatalf("CatalogActionID = %q", id)
	}
	if digest == "" {
		t.Fatal("inputDigest is empty")
	}
	// Digest must be deterministic.
	_, digest2, _ := classifyCatalogAction(raw, "claude_headless", "2.1.209", "Bash")
	if digest != digest2 {
		t.Fatal("digest not deterministic")
	}
}

func TestClassify_ExactMatchWithDescription(t *testing.T) {
	raw := []byte(`{"command":"echo pokitclaudeapprovalprobe","description":"probe"}`)
	id, digest, ok := classifyCatalogAction(raw, "claude_headless", "2.1.209", "Bash")
	if !ok {
		t.Fatal("expected catalog match")
	}
	if id != "claude.bash.approval_probe.v1" {
		t.Fatalf("CatalogActionID = %q", id)
	}
	if digest == "" {
		t.Fatal("inputDigest is empty")
	}
}

// ── Command mutation ──

func TestClassify_CommandMutation(t *testing.T) {
	base := `{"command":"echo pokitclaudeapprovalprobe"}`
	// Change one byte.
	mutated := []byte(strings.Replace(base, "pokitclaudeapprovalprobe", "pokitclaudeapprovalprobeX", 1))
	_, _, ok := classifyCatalogAction(mutated, "claude_headless", "2.1.209", "Bash")
	if ok {
		t.Fatal("mutated command should not match")
	}
}

func TestClassify_CommandPrefixOnly(t *testing.T) {
	raw := []byte(`{"command":"echo pokitclaudeapproval"}`)
	_, _, ok := classifyCatalogAction(raw, "claude_headless", "2.1.209", "Bash")
	if ok {
		t.Fatal("prefix-only command should not match")
	}
}

// ── Missing / wrong-type command ──

func TestStrictDecode_MissingCommand(t *testing.T) {
	raw := []byte(`{"description":"test"}`)
	_, _, ok := strictDecodeToolInput(raw)
	if ok {
		t.Fatal("missing command should reject")
	}
}

func TestStrictDecode_CommandAsNumber(t *testing.T) {
	raw := []byte(`{"command":42}`)
	_, _, ok := strictDecodeToolInput(raw)
	if ok {
		t.Fatal("numeric command should reject")
	}
}

func TestStrictDecode_CommandAsNull(t *testing.T) {
	raw := []byte(`{"command":null}`)
	_, _, ok := strictDecodeToolInput(raw)
	if ok {
		t.Fatal("null command should reject")
	}
}

func TestStrictDecode_CommandAsArray(t *testing.T) {
	raw := []byte(`{"command":["echo","hello"]}`)
	_, _, ok := strictDecodeToolInput(raw)
	if ok {
		t.Fatal("array command should reject")
	}
}

func TestStrictDecode_CommandAsObject(t *testing.T) {
	raw := []byte(`{"command":{"cmd":"echo"}}`)
	_, _, ok := strictDecodeToolInput(raw)
	if ok {
		t.Fatal("object command should reject")
	}
}

func TestStrictDecode_CommandAsBool(t *testing.T) {
	raw := []byte(`{"command":true}`)
	_, _, ok := strictDecodeToolInput(raw)
	if ok {
		t.Fatal("boolean command should reject")
	}
}

func TestStrictDecode_DescriptionAsNumber(t *testing.T) {
	raw := []byte(`{"command":"echo pokitclaudeapprovalprobe","description":123}`)
	_, _, ok := strictDecodeToolInput(raw)
	if ok {
		t.Fatal("numeric description should reject")
	}
}

// ── Duplicate keys ──

func TestStrictDecode_DuplicateCommand(t *testing.T) {
	raw := []byte(`{"command":"echo pokitclaudeapprovalprobe","command":"echo other"}`)
	_, _, ok := strictDecodeToolInput(raw)
	if ok {
		t.Fatal("duplicate command keys should reject")
	}
}

func TestStrictDecode_DuplicateDescription(t *testing.T) {
	raw := []byte(`{"command":"echo pokitclaudeapprovalprobe","description":"a","description":"b"}`)
	_, _, ok := strictDecodeToolInput(raw)
	if ok {
		t.Fatal("duplicate description keys should reject")
	}
}

// ── Known-bad last-key-wins control ──

// TestStrictDecode_LastKeyWinsControl proves that strict token-walking
// rejects duplicate keys, while ordinary json.Unmarshal into a map would
// silently keep the last value. This is the critical difference that makes
// strict decoding necessary.
func TestStrictDecode_LastKeyWinsControl(t *testing.T) {
	// Duplicate command keys: first value is the catalog command,
	// second value is something else.
	raw := []byte(`{"command":"echo pokitclaudeapprovalprobe","command":"rm -rf /"}`)

	// 1. strictDecodeToolInput must reject.
	_, _, ok := strictDecodeToolInput(raw)
	if ok {
		t.Fatal("strict decoder must reject duplicate command keys")
	}

	// 2. Ordinary json.Unmarshal into a map silently keeps the LAST value.
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m["command"] != "rm -rf /" {
		t.Fatalf("json.Unmarshal kept first value: %v", m["command"])
	}
	// The map accepted "rm -rf /" — proving Unmarshal-based decode is
	// not a safe substitute for strict token-walking.
}

// ── Unknown fields ──

func TestStrictDecode_UnknownField(t *testing.T) {
	raw := []byte(`{"command":"echo pokitclaudeapprovalprobe","evil":true}`)
	_, _, ok := strictDecodeToolInput(raw)
	if ok {
		t.Fatal("unknown field should reject")
	}
}

func TestStrictDecode_UnknownFieldAlone(t *testing.T) {
	raw := []byte(`{"foo":"bar"}`)
	_, _, ok := strictDecodeToolInput(raw)
	if ok {
		t.Fatal("unknown field alone should reject")
	}
}

// ── Non-object input ──

func TestStrictDecode_StringInput(t *testing.T) {
	raw := []byte(`"just a string"`)
	_, _, ok := strictDecodeToolInput(raw)
	if ok {
		t.Fatal("string input should reject")
	}
}

func TestStrictDecode_ArrayInput(t *testing.T) {
	raw := []byte(`[{"command":"echo ok"}]`)
	_, _, ok := strictDecodeToolInput(raw)
	if ok {
		t.Fatal("array input should reject")
	}
}

func TestStrictDecode_NumberInput(t *testing.T) {
	raw := []byte(`42`)
	_, _, ok := strictDecodeToolInput(raw)
	if ok {
		t.Fatal("number input should reject")
	}
}

func TestStrictDecode_NullInput(t *testing.T) {
	raw := []byte(`null`)
	_, _, ok := strictDecodeToolInput(raw)
	if ok {
		t.Fatal("null input should reject")
	}
}

// ── Trailing content ──

func TestStrictDecode_TrailingJSON(t *testing.T) {
	raw := []byte(`{"command":"echo pokitclaudeapprovalprobe"}{"extra":"data"}`)
	_, _, ok := strictDecodeToolInput(raw)
	if ok {
		t.Fatal("trailing content should reject")
	}
}

func TestStrictDecode_TrailingWhitespace(t *testing.T) {
	raw := []byte(`{"command":"echo pokitclaudeapprovalprobe"} `)
	// Trailing whitespace: the json.Decoder.Token() for the closing '}'
	// consumes it as whitespace, then the next Token() call returns io.EOF.
	// So this should actually succeed.
	cmd, _, ok := strictDecodeToolInput(raw)
	if !ok {
		t.Fatal("trailing whitespace should succeed (dec.Token skips whitespace)")
	}
	if cmd != "echo pokitclaudeapprovalprobe" {
		t.Fatalf("command = %q", cmd)
	}
}

// ── UTF-8 ──

func TestStrictDecode_InvalidUTF8InRawBytes(t *testing.T) {
	// Real 0xFF byte in the raw input before the JSON decoder sees it.
	// utf8.Valid(raw) must reject before json.Decoder can substitute
	// replacement characters.
	raw := []byte(`{"command":"echo `)
	raw = append(raw, 0xFF) // invalid UTF-8 byte inside a JSON string
	raw = append(raw, []byte(`"}`)...)
	if utf8.Valid(raw) {
		t.Fatal("test setup: raw must be invalid UTF-8")
	}
	_, _, ok := strictDecodeToolInput(raw)
	if ok {
		t.Fatal("invalid UTF-8 in raw bytes must reject at the pre-decode gate")
	}
}

func TestStrictDecode_ReplacementCharDoesNotMatchCatalog(t *testing.T) {
	// If invalid UTF-8 slips past the pre-decode gate (defense in depth),
	// Go's json.Unmarshal replaces ill-formed sequences with U+FFFD.
	// A command containing U+FFFD can never match the catalog command,
	// so classification is still fail-closed.
	//
	// Construct raw bytes with the actual UTF-8 encoding of U+FFFD
	// (0xEF 0xBF 0xBD) inside the JSON string value.
	raw := []byte(`{"command":"echo `)
	raw = append(raw, 0xEF, 0xBF, 0xBD) // U+FFFD in UTF-8
	raw = append(raw, []byte(`"}`)...)
	if !utf8.Valid(raw) {
		t.Fatal("test setup: raw must be valid UTF-8")
	}
	cmd, _, ok := strictDecodeToolInput(raw)
	if !ok {
		t.Fatal("replacement char command must decode successfully")
	}
	if cmd != "echo �" {
		t.Fatalf("command = %q", cmd)
	}
	// Now prove it does NOT match the catalog.
	_, _, ok = classifyCatalogAction(raw, "claude_headless", "2.1.209", "Bash")
	if ok {
		t.Fatal("command containing replacement char must not match catalog")
	}
}

// ── Description bounds ──

func TestStrictDecode_DescriptionExactly512(t *testing.T) {
	desc := strings.Repeat("a", 512)
	raw := []byte(`{"command":"echo pokitclaudeapprovalprobe","description":"` + desc + `"}`)
	_, d, ok := strictDecodeToolInput(raw)
	if !ok {
		t.Fatal("512-byte description should succeed")
	}
	if len(d) != 512 {
		t.Fatalf("description length = %d", len(d))
	}
}

func TestStrictDecode_Description513Bytes(t *testing.T) {
	desc := strings.Repeat("a", 513)
	raw := []byte(`{"command":"echo pokitclaudeapprovalprobe","description":"` + desc + `"}`)
	_, _, ok := strictDecodeToolInput(raw)
	if ok {
		t.Fatal("513-byte description should reject")
	}
}

func TestStrictDecode_EmptyDescription(t *testing.T) {
	raw := []byte(`{"command":"echo pokitclaudeapprovalprobe","description":""}`)
	cmd, desc, ok := strictDecodeToolInput(raw)
	if !ok {
		t.Fatal("empty description should succeed")
	}
	if cmd != "echo pokitclaudeapprovalprobe" {
		t.Fatalf("command = %q", cmd)
	}
	if desc != "" {
		t.Fatalf("description = %q", desc)
	}
}

// ── Control characters in description ──

func TestStrictDecode_ControlCharInDescription(t *testing.T) {
	for _, r := range []rune{0x00, 0x01, 0x1F, 0x7F, 0x80, 0x9F} {
		desc := "before" + string(r) + "after"
		raw := []byte(`{"command":"echo pokitclaudeapprovalprobe","description":"` + desc + `"}`)
		_, _, ok := strictDecodeToolInput(raw)
		if ok {
			t.Fatalf("control char U+%04X in description should reject", r)
		}
	}
}

// ── Provider/version/tool mismatch ──

func TestClassify_ProviderMismatch(t *testing.T) {
	raw := []byte(`{"command":"echo pokitclaudeapprovalprobe"}`)
	_, _, ok := classifyCatalogAction(raw, "codex", "2.1.209", "Bash")
	if ok {
		t.Fatal("wrong provider should not match")
	}
}

func TestClassify_VersionMismatch(t *testing.T) {
	raw := []byte(`{"command":"echo pokitclaudeapprovalprobe"}`)
	_, _, ok := classifyCatalogAction(raw, "claude_headless", "2.1.208", "Bash")
	if ok {
		t.Fatal("wrong version should not match")
	}
}

func TestClassify_ToolMismatch(t *testing.T) {
	raw := []byte(`{"command":"echo pokitclaudeapprovalprobe"}`)
	_, _, ok := classifyCatalogAction(raw, "claude_headless", "2.1.209", "Read")
	if ok {
		t.Fatal("wrong tool name should not match")
	}
}

// ── Empty / edge input ──

func TestStrictDecode_EmptyObject(t *testing.T) {
	raw := []byte(`{}`)
	_, _, ok := strictDecodeToolInput(raw)
	if ok {
		t.Fatal("empty object should reject")
	}
}

func TestStrictDecode_EmptyCommand(t *testing.T) {
	raw := []byte(`{"command":""}`)
	_, _, ok := strictDecodeToolInput(raw)
	if ok {
		t.Fatal("empty command string should reject")
	}
}

func TestStrictDecode_WhitespaceOnlyCommand(t *testing.T) {
	raw := []byte(`{"command":"   "}`)
	cmd, _, ok := strictDecodeToolInput(raw)
	if !ok {
		t.Fatal("whitespace-only command should succeed (it is non-empty)")
	}
	if cmd != "   " {
		t.Fatalf("command = %q", cmd)
	}
}

// ── Byte bounds ──

func TestStrictDecode_ExactlyMaxToolInputBytes(t *testing.T) {
	// Construct a valid JSON object that is exactly maxToolInputBytes bytes.
	// Use whitespace padding between the last value and the closing '}';
	// json.Decoder skips whitespace between tokens, so the padding does not
	// affect the decoded values.
	prefix := []byte(`{"command":"echo pokitclaudeapprovalprobe"`)
	suffix := []byte(`}`)
	// Need enough padding to reach exactly maxToolInputBytes.
	padding := maxToolInputBytes - len(prefix) - len(suffix)
	if padding < 1 {
		t.Skip("maxToolInputBytes too small")
	}
	raw := make([]byte, 0, maxToolInputBytes)
	raw = append(raw, prefix...)
	raw = append(raw, bytes.Repeat([]byte(" "), padding)...)
	raw = append(raw, suffix...)
	if len(raw) != maxToolInputBytes {
		t.Fatalf("test setup: len=%d want=%d", len(raw), maxToolInputBytes)
	}
	if !utf8.Valid(raw) {
		t.Fatal("test setup: must be valid UTF-8")
	}
	cmd, _, ok := strictDecodeToolInput(raw)
	if !ok {
		t.Fatal("exact-bound input should succeed")
	}
	if cmd != "echo pokitclaudeapprovalprobe" {
		t.Fatalf("command = %q", cmd)
	}
}

func TestStrictDecode_OverMaxToolInputBytes(t *testing.T) {
	// One byte over the outer rampart — rejected by the len check
	// before any JSON decoding.
	prefix := []byte(`{"command":"echo pokitclaudeapprovalprobe"`)
	suffix := []byte(`}`)
	padding := maxToolInputBytes + 1 - len(prefix) - len(suffix)
	if padding < 1 {
		t.Skip("maxToolInputBytes too small")
	}
	raw := make([]byte, 0, maxToolInputBytes+1)
	raw = append(raw, prefix...)
	raw = append(raw, bytes.Repeat([]byte(" "), padding)...)
	raw = append(raw, suffix...)
	if len(raw) <= maxToolInputBytes {
		t.Fatalf("test setup: len=%d not over max=%d", len(raw), maxToolInputBytes)
	}
	_, _, ok := strictDecodeToolInput(raw)
	if ok {
		t.Fatal("over-bound input should reject at outer rampart")
	}
}

func TestStrictDecode_EmptyInput(t *testing.T) {
	_, _, ok := strictDecodeToolInput(nil)
	if ok {
		t.Fatal("nil input should reject")
	}
	_, _, ok = strictDecodeToolInput([]byte{})
	if ok {
		t.Fatal("empty input should reject")
	}
}

// ── Privacy: classifier output never contains command/description ──

func TestClassify_OutputDoesNotContainCommand(t *testing.T) {
	raw := []byte(`{"command":"echo pokitclaudeapprovalprobe"}`)
	id, digest, ok := classifyCatalogAction(raw, "claude_headless", "2.1.209", "Bash")
	if !ok {
		t.Fatal("expected match")
	}
	// The catalog action ID must not contain the command text.
	if strings.Contains(id, "echo") || strings.Contains(id, "pokitclaudeapprovalprobe") {
		t.Fatalf("CatalogActionID contains command text: %q", id)
	}
	// The digest is hex-only — it cannot carry the command.
	for _, c := range digest {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Fatalf("digest contains non-hex: %q", digest)
		}
	}
}

// ── Privacy: sentinel values in description never appear in output ──

func TestClassify_SentinelsNeverInOutput(t *testing.T) {
	// Description contains path-shaped, token-shaped, and other sentinel text.
	// Sentinel values use patterns that do not match production secret-scan regexes.
	desc := "/etc/passwd sekret-token-value gh_token_placeholder slack-bot-token ~/.ssh/id_rsa"
	raw := []byte(`{"command":"echo pokitclaudeapprovalprobe","description":"` + desc + `"}`)
	id, digest, ok := classifyCatalogAction(raw, "claude_headless", "2.1.209", "Bash")
	if !ok {
		t.Fatal("expected match despite sentinel description")
	}
	// The output (CatalogActionID + digest) must not contain any sentinel.
	output := id + "|" + digest
	for _, sentinel := range []string{
		"/etc/passwd", "sekret-token-value", "gh_token_placeholder", "slack-bot-token", ".ssh", "id_rsa",
	} {
		if strings.Contains(output, sentinel) {
			t.Fatalf("output contains sentinel %q: %s", sentinel, output)
		}
	}
}

// ── Catalog validation ──

func TestValidateCatalog_ProductionEntry(t *testing.T) {
	if err := validateCatalog(certifiedClaudeCatalog); err != nil {
		t.Fatal(err)
	}
}

func TestValidateCatalog_DuplicateCatalogActionID(t *testing.T) {
	entries := []catalogEntry{
		{CatalogActionID: "dup.v1", Provider: "p", Version: "v", ToolName: "Bash", Command: "a", Summary: "A"},
		{CatalogActionID: "dup.v1", Provider: "p", Version: "v", ToolName: "Bash", Command: "b", Summary: "B"},
	}
	if err := validateCatalog(entries); err == nil {
		t.Fatal("duplicate CatalogActionID must reject")
	}
}

func TestValidateCatalog_DuplicateSummary(t *testing.T) {
	entries := []catalogEntry{
		{CatalogActionID: "a.v1", Provider: "p", Version: "v", ToolName: "Bash", Command: "a", Summary: "Same Label"},
		{CatalogActionID: "b.v1", Provider: "p", Version: "v", ToolName: "Bash", Command: "b", Summary: "Same Label"},
	}
	if err := validateCatalog(entries); err == nil {
		t.Fatal("duplicate Summary must reject")
	}
}

func TestValidateCatalog_DuplicateTuple(t *testing.T) {
	entries := []catalogEntry{
		{CatalogActionID: "a.v1", Provider: "p", Version: "v", ToolName: "Bash", Command: "same", Summary: "A"},
		{CatalogActionID: "b.v1", Provider: "p", Version: "v", ToolName: "Bash", Command: "same", Summary: "B"},
	}
	if err := validateCatalog(entries); err == nil {
		t.Fatal("duplicate provider/version/tool/command tuple must reject")
	}
}

func TestValidateCatalog_EmptyFields(t *testing.T) {
	for _, entry := range []catalogEntry{
		{Provider: "p", Version: "v", ToolName: "Bash", Command: "c", Summary: "S"},
		{CatalogActionID: "id.v1", Version: "v", ToolName: "Bash", Command: "c", Summary: "S"},
		{CatalogActionID: "id.v1", Provider: "p", ToolName: "Bash", Command: "c", Summary: "S"},
		{CatalogActionID: "id.v1", Provider: "p", Version: "v", Command: "c", Summary: "S"},
		{CatalogActionID: "id.v1", Provider: "p", Version: "v", ToolName: "Bash", Summary: "S"},
		{CatalogActionID: "id.v1", Provider: "p", Version: "v", ToolName: "Bash", Command: "c"},
	} {
		if err := validateCatalog([]catalogEntry{entry}); err == nil {
			t.Errorf("empty required field must reject: %+v", entry)
		}
	}
}
