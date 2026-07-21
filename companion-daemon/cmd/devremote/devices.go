package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"time"
)

const pokitSocket = "/tmp/pokit.sock"

// runDevicesClient implements `pokit devices`, `pokit devices revoke <id>`, and
// `pokit devices recover-owner --from <old-owner-id> --to <target-device-id>`.
// Device management is local-only: it speaks the privileged 0600 Unix socket,
// never the tunnel-reachable HTTP listener.
func runDevicesClient(args []string) {
	if len(args) > 0 && args[0] == "revoke" {
		if len(args) < 2 || args[1] == "" {
			fmt.Fprintln(os.Stderr, "Usage: pokit devices revoke <deviceId>")
			os.Exit(1)
		}
		revokeDevice(args[1])
		return
	}
	if len(args) > 0 && args[0] == "recover-owner" {
		runRecoverOwner(args[1:])
		return
	}
	listDevices()
}

func listDevices() {
	conn := dialPokit()
	defer conn.Close()
	writeJSONLine(conn, map[string]interface{}{"version": 1, "operation": "devices-list"})

	var resp struct {
		Devices []struct {
			DeviceID    string     `json:"deviceId"`
			Fingerprint string     `json:"fingerprint"`
			DisplayName string     `json:"displayName"`
			Role        string     `json:"role"`
			LastSeenAt  time.Time  `json:"lastSeenAt"`
			RevokedAt   *time.Time `json:"revokedAt"`
		} `json:"devices"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		log.Fatalf("devices list failed: %v", err)
	}
	if resp.Error != "" {
		log.Fatalf("devices list: %s", resp.Error)
	}
	if len(resp.Devices) == 0 {
		fmt.Println("No paired devices.")
		return
	}
	// Print the FULL canonical device ID — it is the exact value
	// `pokit devices revoke <id>` requires (the registry matches it exactly).
	for _, d := range resp.Devices {
		state := "active"
		if d.RevokedAt != nil {
			state = "revoked"
		}
		fmt.Println(deviceListLine(d.DeviceID, d.Role, state, d.LastSeenAt.Local().Format("2006-01-02 15:04"), d.DisplayName))
	}
}

// deviceListLine renders one `pokit devices` row. The full device ID is printed
// verbatim on its own line so it can be copied directly into
// `pokit devices revoke <id>`; the human-friendly fields follow, indented.
func deviceListLine(id, role, state, lastSeen, name string) string {
	return fmt.Sprintf("%s\n  role=%s  state=%s  last-seen=%s  name=%q", id, role, state, lastSeen, name)
}

func revokeDevice(deviceID string) {
	conn := dialPokit()
	defer conn.Close()
	writeJSONLine(conn, map[string]interface{}{"version": 1, "operation": "devices-revoke", "deviceId": deviceID})

	var resp struct {
		Status   string `json:"status"`
		DeviceID string `json:"deviceId"`
		Error    string `json:"error"`
	}
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		log.Fatalf("revoke failed: %v", err)
	}
	if resp.Error != "" {
		log.Fatalf("revoke: %s", resp.Error)
	}
	fmt.Printf("✔ Revoked device %s. Active sessions, tickets, and connections were invalidated.\n", resp.DeviceID)
}

// runRecoverOwner implements `pokit devices recover-owner --from <id> --to <id>`.
func runRecoverOwner(args []string) {
	var fromID, toID string
	for len(args) > 0 && args[0] != "" {
		if args[0] == "--from" && len(args) > 1 {
			fromID = args[1]
			args = args[2:]
		} else if args[0] == "--to" && len(args) > 1 {
			toID = args[1]
			args = args[2:]
		} else {
			fmt.Fprintf(os.Stderr, "Usage: pokit devices recover-owner --from <old-owner-id> --to <target-device-id>\n")
			os.Exit(1)
		}
	}
	if fromID == "" || toID == "" {
		fmt.Fprintf(os.Stderr, "Usage: pokit devices recover-owner --from <old-owner-id> --to <target-device-id>\n")
		os.Exit(1)
	}
	if fromID == toID {
		fmt.Fprintf(os.Stderr, "Old owner and target must be different devices.\n")
		os.Exit(1)
	}

	// Show shortened fingerprints and confirm.
	shortFrom := fromID
	if len(shortFrom) > 16 {
		shortFrom = shortFrom[:8] + "..." + shortFrom[len(shortFrom)-8:]
	}
	shortTo := toID
	if len(shortTo) > 16 {
		shortTo = shortTo[:8] + "..." + shortTo[len(shortTo)-8:]
	}

	fmt.Printf(`
Owner Recovery
══════════════
  Current owner: %s
  → will be REVOKED and all its sessions invalidated

  Target device: %s
  → will be PROMOTED to owner (its current sessions are also invalidated)

This is a destructive authority change. It cannot be undone without
a second recovery. Both the old owner and the promoted target will
need to re-authenticate.

`, shortFrom, shortTo)

	// Require non-trivial confirmation — type the first 8 hex chars of the
	// target device ID.
	fmt.Printf("To confirm, type the first 8 characters of the TARGET device ID: ")
	var confirm string
	fmt.Scanln(&confirm)

	want := toID[:8]
	if confirm != want {
		fmt.Fprintf(os.Stderr, "Confirmation mismatch (expected %q, got %q). Aborted.\n", want, confirm)
		os.Exit(1)
	}

	// Execute.
	conn := dialPokit()
	defer conn.Close()
	writeJSONLine(conn, map[string]interface{}{
		"version":      1,
		"operation":    "devices-recover-owner",
		"fromDeviceId": fromID,
		"toDeviceId":   toID,
	})

	var resp struct {
		Status        string `json:"status"`
		OldOwnerID    string `json:"oldOwnerId"`
		NewOwnerID    string `json:"newOwnerId"`
		OldOwnerState string `json:"oldOwnerState"`
		NewOwnerRole  string `json:"newOwnerRole"`
		Error         string `json:"error"`
	}
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		log.Fatalf("owner recovery failed: %v", err)
	}
	if resp.Error != "" {
		log.Fatalf("owner recovery: %s", resp.Error)
	}
	fmt.Printf(`
✔ Owner recovered.
  Old owner (%s): %s
  New owner (%s): %s
  All sessions and tickets invalidated for both devices.
  The promoted device must re-authenticate before receiving owner permissions.
`, shortFrom, resp.OldOwnerState, shortTo, resp.NewOwnerRole)
}

// runAuditClient implements `pokit audit [--limit N]`.
func runAuditClient(args []string) {
	limit := 100
	for len(args) > 0 {
		if args[0] == "--limit" && len(args) > 1 {
			var n int
			if _, err := fmt.Sscanf(args[1], "%d", &n); err == nil && n > 0 {
				limit = n
			}
			args = args[2:]
			continue
		}
		fmt.Fprintln(os.Stderr, "Usage: pokit audit [--limit N]")
		os.Exit(1)
	}
	conn := dialPokit()
	defer conn.Close()
	writeJSONLine(conn, map[string]interface{}{"version": 1, "operation": "audit-list", "limit": limit})

	var resp struct {
		Events []struct {
			Timestamp     time.Time `json:"timestamp"`
			DeviceID      string    `json:"deviceId"`
			Action        string    `json:"action"`
			SessionID     string    `json:"sessionId"`
			Result        string    `json:"result"`
			CorrelationID string    `json:"correlationId"`
		} `json:"events"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		log.Fatalf("audit list failed: %v", err)
	}
	if resp.Error != "" {
		log.Fatalf("audit: %s", resp.Error)
	}
	if len(resp.Events) == 0 {
		fmt.Println("No audit events.")
		return
	}
	for _, e := range resp.Events {
		fmt.Printf("%s  %-14s  %-8s  device=%s session=%s corr=%s\n",
			e.Timestamp.Local().Format("2006-01-02 15:04:05"),
			e.Action, e.Result, e.DeviceID, e.SessionID, e.CorrelationID)
	}
}

func dialPokit() net.Conn {
	conn, err := net.Dial("unix", pokitSocket)
	if err != nil {
		log.Fatalf("Daemon socket unavailable: %v\nIs the daemon running?", err)
	}
	return conn
}

func writeJSONLine(conn net.Conn, v interface{}) {
	b, _ := json.Marshal(v)
	if _, err := conn.Write(append(b, '\n')); err != nil {
		log.Fatalf("write to daemon: %v", err)
	}
}
