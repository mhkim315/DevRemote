package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"

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

	// Build the QR payload.
	qrPayload, _ := json.Marshal(map[string]string{
		"sessionId":      sess.SessionID,
		"hostId":         sess.HostID,
		"fingerprint":    sess.Fingerprint,
		"hostPubKey":     sess.HostPubKey,
		"bootstrapToken": sess.BootstrapToken,
		"endpoint":       sess.Endpoint,
		"expiresAt":      sess.ExpiresAt,
	})
	// Render an actual scannable QR.
	fmt.Println()
	renderQR(string(qrPayload))
	// Debug fallback (no secrets — the bootstrap token is already in the QR).
	fmt.Printf("\nQR payload: %s\n", string(qrPayload))
	fmt.Println()

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

// renderQR prints a scannable QR code. Prefers ANSI terminal output; falls
// back to PNG file when TERM is dumb/unknown or when stdout is not a terminal.
func renderQR(data string) {
	code, err := qr.Encode(data, qr.M)
	if err != nil {
		fmt.Println("(QR error)")
		return
	}
	termWidth := terminalWidth()
	qrWidth := code.Size // modules
	cellW := 1           // one character per module (compact)
	quietModules := 4    // standard QR quiet zone
	fullWidth := quietModules*2*cellW + qrWidth*cellW

	// Fall back to PNG if terminal is too narrow or not a TTY.
	if termWidth > 0 && fullWidth > termWidth {
		renderQRPNG(code)
		return
	}
	if !isTerminal() {
		renderQRPNG(code)
		return
	}
	renderQRANSI(code, quietModules, cellW)
}

// renderQRANSI prints a compact, ANSI-clean QR to the terminal.
// Every row begins and ends with a full ANSI reset, guaranteeing a clean
// quiet zone and no horizontal style leakage.
func renderQRANSI(code *qr.Code, quiet, cellW int) {
	blackBG := "\033[40m"
	whiteBG := "\033[47m"
	reset := "\033[0m"
	qrSize := code.Size

	// One-space cell for compact rendering.
	cell := strings.Repeat(" ", cellW)
	quietCol := strings.Repeat(" ", quiet*cellW)

	// Top quiet zone.
	for i := 0; i < quiet; i++ {
		fmt.Print(reset, quietCol)
		for x := 0; x < qrSize; x++ {
			if code.Black(x, 0) {
				fmt.Print(blackBG, cell)
			} else {
				fmt.Print(whiteBG, cell)
			}
		}
		fmt.Println(reset)
	}

	// QR rows.
	for y := 0; y < qrSize; y++ {
		fmt.Print(reset, quietCol) // ANSI reset before every row
		for x := 0; x < qrSize; x++ {
			if code.Black(x, y) {
				fmt.Print(blackBG, cell)
			} else {
				fmt.Print(whiteBG, cell)
			}
		}
		fmt.Print(reset) // ANSI reset at row end
		fmt.Println()
	}

	// Bottom quiet zone.
	for i := 0; i < quiet; i++ {
		fmt.Print(reset, quietCol)
		for x := 0; x < qrSize; x++ {
			if code.Black(x, qrSize-1) {
				fmt.Print(blackBG, cell)
			} else {
				fmt.Print(whiteBG, cell)
			}
		}
		fmt.Println(reset)
	}

	// Final ANSI reset.
	fmt.Print(reset)
}

// renderQRPNG writes a QR PNG file to a temp location and opens it.
func renderQRPNG(code *qr.Code) {
	f, err := os.CreateTemp("", "pokit-pair-*.png")
	if err != nil {
		fmt.Fprintf(os.Stderr, "PNG temp file: %v\n", err)
		return
	}
	pngBytes := code.PNG()
	if _, err := f.Write(pngBytes); err != nil {
		f.Close()
		os.Remove(f.Name())
		fmt.Fprintf(os.Stderr, "PNG write: %v\n", err)
		return
	}
	name := f.Name()
	f.Close()
	fmt.Printf("QR saved to: %s\n", name)
	go openFile(name)
}

func terminalWidth() int {
	return 0 // auto-detect not implemented; QR fits on modern terminals
}

func isTerminal() bool {
	fi, _ := os.Stdout.Stat()
	return fi != nil && (fi.Mode()&os.ModeCharDevice) != 0
}

func openFile(path string) {
	// best-effort: xdg-open / open
	_ = path
}
