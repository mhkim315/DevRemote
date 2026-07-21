package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"
	"rsc.io/qr"
)

func runPairClient(args []string) {
	duration := 2 * time.Minute
	autoApprove := false
	for len(args) > 0 && strings.HasPrefix(args[0], "--") {
		if args[0] == "--duration" && len(args) > 1 {
			if d, err := time.ParseDuration(args[1]); err == nil && d > 0 && d <= 10*time.Minute {
				duration = d
			}
			args = args[2:]
		} else if args[0] == "--auto-approve" {
			autoApprove = true
			args = args[1:]
		} else {
			fmt.Fprintf(os.Stderr, "Usage: pokit pair [--duration <time>] [--auto-approve]\n")
			os.Exit(1)
		}
	}

	conn, err := net.Dial("unix", "/tmp/pokit.sock")
	if err != nil {
		log.Fatalf("Daemon socket unavailable: %v\nIs the daemon running?", err)
	}
	defer conn.Close()

	// 1) Send pair-start (version + operation + duration).
	start, _ := json.Marshal(map[string]interface{}{
		"version":   1,
		"operation": "pair-start",
		"duration":  int(duration.Seconds()),
	})
	conn.Write(append(start, '\n'))
	conn.SetReadDeadline(time.Now().Add(duration + 30*time.Second))
	dec := json.NewDecoder(conn)

	// 1) Decode session payload (immediately after pair-start).
	var sess struct {
		OK             bool   `json:"ok"`
		SessionID      string `json:"sessionId"`
		HostID         string `json:"hostId"`
		Fingerprint    string `json:"fingerprint"`
		HostPubKey     string `json:"hostPubKey"`
		BootstrapToken string `json:"bootstrapToken"`
		Endpoint       string `json:"endpoint"`
		ExpiresAt      string `json:"expiresAt"`
		Error          string `json:"error"`
	}
	if err := dec.Decode(&sess); err != nil || sess.Error != "" || !sess.OK {
		log.Fatalf("Pairing start failed: %s (err=%v)", sess.Error, err)
	}

	fmt.Printf("\nPokit Pairing\nHost ID: %s\nFingerprint: %s\n", sess.HostID, sess.Fingerprint)
	fmt.Printf("Endpoint: %s\nExpires: %s\n", sess.Endpoint, sess.ExpiresAt)

	// Build the QR payload. Payload bytes are NEVER emitted to
	// stdout, stderr, logs, diagnostics, or process arguments.
	qrPayload, _ := json.Marshal(map[string]string{
		"sessionId":      sess.SessionID,
		"hostId":         sess.HostID,
		"fingerprint":    sess.Fingerprint,
		"hostPubKey":     sess.HostPubKey,
		"bootstrapToken": sess.BootstrapToken,
		"endpoint":       sess.Endpoint,
		"expiresAt":      sess.ExpiresAt,
	})
	fmt.Println()
	cleanup := renderQR(string(qrPayload))
	defer cleanup()

	// 2) Decode candidate (arrives after proof_verified).
	var candMsg struct {
		Candidate struct {
			Fingerprint string `json:"fingerprint"`
			DisplayName string `json:"displayName"`
			PhoneNonce  string `json:"phoneNonce"`
			HostNonce   string `json:"hostNonce"`
			HostPubKey  string `json:"hostPubKey"`
		} `json:"candidate"`
		Error string `json:"error"`
	}
	if err := dec.Decode(&candMsg); err != nil || candMsg.Candidate.Fingerprint == "" {
		log.Fatalf("No candidate received: %s (err=%v)", candMsg.Error, err)
	}
	c := candMsg.Candidate
	fmt.Printf("\n📱 Device candidate:\n   Fingerprint: %s\n   Name: %s\n",
		c.Fingerprint, c.DisplayName)

	var approved bool
	if autoApprove {
		fmt.Println("Auto-approving device (--auto-approve).")
		approved = true
	} else {
		fmt.Printf("\nApprove this device? (y/N): ")
		var answer string
		fmt.Scanln(&answer)
		approved = strings.ToLower(strings.TrimSpace(answer)) == "y"
	}
	if !approved {
		reject, _ := json.Marshal(map[string]interface{}{"action": "reject", "version": 1})
		conn.Write(append(reject, '\n'))
		var done struct {
			Status string `json:"status"`
			Error  string `json:"error"`
		}
		dec.Decode(&done)
		fmt.Println("Pairing rejected.")
		os.Exit(1)
	}

	// 3) Approve → decode final result.
	approveReq, _ := json.Marshal(map[string]interface{}{"action": "approve", "version": 1})
	conn.Write(append(approveReq, '\n'))
	var done struct {
		Status string `json:"status"`
		Error  string `json:"error"`
		Device struct {
			DeviceID    string `json:"deviceId"`
			Fingerprint string `json:"fingerprint"`
			Role        string `json:"role"`
		} `json:"device"`
		State string `json:"state"`
	}
	if err := dec.Decode(&done); err != nil || done.Error != "" || done.Status != "paired" {
		log.Fatalf("Approval failed: %s (err=%v)", done.Error, err)
	}
	fmt.Printf("\n✔ Paired: %s\n  Fingerprint: %s\n  Role: %s\n",
		done.Device.DeviceID, done.Device.Fingerprint, done.Device.Role)
}

// ── QR Renderer ──

// renderQR renders a scannable QR code. It prefers ANSI half-block terminal
// output when the compact QR with quiet zone fits the measured dimensions;
// otherwise falls back to a securely created PNG file. Returns a cleanup
// function that removes any temporary file (best-effort, safe path only).
func renderQR(data string) func() {
	code, err := qr.Encode(data, qr.M)
	if err != nil {
		fmt.Println("(QR error)")
		return func() {}
	}

	// Selection: TTY + known fitting dimensions → ANSI; otherwise PNG.
	if ansiOK(code) {
		renderQRANSI(code)
		return func() {}
	}
	pngPath, err := renderQRPNG(code)
	if err != nil {
		// Secure creation failure is fatal.
		log.Fatalf("QR PNG: %v", err)
	}
	// Opener failure is non-fatal after secure creation.
	fmt.Printf("QR saved to: %s\n", pngPath)
	if err := openPNG(pngPath); err != nil {
		fmt.Fprintf(os.Stderr, "Could not open QR image: %v\n", err)
	}
	return func() { cleanupPNG(pngPath) }
}

// ansiOK returns true when stdout is a TTY, terminal dimensions are known,
// and the compact QR with 4-module quiet zone fits both width and height.
func ansiOK(code *qr.Code) bool {
	if !isTTY() {
		return false
	}
	w, h := termSize()
	if w <= 0 || h <= 0 {
		return false
	}
	qrSize := code.Size
	// Width: 4 left quiet + qrSize modules + 4 right quiet.
	fullWidth := 8 + qrSize
	// Height: 2 half-block rows (4 module rows of quiet) top + bottom,
	// plus ceil(qrSize/2) half-block rows for the QR body.
	halfRows := (qrSize + 1) / 2
	fullHeight := 4 + halfRows // 2 top quiet + 2 bottom quiet = 4 half-block rows
	return fullWidth <= w && fullHeight <= h
}

// ── ANSI Half-Block Terminal Renderer ──

const (
	ansiReset   = "\033[0m"
	ansiBlackBG = "\033[40m"
	ansiWhiteBG = "\033[47m"
	ansiBlackFG = "\033[30m"
	ansiWhiteFG = "\033[37m"

	quietModules = 4 // QR standard quiet zone
)

// halfBlock maps a pair of QR module rows (top, bottom) into one terminal
// cell using U+2584 (▄ LOWER HALF BLOCK). The foreground (ink) is the
// bottom module; the background is the top module. Black → FG black,
// white → FG white (explicit per cell).
func halfBlock(topBlack, bottomBlack bool) string {
	fg := ansiWhiteFG
	bg := ansiWhiteBG
	if topBlack {
		bg = ansiBlackBG
	}
	if bottomBlack {
		fg = ansiBlackFG
	}
	return fg + bg + "▄"
}

// fullBlockTop renders a single QR module row in the upper half of the
// cell using U+2580 (▀ UPPER HALF BLOCK). The foreground (ink) is the
// top/only module; the background is always white.
func fullBlockTop(black bool) string {
	fg := ansiWhiteFG
	if black {
		fg = ansiBlackFG
	}
	return fg + ansiWhiteBG + "▀"
}

// whiteCell returns a half-block cell that is entirely white.
func whiteCell() string {
	return ansiWhiteFG + ansiWhiteBG + "▄"
}

func renderQRANSI(code *qr.Code) {
	qrSize := code.Size

	// Per-row reset and final reset after output.
	defer fmt.Print(ansiReset)

	// Left quiet zone: 4 white columns.
	leftQuiet := strings.Repeat(whiteCell(), quietModules)

	// Top quiet zone: 2 blank white half-block rows (4 module rows).
	topQuietRow := leftQuiet + strings.Repeat(whiteCell(), qrSize) + leftQuiet
	for range 2 {
		fmt.Print(ansiReset, topQuietRow, ansiReset, "\n")
	}

	// QR body: process 2 module rows per terminal row.
	for y := 0; y < qrSize; y += 2 {
		hasNext := y+1 < qrSize
		fmt.Print(ansiReset, leftQuiet)
		for x := 0; x < qrSize; x++ {
			if hasNext {
				fmt.Print(halfBlock(code.Black(x, y), code.Black(x, y+1)))
			} else {
				fmt.Print(fullBlockTop(code.Black(x, y)))
			}
		}
		fmt.Print(leftQuiet)
		fmt.Print(ansiReset, "\n")
	}

	// Bottom quiet zone: 2 blank white half-block rows.
	for range 2 {
		fmt.Print(ansiReset, topQuietRow, ansiReset, "\n")
	}
}

// ── Secure PNG Fallback ──

// renderQRPNG creates a QR PNG file securely. The file is created atomically
// with an unpredictable name, mode 0600, and current-user ownership. Symlinks,
// non-regular targets, permissive modes, and ownership mismatch are rejected.
// Returns the absolute path of the successfully created file.
func renderQRPNG(code *qr.Code) (string, error) {
	dir := os.TempDir()

	// Unpredictable name: 16 random bytes, hex-encoded.
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("rand: %w", err)
	}
	name := filepath.Join(dir, "pokit-pair-"+hex.EncodeToString(b)+".png")

	// Atomic creation: O_EXCL ensures no overwrite.
	f, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", fmt.Errorf("create: %w", err)
	}

	// Security verification: mode, ownership, symlink, non-regular.
	if err := verifySecureFile(f); err != nil {
		f.Close()
		os.Remove(name)
		return "", err
	}

	if _, err := f.Write(code.PNG()); err != nil {
		f.Close()
		os.Remove(name)
		return "", fmt.Errorf("write: %w", err)
	}

	if err := f.Close(); err != nil {
		os.Remove(name)
		return "", fmt.Errorf("close: %w", err)
	}

	return name, nil
}

// verifySecureFile checks that an open file is a regular file (not symlink),
// has mode exactly 0600, and is owned by the current user.
func verifySecureFile(f *os.File) error {
	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat: %w", err)
	}

	// Reject non-regular files.
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("not a regular file: mode=%s", fi.Mode())
	}

	// Reject permissive modes (anything other than 0600).
	if fi.Mode().Perm() != 0600 {
		return fmt.Errorf("permissive mode: %o", fi.Mode().Perm())
	}

	// Verify ownership: current user must own the file.
	if stat, ok := fi.Sys().(*syscall.Stat_t); ok {
		if stat.Uid != uint32(os.Getuid()) {
			return fmt.Errorf("ownership mismatch: uid=%d, want=%d", stat.Uid, os.Getuid())
		}
	}

	// Verify the path is not a symlink (TOCTOU check on name).
	lfi, err := os.Lstat(f.Name())
	if err != nil {
		return fmt.Errorf("lstat: %w", err)
	}
	if lfi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("path is a symlink")
	}

	return nil
}

// cleanupPNG removes the PNG file at path. Best-effort, safe path only.
// Failure is silent.
func cleanupPNG(path string) {
	if path != "" {
		os.Remove(path)
	}
}

// ── Terminal Detection ──

func isTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// termSize returns the terminal width and height in cells, or 0,0 if
// stdout is not a TTY or the query fails.
func termSize() (int, int) {
	fd := int(os.Stdout.Fd())
	if !term.IsTerminal(fd) {
		return 0, 0
	}
	w, h, err := term.GetSize(fd)
	if err != nil {
		return 0, 0
	}
	return w, h
}

// ── macOS Opener ──

// openPNG opens a PNG file with the system opener using direct argv
// (no shell, no env command, no URL interpolation). On macOS this is
// equivalent to "open <path>".
func openPNG(path string) error {
	return exec.Command("open", path).Start()
}
