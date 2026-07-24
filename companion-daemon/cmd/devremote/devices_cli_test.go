package main

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"devremote/companion-daemon/internal/devicetrust"
	"devremote/companion-daemon/internal/term"
)

// TestDevicesCLI_ListLinePrintsFullID proves the displayed row contains the
// full canonical device ID verbatim (no truncation), so it is copy-pasteable.
func TestDevicesCLI_ListLinePrintsFullID(t *testing.T) {
	full := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" // 64 hex
	line := deviceListLine(full, "owner", "active", "2026-07-11 09:00", "my phone")
	if !strings.Contains(line, full) {
		t.Fatalf("device list line does not contain the full ID:\n%s", line)
	}
}

// TestDevicesCLI_ListedIDRevokesUnchanged drives the exact command-layer wire
// the CLI uses: devices-list returns an ID, and that same ID string (unmodified)
// is accepted by devices-revoke and actually revokes the device.
func TestDevicesCLI_ListedIDRevokesUnchanged(t *testing.T) {
	dir := t.TempDir()
	reg, err := devicetrust.NewDeviceRegistry(&devicetrust.FileDeviceStore{Path: dir + "/devices.json"})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	_, pub, _ := devicetrust.GenKeypair(t)
	dev, err := reg.Add(pub, "my phone")
	if err != nil {
		t.Fatalf("add device: %v", err)
	}
	bootID, _ := devicetrust.NewBootID()
	mgr := devicetrust.NewPermissiveSessionManager(bootID, time.Minute)
	term.SetPairingContext(nil, reg)
	term.SetDeviceAdminContext(mgr, devicetrust.NopAuditLog{})

	sock := "/tmp/pokit-m255-cli-test.sock"
	_ = os.Remove(sock)
	ipc, err := term.StartIPCServer(sock, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("start IPC: %v", err)
	}
	t.Cleanup(func() { _ = ipc.Close(); _ = os.Remove(sock) })

	// devices-list → capture the exact displayed ID.
	listed := ipcRoundTrip(t, sock, map[string]interface{}{"version": 1, "operation": "devices-list"})
	var lr struct {
		Devices []struct {
			DeviceID string `json:"deviceId"`
		} `json:"devices"`
	}
	if err := json.Unmarshal(listed, &lr); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(lr.Devices) != 1 {
		t.Fatalf("want 1 device, got %d", len(lr.Devices))
	}
	shownID := lr.Devices[0].DeviceID
	if shownID != dev.DeviceID || len(shownID) != 64 {
		t.Fatalf("listed id %q (len %d) != full registry id %q", shownID, len(shownID), dev.DeviceID)
	}

	// Feed the exact shown ID straight into revoke.
	revoked := ipcRoundTrip(t, sock, map[string]interface{}{"version": 1, "operation": "devices-revoke", "deviceId": shownID})
	var rr struct{ Status, Error string }
	_ = json.Unmarshal(revoked, &rr)
	if rr.Status != "revoked" {
		t.Fatalf("revoke status=%q err=%q", rr.Status, rr.Error)
	}
	if _, ok := reg.GetActive(dev.DeviceID); ok {
		t.Fatal("device still active after revoking the CLI-displayed ID")
	}
}

func ipcRoundTrip(t *testing.T, sock string, req map[string]interface{}) []byte {
	t.Helper()
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	b, _ := json.Marshal(req)
	if _, err := conn.Write(append(b, '\n')); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		t.Fatalf("read: %v", err)
	}
	return line
}
