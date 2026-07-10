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
	for len(args) > 0 && strings.HasPrefix(args[0], "--") {
		if args[0] == "--duration" && len(args) > 1 {
			if d, err := time.ParseDuration(args[1]); err == nil && d > 0 && d <= 10*time.Minute {
				duration = d
			}
			args = args[2:]
		} else {
			fmt.Fprintf(os.Stderr, "Usage: pokit pair [--duration <time>]\n")
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
	fmt.Printf("\nApprove this device? (y/N): ")

	var answer string
	fmt.Scanln(&answer)
	if strings.ToLower(strings.TrimSpace(answer)) != "y" {
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

// renderQR prints a scannable QR code to the terminal using ANSI blocks.
func renderQR(data string) {
	code, err := qr.Encode(data, qr.M)
	if err != nil {
		fmt.Println("(QR error)")
		return
	}
	// White-on-black QR blocks.
	for y := 0; y < code.Size; y++ {
		fmt.Print("  ")
		for x := 0; x < code.Size; x++ {
			if code.Black(x, y) {
				fmt.Print("\033[47m  \033[0m")
			} else {
				fmt.Print("\033[40m  \033[0m")
			}
		}
		fmt.Println()
	}
}
