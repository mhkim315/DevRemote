package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"
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

	buf := make([]byte, 16384)
	n, _ := conn.Read(buf)
	var sess struct {
		OK          bool   `json:"ok"`
		SessionID   string `json:"sessionId"`
		HostID      string `json:"hostId"`
		Fingerprint string `json:"fingerprint"`
		HostPubKey  string `json:"hostPubKey"`
		Endpoint    string `json:"endpoint"`
		ExpiresAt   string `json:"expiresAt"`
		Error       string `json:"error"`
	}
	if err := json.Unmarshal(buf[:n], &sess); err != nil || sess.Error != "" || !sess.OK {
		log.Fatalf("Pairing start failed: %s", sess.Error)
	}

	fmt.Printf("\nPokit Pairing\nHost ID: %s\nFingerprint: %s\n", sess.HostID, sess.Fingerprint)
	fmt.Printf("Endpoint: %s\nExpires: %s\n", sess.Endpoint, sess.ExpiresAt)

	// QR payload (JSON, for the phone to scan).
	qr, _ := json.Marshal(map[string]string{
		"sessionId":   sess.SessionID,
		"hostId":      sess.HostID,
		"fingerprint": sess.Fingerprint,
		"hostPubKey":  sess.HostPubKey,
		"endpoint":    sess.Endpoint,
		"expiresAt":   sess.ExpiresAt,
	})
	fmt.Printf("\n\x1b[44;97m QR Payload (scan from phone): %s \x1b[0m\n\n", string(qr))

	fmt.Println("Waiting for device to pair...")

	// 2) Read the candidate.
	n, _ = conn.Read(buf)
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
	if err := json.Unmarshal(buf[:n], &candMsg); err != nil || candMsg.Candidate.Fingerprint == "" {
		log.Fatalf("No candidate received: %s", candMsg.Error)
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
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		n, _ = conn.Read(buf)
		fmt.Println("Pairing rejected.")
		var done map[string]interface{}
		json.Unmarshal(buf[:n], &done)
		os.Exit(1)
	}

	// 3) Approve.
	approveReq, _ := json.Marshal(map[string]interface{}{"action": "approve", "version": 1})
	conn.Write(append(approveReq, '\n'))
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	n, _ = conn.Read(buf)
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
	json.Unmarshal(buf[:n], &done)
	if done.Error != "" || done.Status != "paired" {
		log.Fatalf("Approval failed: %s (state=%s)", done.Error, done.State)
	}
	fmt.Printf("\n✔ Paired: %s\n  Fingerprint: %s\n  Role: %s\n",
		done.Device.DeviceID, done.Device.Fingerprint, done.Device.Role)
}
