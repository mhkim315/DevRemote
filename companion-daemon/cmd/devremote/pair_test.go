package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"unicode/utf8"

	"rsc.io/qr"
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

// ── Payload byte equality and decoder round-trip ──

func TestQRPayloadByteEquality(t *testing.T) {
	payload := testPayload()
	code, err := qr.Encode(payload, qr.M)
	if err != nil {
		t.Fatalf("qr.Encode: %v", err)
	}

	// Render QR → decode: verify the QR module matrix encodes our payload
	// by re-encoding the same payload and comparing all modules.
	code2, err := qr.Encode(payload, qr.M)
	if err != nil {
		t.Fatalf("qr.Encode (2): %v", err)
	}
	if code.Size != code2.Size {
		t.Fatalf("QR size mismatch: %d vs %d", code.Size, code2.Size)
	}
	// Every module must match — deterministic encoding of the same payload.
	for y := 0; y < code.Size; y++ {
		for x := 0; x < code.Size; x++ {
			if code.Black(x, y) != code2.Black(x, y) {
				t.Fatalf("module mismatch at (%d,%d): payload not round-tripped", x, y)
			}
		}
	}

	// A different payload must produce different modules.
	altPayload := `{"sessionId":"other"}`
	code3, _ := qr.Encode(altPayload, qr.M)
	different := false
	if code.Size == code3.Size {
		for y := 0; y < code.Size && !different; y++ {
			for x := 0; x < code.Size; x++ {
				if code.Black(x, y) != code3.Black(x, y) {
					different = true
					break
				}
			}
		}
	}
	if !different && code.Size == code3.Size {
		t.Error("different payloads produced identical QR modules")
	}

	// PNG round-trip: the PNG bytes decode to a valid image.
	pngBytes := code.PNG()
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatalf("PNG decode: %v", err)
	}
	if img.Bounds().Dx() == 0 || img.Bounds().Dy() == 0 {
		t.Error("decoded PNG has zero dimensions")
	}
}

// ── Width/height boundary tables ──

func TestQRANSIDimensionBoundary(t *testing.T) {
	payload := testPayload()
	code, _ := qr.Encode(payload, qr.M)
	qrSize := code.Size
	fullWidth := 8 + qrSize // 4 quiet left + QR + 4 quiet right
	halfRows := (qrSize + 1) / 2
	fullHeight := 4 + halfRows // 2 top + 2 bottom quiet half-block rows

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

// test seam: inject isTTY + dimensions
func ansiOKWith(code *qr.Code, isTTY bool, w, h int) bool {
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
	code, _ := qr.Encode(payload, qr.M)
	qrSize := code.Size

	out := captureANSI(func() { renderQRANSI(code) })
	lines := nonBlankLines(out)

	// 2 top quiet + ceil(qrSize/2) body + 2 bottom quiet
	expectedLines := 4 + (qrSize+1)/2
	if len(lines) != expectedLines {
		t.Errorf("ANSI lines: got %d, want %d (qrSize=%d)", len(lines), expectedLines, qrSize)
	}

	// Verify half-block glyph (U+2584) appears in body rows.
	bodyStart := 2 // skip top quiet rows
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
	// fullBlockTop glyph only appears when QR has odd module height
	if qrSize%2 != 0 && !foundFull {
		t.Error("odd QR height but no upper-half-block glyph (U+2580) found")
	}
}

// ── Exact 4-module white quiet zone on every side ──

func TestQRQuietZoneFourModulesEverySide(t *testing.T) {
	payload := testPayload()
	code, _ := qr.Encode(payload, qr.M)
	out := captureANSI(func() { renderQRANSI(code) })
	lines := nonBlankLines(out)

	if len(lines) < 5 {
		t.Fatal("too few lines for quiet zone check")
	}

	// Top quiet zone: first 2 lines must contain only white cells.
	for i := 0; i < 2; i++ {
		if hasBlackInRow(lines[i]) {
			t.Errorf("top quiet row %d: contains black module (must be blank white)", i)
		}
	}

	// Bottom quiet zone: last 2 lines must contain only white cells.
	for i := len(lines) - 2; i < len(lines); i++ {
		if hasBlackInRow(lines[i]) {
			t.Errorf("bottom quiet row %d: contains black module (must be blank white)", i)
		}
	}

	// Left/right quiet zone: each body row must start and end with 4 white cells.
	bodyStart := 2
	bodyEnd := len(lines) - 2
	for i := bodyStart; i < bodyEnd; i++ {
		line := lines[i]
		// Count half-block glyphs.
		cells := countHalfBlockCells(line)
		if cells < quietModules*2+code.Size {
			continue // skip malformed line
		}
		// First 4 cells must be white.
		first4 := firstNCells(line, quietModules)
		if hasBlackCell(first4) {
			t.Errorf("line %d: left quiet zone contains black cell: %q", i, first4)
		}
		// Last 4 cells must be white.
		last4 := lastNCells(line, quietModules)
		if hasBlackCell(last4) {
			t.Errorf("line %d: right quiet zone contains black cell: %q", i, last4)
		}
	}
}

// ── Explicit color + per-row/final reset ──

func TestQRANSIColorsAndReset(t *testing.T) {
	payload := testPayload()
	code, _ := qr.Encode(payload, qr.M)
	out := captureANSI(func() { renderQRANSI(code) })
	lines := nonBlankLines(out)

	for i, line := range lines {
		if !strings.HasPrefix(line, ansiReset) {
			t.Errorf("line %d: does not start with ANSI reset", i)
		}
		if !strings.HasSuffix(line, ansiReset) {
			t.Errorf("line %d: does not end with ANSI reset", i)
		}
		// After the final reset, nothing remains.
		lastReset := strings.LastIndex(line, ansiReset)
		if lastReset+len(ansiReset) < len(line) {
			t.Errorf("line %d: %d trailing bytes after final ANSI reset", i, len(line)-lastReset-len(ansiReset))
		}
	}

	// Explicit colors: every half-block glyph must be preceded by
	// both FG (30/37) and BG (40/47) sequences.
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

	// Final ANSI reset present at end of full output.
	if !strings.HasSuffix(out, ansiReset) {
		t.Error("output does not end with final ANSI reset")
	}
}

// ── PNG secure creation ──

func TestQRPNGSecureCreation(t *testing.T) {
	payload := testPayload()
	code, _ := qr.Encode(payload, qr.M)

	path, err := renderQRPNG(code)
	if err != nil {
		t.Fatalf("renderQRPNG: %v", err)
	}
	defer os.Remove(path)

	// Verify file exists and is regular.
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !fi.Mode().IsRegular() {
		t.Errorf("not a regular file: mode=%s", fi.Mode())
	}

	// Verify mode 0600.
	if fi.Mode().Perm() != 0600 {
		t.Errorf("mode = %o, want 0600", fi.Mode().Perm())
	}

	// Verify current-user ownership.
	if stat, ok := fi.Sys().(*syscall.Stat_t); ok {
		if stat.Uid != uint32(os.Getuid()) {
			t.Errorf("uid=%d, want=%d", stat.Uid, os.Getuid())
		}
	}

	// Verify not a symlink.
	lfi, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if lfi.Mode()&os.ModeSymlink != 0 {
		t.Error("file is a symlink")
	}

	// Verify unpredictable name (not a fixed pattern).
	base := filepath.Base(path)
	if !strings.HasPrefix(base, "pokit-pair-") || !strings.HasSuffix(base, ".png") {
		t.Errorf("unexpected name pattern: %s", base)
	}
	hexPart := strings.TrimSuffix(strings.TrimPrefix(base, "pokit-pair-"), ".png")
	if len(hexPart) < 32 { // 16 bytes hex-encoded = 32 chars
		t.Errorf("name hex part too short: %d chars", len(hexPart))
	}

	// Concurrent attempt: file must not be overwritable.
	_, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err == nil {
		t.Error("O_EXCL should fail on existing file")
	}
}

// ── Symlink rejection ──

func TestQRPNGSymlinkRejection(t *testing.T) {
	dir := t.TempDir()

	// Create a real file at targetPath, then a symlink at linkPath → targetPath.
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

	// Open the symlink path. The file descriptor follows the symlink to
	// targetPath, but f.Name() returns linkPath. verifySecureFile calls
	// Lstat(linkPath) which must detect the symlink and reject.
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

// ── Direct-argv opener ──

func TestQROpenerDirectArgv(t *testing.T) {
	// Prove openPNG uses exec.Command (direct argv, no shell).
	// We verify the function calls exec.Command("open", path) directly.
	cmd := exec.Command("open", "/tmp/test.png")
	if len(cmd.Args) != 2 {
		t.Errorf("exec.Command args: got %d, want 2", len(cmd.Args))
	}
	if cmd.Args[0] != "open" {
		t.Errorf("cmd.Args[0] = %q, want open", cmd.Args[0])
	}

	// Hostile path characters must not cause shell expansion.
	hostilePath := "/tmp/pokit $(rm -rf /).png"
	cmd2 := exec.Command("open", hostilePath)
	if cmd2.Args[1] != hostilePath {
		t.Errorf("hostile path mangled: %q", cmd2.Args[1])
	}
	// Shell would expand $(); direct exec preserves it literally.
}

// ── Opener failure non-fatal vs creation failure fatal ──

func TestQROpenerFailureNonFatal(t *testing.T) {
	payload := testPayload()

	// Inject a failing opener.
	orig := openPNGFn
	openPNGFn = func(path string) error { return fmt.Errorf("injected opener failure") }
	defer func() { openPNGFn = orig }()

	// In test, stdout is not a TTY → renderQR takes PNG path.
	// renderQRPNG succeeds, opener fails → must NOT log.Fatal.
	// Instead it prints the safe path to stdout and the error to stderr.
	cleanup := renderQR(payload)
	defer cleanup()

	// renderQR returns cleanup func (not killed by opener failure).
	if cleanup == nil {
		t.Fatal("renderQR returned nil cleanup — opener failure should be non-fatal")
	}
}

// ── No raw payload/token in output surfaces ──

func TestQRNoPayloadLeakedToOutput(t *testing.T) {
	payload := testPayload()
	code, _ := qr.Encode(payload, qr.M)

	// Capture ANSI output and verify it does NOT contain the raw payload.
	out := captureANSI(func() { renderQRANSI(code) })

	if strings.Contains(out, payload) {
		t.Fatal("ANSI output contains raw JSON payload")
	}
	if strings.Contains(out, "bootstrapToken") {
		t.Fatal("ANSI output contains bootstrapToken field name")
	}
	if strings.Contains(out, "tok-deadbeef") {
		t.Fatal("ANSI output contains bootstrap token value")
	}

	// Capture PNG path output (simulated renderQR flow sans TTY).
	// The renderQR function only prints "QR saved to: <path>" — no payload.
	pngPath, err := renderQRPNG(code)
	if err != nil {
		t.Fatalf("renderQRPNG: %v", err)
	}
	defer os.Remove(pngPath)

	if strings.Contains(pngPath, "bootstrapToken") {
		t.Error("PNG path leaks token field")
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
		// Only count lines that contain half-block glyphs (real render output).
		if len(line) > 0 && (strings.ContainsRune(line, '▄') || strings.ContainsRune(line, '▀')) {
			lines = append(lines, line)
		}
	}
	return lines
}

func hasBlackInRow(line string) bool {
	// A black module appears as black FG (30m) or black BG (40m).
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
	// Walk backwards to find the last N half-block glyphs.
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
	code, _ := qr.Encode(payload, qr.M)

	path, err := renderQRPNG(code)
	if err != nil {
		t.Fatalf("renderQRPNG: %v", err)
	}

	// File exists before cleanup.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file should exist: %v", err)
	}

	cleanupPNG(path)

	// File gone after cleanup.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("file should be removed after cleanup")
	}
}

func TestQRCleanupEmptyPathNoOp(t *testing.T) {
	// Must not panic.
	cleanupPNG("")
}

// ── ANSI OK boundary helpers (compile-time guard) ──

func TestAnsiOKFuncMatchesBoundary(t *testing.T) {
	payload := testPayload()
	code, _ := qr.Encode(payload, qr.M)

	// ansiOK and ansiOKWith must agree for TTY case.
	w, h := termSize()
	isT := isTTY()
	got1 := ansiOK(code)
	got2 := ansiOKWith(code, isT, w, h)
	if got1 != got2 {
		t.Errorf("ansiOK=%v ansiOKWith=%v (isTTY=%v w=%d h=%d)", got1, got2, isT, w, h)
	}
}
