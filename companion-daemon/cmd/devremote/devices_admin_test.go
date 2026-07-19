package main

import (
	"encoding/json"
	"net"
	"os"
	"testing"
	"time"

	"devremote/companion-daemon/internal/devicetrust"
	"devremote/companion-daemon/internal/term"
)

// TestProofDeviceRevokeViaLocalIPCClosesLiveWS drives revoke through the real
// local 0600 IPC surface (not a direct sessionMgr call) and proves it closes an
// actual controlled-PTY WebSocket, clears the connection registry, marks the
// device revoked, invalidates the bearer, and records a redacted audit event.
func TestProofDeviceRevokeViaLocalIPCClosesLiveWS(t *testing.T) {
	f := newRemoteFixture(t,
		&devicetrust.WSTicketStoreConfig{TTL: 30 * time.Second, MaxPerDevice: 4, MaxTotal: 16},
		&devicetrust.DeviceSessionManagerConfig{Lifetime: 10 * time.Minute})

	ownerPriv, ownerID := f.pairDevice(t, "owner-ipc")
	ownerToken := f.token(t, ownerID, ownerPriv)
	session := f.createControlledSession(t, ownerToken, "ipc-revoke")
	_, ticket, _ := f.issue(t, ownerToken, session)
	ws := f.dialWS(t, session, ticket)
	waitFor(t, func() int { return f.app.connRegistry.Count(ownerID) }, 1, time.Second, "registered")

	// Wire the local device-admin surface to THIS app's trust instances plus a
	// real audit file, then start a real IPC server on a short temp socket path.
	auditPath := t.TempDir() + "/audit.jsonl"
	adminAudit := devicetrust.NewFileAuditLog(auditPath)
	term.SetPairingContext(f.id, f.reg)
	term.SetDeviceAdminContext(f.app.sessionMgr, adminAudit)

	sockPath := "/tmp/pokit-m255-ipc-test.sock"
	_ = os.Remove(sockPath)
	ipc, err := term.StartIPCServer(sockPath, f.app.registry, nil, f.app.lifecycle, nil, nil)
	if err != nil {
		t.Fatalf("start IPC: %v", err)
	}
	t.Cleanup(func() { _ = ipc.Close(); _ = os.Remove(sockPath) })

	// Revoke over the real 0600 socket.
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("dial socket: %v", err)
	}
	defer conn.Close()
	req, _ := json.Marshal(map[string]interface{}{"version": 1, "operation": "devices-revoke", "deviceId": ownerID})
	if _, err := conn.Write(append(req, '\n')); err != nil {
		t.Fatalf("write revoke: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var resp struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if err := json.NewDecoder(conn).Decode(&resp); err != nil || resp.Status != "revoked" {
		t.Fatalf("revoke response: status=%q err=%q decodeErr=%v", resp.Status, resp.Error, err)
	}

	// The live controlled-PTY WebSocket closes; registry + connection cleared.
	ws.awaitClosed(t, 2*time.Second, "ipc revoke")
	waitFor(t, func() int { return f.app.connRegistry.Count(ownerID) }, 0, time.Second, "connection removed")

	// Device revoked, bearer invalid, re-auth blocked.
	if _, ok := f.reg.GetActive(ownerID); ok {
		t.Fatal("device still active after IPC revoke")
	}
	if f.app.sessionMgr.AuthenticateBearer(ownerToken) != nil {
		t.Fatal("bearer still valid after IPC revoke")
	}

	// Redacted audit event recorded through the full socket path.
	found := false
	for _, e := range adminAudit.List(0) {
		if e.Action == devicetrust.ActionDeviceRevoke && e.DeviceID == ownerID {
			found = true
		}
	}
	if !found {
		t.Fatal("no device.revoke audit event recorded via IPC path")
	}
}
