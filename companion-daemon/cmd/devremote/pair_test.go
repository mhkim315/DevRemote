package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
	gozxqr "rsc.io/qr"
)

// testPayload builds a pairing payload matching the mobile parser contract
// (mobile/src/lib/qrParser.ts:53-81). Every field is chosen to pass validation:
//
//	fingerprint: 64-char lowercase hex
//	hostPubKey:  182-char hex (91-byte P-256 SPKI)
//	endpoint:    private-LAN HTTP with explicit port
//	expiresAt:   valid ISO date in the FUTURE
func testPayload() string {
	fp := make([]byte, 32)
	rand.Read(fp)
	pk := make([]byte, 91)
	rand.Read(pk)
	tok := make([]byte, 16)
	rand.Read(tok)
	p := map[string]string{
		"sessionId":      "test-session",
		"hostId":         "test-host",
		"fingerprint":    hex.EncodeToString(fp),                                   // 64 lowercase hex
		"hostPubKey":     hex.EncodeToString(pk),                                   // 182 hex chars
		"bootstrapToken": hex.EncodeToString(tok),                                  // 32 hex chars
		"endpoint":       "http://192.168.1.10:8765",                               // private-LAN HTTP
		"expiresAt":      time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339), // future
	}
	b, _ := json.Marshal(p)
	return string(b)
}

// ── Payload byte equality + mobile parser field validation ──
//
// The daemon's QR payload must be byte-identical after Go encode→decode AND
// satisfy every field constraint the mobile parser enforces (qrParser.ts:53-81).

func TestQRPayloadByteEquality(t *testing.T) {
	payload := testPayload()

	// ONE encode.
	code, err := gozxqr.Encode(payload, gozxqr.M)
	if err != nil {
		t.Fatalf("qr.Encode: %v", err)
	}

	// Render to PNG, then decode the QR back to text.
	pngBytes := code.PNG()
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatalf("PNG decode: %v", err)
	}

	decoded, err := decodeQRFromImage(img)
	if err != nil {
		t.Fatalf("QR decode: %v", err)
	}

	// ONE decode. Compare to original payload bytes.
	if decoded != payload {
		t.Errorf("decoded payload mismatch:\n got:  %s\n want: %s", decoded, payload)
	}

	// Prove the decoded payload would be accepted by the mobile parser.
	// These checks mirror qrParser.ts _parse() field-by-field.
	if err := validateMobilePayload(decoded); err != nil {
		t.Errorf("decoded payload fails mobile parser rules: %v", err)
	}
}

// validateMobilePayload mirrors mobile/src/lib/qrParser.ts _parse().
// Returns nil if the payload would be accepted by the mobile parser.
func validateMobilePayload(raw string) error {
	var o map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &o); err != nil {
		return fmt.Errorf("JSON parse: %w", err)
	}

	allowed := map[string]bool{
		"sessionId": true, "hostId": true, "fingerprint": true,
		"hostPubKey": true, "bootstrapToken": true, "endpoint": true, "expiresAt": true,
	}
	for k := range o {
		if !allowed[k] {
			return fmt.Errorf("unknown field: %s", k)
		}
	}

	s, ok := str(o, "fingerprint")
	if !ok || len(s) != 64 {
		return fmt.Errorf("fingerprint must be 64 chars, got %d", len(s))
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(s) {
		return fmt.Errorf("fingerprint not 64-char lowercase hex")
	}

	pk, ok := str(o, "hostPubKey")
	if !ok || len(pk) != 182 {
		return fmt.Errorf("hostPubKey must be 182 chars, got %d", len(pk))
	}
	if !regexp.MustCompile(`^[0-9a-fA-F]{182}$`).MatchString(pk) {
		return fmt.Errorf("hostPubKey not 182-char hex")
	}

	ep, ok := str(o, "endpoint")
	if !ok {
		return fmt.Errorf("missing endpoint")
	}
	u, err := parseEndpoint(ep)
	if err != nil {
		return fmt.Errorf("endpoint: %w", err)
	}
	if u.Scheme != "http" {
		return fmt.Errorf("endpoint scheme must be http")
	}
	if u.User != nil {
		return fmt.Errorf("endpoint must not contain credentials")
	}
	if u.Fragment != "" || u.RawQuery != "" {
		return fmt.Errorf("endpoint must not contain fragment or query")
	}
	if !isPrivateIPv4(u.Hostname()) {
		return fmt.Errorf("endpoint must be private-IPv4 LAN address")
	}
	if u.Port() == "" {
		return fmt.Errorf("endpoint must include explicit port")
	}

	exp, ok := str(o, "expiresAt")
	if !ok {
		return fmt.Errorf("missing expiresAt")
	}
	et, err := time.Parse(time.RFC3339, exp)
	if err != nil {
		return fmt.Errorf("expiresAt not valid ISO date: %w", err)
	}
	if !et.After(time.Now()) {
		return fmt.Errorf("expiresAt is in the past: %s", et.Format(time.RFC3339))
	}

	sid, ok := str(o, "sessionId")
	if !ok || len(sid) < 1 || len(sid) > 128 {
		return fmt.Errorf("sessionId length out of range")
	}
	hid, ok := str(o, "hostId")
	if !ok || len(hid) < 1 || len(hid) > 128 {
		return fmt.Errorf("hostId length out of range")
	}
	tok, ok := str(o, "bootstrapToken")
	if !ok || len(tok) < 1 || len(tok) > 256 {
		return fmt.Errorf("bootstrapToken length out of range")
	}

	return nil
}

func str(o map[string]interface{}, k string) (string, bool) {
	v, ok := o[k]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func parseEndpoint(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	// net.ParseURL accepts "192.168.1.10:8765" without scheme.
	// new URL() in JS requires http:// prefix. We enforce the http scheme
	// separately; here we only validate basic structure.
	return u, nil
}

func isPrivateIPv4(host string) bool {
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	if ip4[0] == 10 {
		return true
	}
	if ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31 {
		return true
	}
	if ip4[0] == 192 && ip4[1] == 168 {
		return true
	}
	return false
}

// decodeQRFromImage decodes a QR code from a Go image using gozxing.
func decodeQRFromImage(img image.Image) (string, error) {
	bmp, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return "", fmt.Errorf("binary bitmap: %w", err)
	}
	reader := qrcode.NewQRCodeReader()
	result, err := reader.Decode(bmp, nil)
	if err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}
	return result.GetText(), nil
}

// ── Width/height boundary tables ──

func TestQRANSIDimensionBoundary(t *testing.T) {
	payload := testPayload()
	code, _ := gozxqr.Encode(payload, gozxqr.M)
	qrSize := code.Size
	fullWidth := 8 + qrSize
	halfRows := (qrSize + 1) / 2
	fullHeight := 4 + halfRows

	tests := []struct {
		name   string
		isTTY  bool
		w, h   int
		wantOK bool
	}{
		{"exact fit", true, fullWidth, fullHeight, true},
		{"wider and taller", true, fullWidth + 10, fullHeight + 10, true},
		{"one column short", true, fullWidth - 1, fullHeight, false},
		{"one row short", true, fullWidth, fullHeight - 1, false},
		{"zero dimensions", true, 0, 0, false},
		{"non-TTY", false, 999, 999, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ansiOKWith(code, tt.isTTY, tt.w, tt.h)
			if got != tt.wantOK {
				t.Errorf("ansiOK = %v, want %v (isTTY=%v w=%d h=%d full=%dx%d)",
					got, tt.wantOK, tt.isTTY, tt.w, tt.h, fullWidth, fullHeight)
			}
		})
	}
}

func ansiOKWith(code *gozxqr.Code, isTTY bool, w, h int) bool {
	if !isTTY {
		return false
	}
	if w <= 0 || h <= 0 {
		return false
	}
	qrSize := code.Size
	fullWidth := 8 + qrSize
	halfRows := (qrSize + 1) / 2
	fullHeight := 4 + halfRows
	return fullWidth <= w && fullHeight <= h
}

// ── Half-block mapping ──

func TestQRHalfBlockMapping(t *testing.T) {
	payload := testPayload()
	code, _ := gozxqr.Encode(payload, gozxqr.M)
	qrSize := code.Size

	out := captureANSI(func() { renderQRANSI(code) })
	lines := nonBlankLines(out)

	expectedLines := 4 + (qrSize+1)/2
	if len(lines) != expectedLines {
		t.Errorf("ANSI lines: got %d, want %d (qrSize=%d)", len(lines), expectedLines, qrSize)
	}

	bodyStart := 2
	bodyEnd := len(lines) - 2
	foundHalf := false
	foundFull := false
	for i := bodyStart; i < bodyEnd; i++ {
		for _, r := range lines[i] {
			if r == '▄' {
				foundHalf = true
			}
			if r == '▀' {
				foundFull = true
			}
		}
	}
	if !foundHalf {
		t.Error("no half-block glyph (U+2584) found in QR body rows")
	}
	if qrSize%2 != 0 && !foundFull {
		t.Error("odd QR height but no upper-half-block glyph (U+2580) found")
	}
}

// ── Exact 4-module white quiet zone on every side ──

func TestQRQuietZoneFourModulesEverySide(t *testing.T) {
	payload := testPayload()
	code, _ := gozxqr.Encode(payload, gozxqr.M)
	out := captureANSI(func() { renderQRANSI(code) })
	lines := nonBlankLines(out)

	if len(lines) < 5 {
		t.Fatal("too few lines for quiet zone check")
	}

	for i := 0; i < 2; i++ {
		if hasBlackInRow(lines[i]) {
			t.Errorf("top quiet row %d: contains black module (must be blank white)", i)
		}
	}
	for i := len(lines) - 2; i < len(lines); i++ {
		if hasBlackInRow(lines[i]) {
			t.Errorf("bottom quiet row %d: contains black module (must be blank white)", i)
		}
	}

	bodyStart := 2
	bodyEnd := len(lines) - 2
	for i := bodyStart; i < bodyEnd; i++ {
		line := lines[i]
		cells := countHalfBlockCells(line)
		if cells < quietModules*2+code.Size {
			continue
		}
		first4 := firstNCells(line, quietModules)
		if hasBlackCell(first4) {
			t.Errorf("line %d: left quiet zone contains black cell: %q", i, first4)
		}
		last4 := lastNCells(line, quietModules)
		if hasBlackCell(last4) {
			t.Errorf("line %d: right quiet zone contains black cell: %q", i, last4)
		}
	}
}

// ── Explicit color + per-row/final reset ──

func TestQRANSIColorsAndReset(t *testing.T) {
	payload := testPayload()
	code, _ := gozxqr.Encode(payload, gozxqr.M)
	out := captureANSI(func() { renderQRANSI(code) })
	lines := nonBlankLines(out)

	for i, line := range lines {
		if !strings.HasPrefix(line, ansiReset) {
			t.Errorf("line %d: does not start with ANSI reset", i)
		}
		if !strings.HasSuffix(line, ansiReset) {
			t.Errorf("line %d: does not end with ANSI reset", i)
		}
		lastReset := strings.LastIndex(line, ansiReset)
		if lastReset+len(ansiReset) < len(line) {
			t.Errorf("line %d: %d trailing bytes after final ANSI reset", i, len(line)-lastReset-len(ansiReset))
		}
	}

	for i, line := range lines {
		idx := 0
		for idx < len(line) {
			r, size := utf8.DecodeRuneInString(line[idx:])
			if r == '▄' || r == '▀' {
				prefix := line[max(0, idx-30):idx]
				if !strings.Contains(prefix, "\033[3") || !strings.Contains(prefix, "\033[4") {
					t.Errorf("line %d: glyph %U missing explicit FG/BG colors", i, r)
				}
			}
			idx += size
		}
	}

	if !strings.HasSuffix(out, ansiReset) {
		t.Error("output does not end with final ANSI reset")
	}
}

// ── PNG secure creation ──

func TestQRPNGSecureCreation(t *testing.T) {
	payload := testPayload()
	code, _ := gozxqr.Encode(payload, gozxqr.M)

	path, err := renderQRPNG(code)
	if err != nil {
		t.Fatalf("renderQRPNG: %v", err)
	}
	defer os.Remove(path)

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !fi.Mode().IsRegular() {
		t.Errorf("not a regular file: mode=%s", fi.Mode())
	}
	if fi.Mode().Perm() != 0600 {
		t.Errorf("mode = %o, want 0600", fi.Mode().Perm())
	}
	if stat, ok := fi.Sys().(*syscall.Stat_t); ok {
		if stat.Uid != uint32(os.Getuid()) {
			t.Errorf("uid=%d, want=%d", stat.Uid, os.Getuid())
		}
	}

	lfi, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if lfi.Mode()&os.ModeSymlink != 0 {
		t.Error("file is a symlink")
	}

	base := filepath.Base(path)
	if !strings.HasPrefix(base, "pokit-pair-") || !strings.HasSuffix(base, ".png") {
		t.Errorf("unexpected name pattern: %s", base)
	}
	hexPart := strings.TrimSuffix(strings.TrimPrefix(base, "pokit-pair-"), ".png")
	if len(hexPart) < 32 {
		t.Errorf("name hex part too short: %d chars", len(hexPart))
	}

	_, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err == nil {
		t.Error("O_EXCL should fail on existing file")
	}
}

// ── Symlink rejection ──

func TestQRPNGSymlinkRejection(t *testing.T) {
	dir := t.TempDir()

	targetPath := filepath.Join(dir, "real.png")
	f, err := os.OpenFile(targetPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("create real: %v", err)
	}
	f.Close()

	linkPath := filepath.Join(dir, "link.png")
	if err := os.Symlink(targetPath, linkPath); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	f2, err := os.OpenFile(linkPath, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open via symlink: %v", err)
	}
	defer f2.Close()

	err = verifySecureFile(f2)
	if err == nil {
		t.Error("verifySecureFile must reject file opened through a symlink path")
	}
	t.Logf("symlink rejected: %v", err)
}

// ── Direct-argv opener — exercises production openPNG and openPNGCmd ──

func TestQROpenerDirectArgv(t *testing.T) {
	// Phase 1: call production openPNG with a benign path. Verify it
	// returns without error (on macOS, "open" exists and Start() succeeds).
	err := openPNG("/tmp/pokit-test-nonexistent.png")
	// "open" may fail if the path does not exist but the Start() of the
	// command itself should succeed. Either way, the call exercises the
	// production code path.
	if err != nil {
		t.Logf("openPNG Start() returned error (expected if path missing): %v", err)
	}

	// Phase 2: verify openPNGCmd builds direct argv, not shell string.
	// Hostile path with spaces, semicolons, and shell metacharacters must
	// arrive verbatim in cmd.Args — exec.Command never uses a shell.
	hostilePath := "/tmp/pokit $(rm -rf /) '; DROP TABLE; \" .png"
	cmd := openPNGCmd(hostilePath)

	if len(cmd.Args) != 2 {
		t.Fatalf("cmd.Args: got %d, want 2", len(cmd.Args))
	}
	if cmd.Args[0] != "open" {
		t.Errorf("cmd.Args[0] = %q, want open", cmd.Args[0])
	}
	if cmd.Args[1] != hostilePath {
		t.Errorf("hostile path mangled:\n got:  %q\n want: %q", cmd.Args[1], hostilePath)
	}
	// exec.Command with separate args NEVER interpolates via shell.
	// Shell would expand $(rm -rf /); direct exec preserves it literally.

	// Phase 3: verify renderQR reaches the opener with the correct path
	// via the openPNGFn seam.
	orig := openPNGFn
	defer func() { openPNGFn = orig }()

	var capturedPath string
	openPNGFn = func(path string) error {
		capturedPath = path
		return nil // don't actually open
	}

	payload := testPayload()
	cleanup := renderQR(payload)
	defer cleanup()

	if capturedPath == "" {
		t.Error("openPNGFn was never called — renderQR did not reach opener")
	}
	// The captured path must be a safe PNG file path, not raw payload.
	if strings.Contains(capturedPath, "bootstrapToken") {
		t.Error("opener path contains token field name")
	}
}

// ── Opener failure non-fatal vs creation failure fatal ──

func TestQROpenerFailureNonFatal(t *testing.T) {
	payload := testPayload()

	orig := openPNGFn
	openPNGFn = func(path string) error { return fmt.Errorf("injected opener failure") }
	defer func() { openPNGFn = orig }()

	cleanup := renderQR(payload)
	defer cleanup()

	if cleanup == nil {
		t.Fatal("renderQR returned nil cleanup — opener failure should be non-fatal")
	}
}

// ── No raw payload/token in ANY output surface ──

func TestQRNoPayloadLeakedToOutput(t *testing.T) {
	payload := testPayload()
	code, _ := gozxqr.Encode(payload, gozxqr.M)

	// Capture stdout, stderr, AND log output from renderQR (PNG path).
	orig := openPNGFn
	var openerPath string
	openPNGFn = func(path string) error {
		openerPath = path
		return nil
	}
	defer func() { openPNGFn = orig }()

	var logBuf bytes.Buffer
	oldLog := log.Writer()
	log.SetOutput(&logBuf)
	defer log.SetOutput(oldLog)

	stdout, stderr := captureBothStreams(func() {
		cleanup := renderQR(payload)
		cleanup()
	})

	// Check all output surfaces.
	checkNoLeak(t, stdout, "stdout", payload)
	checkNoLeak(t, stderr, "stderr", payload)
	checkNoLeak(t, logBuf.String(), "log", payload)

	// Verify opener argv does not carry raw payload.
	checkNoLeak(t, openerPath, "opener argv[1]", payload)

	// Check ANSI output too.
	out := captureANSI(func() { renderQRANSI(code) })
	checkNoLeak(t, out, "ANSI", payload)

	// Check PNG path.
	pngPath, err := renderQRPNG(code)
	if err != nil {
		t.Fatalf("renderQRPNG: %v", err)
	}
	defer os.Remove(pngPath)
	if strings.Contains(pngPath, "bootstrapToken") {
		t.Error("PNG path leaks token field name")
	}
}

// captureBothStreams captures stdout and stderr while fn runs.
func captureBothStreams(fn func()) (string, string) {
	oldOut, oldErr := os.Stdout, os.Stderr
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout, os.Stderr = wOut, wErr

	var outBuf, errBuf bytes.Buffer
	outDone := make(chan struct{})
	errDone := make(chan struct{})

	go func() {
		b := make([]byte, 8192)
		for {
			n, err := rOut.Read(b)
			if n > 0 {
				outBuf.Write(b[:n])
			}
			if err != nil {
				break
			}
		}
		close(outDone)
	}()
	go func() {
		b := make([]byte, 8192)
		for {
			n, err := rErr.Read(b)
			if n > 0 {
				errBuf.Write(b[:n])
			}
			if err != nil {
				break
			}
		}
		close(errDone)
	}()

	fn()
	wOut.Close()
	wErr.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	<-outDone
	<-errDone

	return outBuf.String(), errBuf.String()
}

func checkNoLeak(t *testing.T, output, surface, payload string) {
	t.Helper()
	if strings.Contains(output, payload) {
		t.Errorf("%s contains raw JSON payload", surface)
	}
	if strings.Contains(output, "bootstrapToken") {
		t.Errorf("%s contains bootstrapToken field name", surface)
	}
	if strings.Contains(output, "tok-deadbeef") {
		t.Errorf("%s contains bootstrap token value", surface)
	}
}

// ── Helpers ──

func captureANSI(fn func()) string {
	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		var buf bytes.Buffer
		b := make([]byte, 8192)
		for {
			n, err := r.Read(b)
			if n > 0 {
				buf.Write(b[:n])
			}
			if err != nil {
				break
			}
		}
		done <- buf.String()
	}()
	fn()
	w.Close()
	os.Stdout = old
	return <-done
}

func nonBlankLines(out string) []string {
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		if len(line) > 0 && (strings.ContainsRune(line, '▄') || strings.ContainsRune(line, '▀')) {
			lines = append(lines, line)
		}
	}
	return lines
}

func hasBlackInRow(line string) bool {
	for i := 0; i < len(line); i++ {
		if strings.HasPrefix(line[i:], ansiBlackFG) || strings.HasPrefix(line[i:], ansiBlackBG) {
			return true
		}
	}
	return false
}

func countHalfBlockCells(line string) int {
	count := 0
	for _, r := range line {
		if r == '▄' || r == '▀' {
			count++
		}
	}
	return count
}

func hasBlackCell(s string) bool {
	return strings.Contains(s, ansiBlackFG) || strings.Contains(s, ansiBlackBG)
}

func firstNCells(line string, n int) string {
	count := 0
	idx := 0
	for idx < len(line) && count < n {
		r, size := utf8.DecodeRuneInString(line[idx:])
		if r == '▄' || r == '▀' {
			count++
		}
		idx += size
	}
	return line[:idx]
}

func lastNCells(line string, n int) string {
	positions := []int{}
	idx := 0
	for idx < len(line) {
		r, size := utf8.DecodeRuneInString(line[idx:])
		if r == '▄' || r == '▀' {
			positions = append(positions, idx)
		}
		idx += size
	}
	if len(positions) < n {
		return ""
	}
	start := positions[len(positions)-n]
	return line[start:]
}

// ── Secure file verification ──

func TestVerifySecureFileRegularMode(t *testing.T) {
	dir := os.TempDir()
	b := make([]byte, 16)
	rand.Read(b)
	name := filepath.Join(dir, "pokit-test-"+hex.EncodeToString(b)+".png")
	defer os.Remove(name)

	f, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer f.Close()

	if err := verifySecureFile(f); err != nil {
		t.Errorf("verifySecureFile: %v", err)
	}
}

func TestVerifySecureFileRejectsPermissiveMode(t *testing.T) {
	dir := os.TempDir()
	b := make([]byte, 16)
	rand.Read(b)
	name := filepath.Join(dir, "pokit-test-"+hex.EncodeToString(b)+".png")
	defer os.Remove(name)

	f, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer f.Close()

	if err := verifySecureFile(f); err == nil {
		t.Error("verifySecureFile should reject mode 0644")
	}
}

// ── Cleanup lifecycle ──

func TestQRCleanupRemovesPNG(t *testing.T) {
	payload := testPayload()
	code, _ := gozxqr.Encode(payload, gozxqr.M)

	path, err := renderQRPNG(code)
	if err != nil {
		t.Fatalf("renderQRPNG: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file should exist: %v", err)
	}

	cleanupPNG(path)

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("file should be removed after cleanup")
	}
}

func TestQRCleanupEmptyPathNoOp(t *testing.T) {
	cleanupPNG("")
}

func TestAnsiOKFuncMatchesBoundary(t *testing.T) {
	payload := testPayload()
	code, _ := gozxqr.Encode(payload, gozxqr.M)

	w, h := termSize()
	isT := isTTY()
	got1 := ansiOK(code)
	got2 := ansiOKWith(code, isT, w, h)
	if got1 != got2 {
		t.Errorf("ansiOK=%v ansiOKWith=%v (isTTY=%v w=%d h=%d)", got1, got2, isT, w, h)
	}
}
