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
		log.Fatalf("Daemon socket unavailable: %v\nIs the daemon running? Try: pokit daemon --insecure-local-only", err)
	}
	defer conn.Close()

	// 1) Start pairing session.
	start, _ := json.Marshal(map[string]interface{}{
		"version":   1,
		"operation": "pair-start",
		"duration":  int(duration.Seconds()),
	})
	conn.Write(append(start, '\n'))
	buf := make([]byte, 16384)
	n, _ := conn.Read(buf)
	var startResp struct {
		SessionID   string `json:"sessionId"`
		HostID      string `json:"hostId"`
		Fingerprint string `json:"fingerprint"`
		Endpoint    string `json:"endpoint"`
		Secret      string `json:"secret"`
		ExpiresAt   string `json:"expiresAt"`
		Error       string `json:"error"`
	}
	if err := json.Unmarshal(buf[:n], &startResp); err != nil {
		log.Fatalf("Daemon response error: %v", err)
	}
	if startResp.Error != "" {
		log.Fatalf("Pairing start failed: %s", startResp.Error)
	}

	fmt.Printf("\nPokit Pairing\n")
	fmt.Printf("Host: %s\nFingerprint: %s\n\n", startResp.HostID, startResp.Fingerprint)
	fmt.Printf("Session: %s (expires %s)\n", startResp.SessionID, startResp.ExpiresAt)
	fmt.Printf("Endpoint: %s\n\n", startResp.Endpoint)

	// Show the payload for the phone to scan (including the one-time secret).
	payload, _ := json.Marshal(map[string]string{
		"sessionId":   startResp.SessionID,
		"hostId":      startResp.HostID,
		"fingerprint": startResp.Fingerprint,
		"endpoint":    startResp.Endpoint,
		"secret":      startResp.Secret,
	})
	fmt.Printf("%s\n", payload)
	fmt.Printf("\n\x1b[41;97m  Scan this pairing data from the phone  \x1b[0m\n\n")

	// 2) Wait for a candidate or timeout.
	fmt.Println("Waiting for device to pair...")
	conn.SetReadDeadline(time.Now().Add(duration + 10*time.Second))
	n, _ = conn.Read(buf)
	var candidateResp struct {
		Candidate struct {
			Fingerprint string `json:"fingerprint"`
			DisplayName string `json:"displayName"`
			PhoneNonce  []byte `json:"phoneNonce"`
			HostNonce   []byte `json:"hostNonce"`
		} `json:"candidate"`
		Error        string `json:"error"`
		SessionEnded bool   `json:"sessionEnded"`
		Timeout      bool   `json:"timeout"`
	}
	if err := json.Unmarshal(buf[:n], &candidateResp); err != nil || candidateResp.Error != "" {
		if candidateResp.Timeout || candidateResp.SessionEnded {
			fmt.Println("No device paired (timeout).")
		} else {
			fmt.Printf("Pairing failed: %s\n", candidateResp.Error)
		}
		os.Exit(1)
	}
	if candidateResp.Error == "" && candidateResp.Candidate.Fingerprint == "" {
		fmt.Println("No device paired (timeout).")
		os.Exit(1)
	}

	cand := candidateResp.Candidate
	fmt.Printf("\n📱 Device candidate:\n")
	fmt.Printf("   Fingerprint: %s\n", cand.Fingerprint)
	fmt.Printf("   Name: %s\n", cand.DisplayName)
	fmt.Printf("\nApprove this device? (y/N): ")

	var answer string
	fmt.Scanln(&answer)
	if strings.ToLower(strings.TrimSpace(answer)) != "y" {
		// 3) Reject.
		reject, _ := json.Marshal(map[string]interface{}{"version": 1, "operation": "pair-reject"})
		conn.Write(append(reject, '\n'))
		fmt.Println("Pairing rejected.")
		os.Exit(1)
	}

	// 4) Approve: the phone must have signed the transcript. For now we skip
	//    the signature check (challenge-response depth lands in M2.5-3).
	approve, _ := json.Marshal(map[string]interface{}{"version": 1, "operation": "pair-approve"})
	conn.Write(append(approve, '\n'))
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, _ = conn.Read(buf)
	var doneResp struct {
		Status string `json:"status"`
		Device struct {
			DeviceID    string `json:"deviceId"`
			Fingerprint string `json:"fingerprint"`
			Role        string `json:"role"`
		} `json:"device"`
		Error string `json:"error"`
	}
	json.Unmarshal(buf[:n], &doneResp)
	if doneResp.Error != "" {
		log.Fatalf("Approval failed: %s", doneResp.Error)
	}
	fmt.Printf("\n✔ Paired: %s\n", doneResp.Device.DeviceID)
	fmt.Printf("  Fingerprint: %s\n", doneResp.Device.Fingerprint)
	fmt.Printf("  Role: %s\n", doneResp.Device.Role)
}
