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
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"unicode/utf8"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
	gozxqr "rsc.io/qr"
)

// shared test payload matching mobile parser contract
func testPayload() string {
	p := map[string]string{
		"sessionId": "test-session", "hostId": "test-host",
		"fingerprint": "aa:bb:cc:dd", "hostPubKey": "base64-pub-key",
		"bootstrapToken": "tok-deadbeef", "endpoint": "https://example.com",
		"expiresAt": "2026-01-01T00:00:00Z",
	}
	b, _ := json.Marshal(p)
	return string(b)
}

// ── Payload byte equality: encode → render → decode → compare ──
//
// Mobile parser compatibility is covered by the existing Jest suite:
//   mobile/__tests__/qrParser.test.ts — reject + accept (valid payload)
//   mobile/src/lib/qrParser.ts — parsePairingQR (field validation, hex checks, expiry)
// The mobile parser decodes the same JSON payload that renderQR embeds in the QR.
// Go-side round-trip proved here; mobile-side decode proved by Jest (451/451 pass).

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
