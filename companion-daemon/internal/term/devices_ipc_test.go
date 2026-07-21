package term

import (
	"bufio"
	"encoding/json"
	"net"
	"sync"
	"testing"
	"time"

	"devremote/companion-daemon/internal/devicetrust"
)

// driveIPCJSON sends one JSON request line through the real handleIPCConnection
// dispatch and returns the decoded single-line JSON response.
func driveIPCJSON(t *testing.T, req map[string]interface{}) map[string]json.RawMessage {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	go handleIPCConnection(serverConn, nil, nil, nil, nil)

	b, _ := json.Marshal(req)
	writeErr := make(chan error, 1)
	go func() {
		_, err := clientConn.Write(append(b, '\n'))
		writeErr <- err
	}()
	_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	line, err := bufio.NewReader(clientConn).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		t.Fatalf("read IPC response: %v", err)
	}
	if err := <-writeErr; err != nil {
		t.Fatalf("write IPC request: %v", err)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(line, &out); err != nil {
		t.Fatalf("decode IPC response %q: %v", line, err)
	}
	return out
}

func TestDevicesIPC_List(t *testing.T) {
	reg := deviceTrustReg(t)
	_, ownerPub, _ := devicetrust.GenKeypair(t)
	reg.Add(ownerPub, "owner-phone")
	_, memberPub, _ := devicetrust.GenKeypair(t)
	reg.Add(memberPub, "member-phone")
	SetPairingContext(nil, reg)
	SetDeviceAdminContext(nil, nil)

	resp := driveIPCJSON(t, map[string]interface{}{"version": 1, "operation": "devices-list"})
	var devs []devicetrust.PublicDevice
	if err := json.Unmarshal(resp["devices"], &devs); err != nil {
		t.Fatalf("decode devices: %v", err)
	}
	if len(devs) != 2 {
		t.Fatalf("got %d devices want 2", len(devs))
	}
	if devs[0].Role != devicetrust.RoleOwner || devs[1].Role != devicetrust.RoleMember {
		t.Fatalf("roles: %q, %q", devs[0].Role, devs[1].Role)
	}
	// Safe view only — never raw public-key DER.
	line, _ := json.Marshal(devs[0])
	var m map[string]json.RawMessage
	json.Unmarshal(line, &m)
	if _, leaked := m["publicKey"]; leaked {
		t.Fatal("devices-list leaked raw public key material")
	}
}

// TestDevicesIPC_RevokeEndToEnd drives devices-revoke over the real IPC dispatch
// and asserts it reaches the production revoke wiring: registry marked revoked,
// bearer session gone, the onRevoke callback fired (a registered connection is
// closed, pending tickets purged), and an audit event recorded.
func TestDevicesIPC_RevokeEndToEnd(t *testing.T) {
	reg := deviceTrustReg(t)
	_, pub, _ := devicetrust.GenKeypair(t)
	dev, _ := reg.Add(pub, "phone")

	bootID, _ := devicetrust.NewBootID()
	mgr := devicetrust.NewDeviceSessionManager(bootID, time.Minute)
	tickets := devicetrust.NewWSTicketStore()
	connReg := devicetrust.NewAuthenticatedConnRegistry()
	// Replicate the production onRevoke wiring (app.go `cb`).
	mgr.SetOnRevoke(func(deviceID string) {
		connReg.CloseDevice(deviceID)
		tickets.RevokeForDevice(deviceID)
	})

	// Active bearer + a registered connection + a pending ticket for the device.
	rawTok, _, _, err := mgr.CreateAfterVerifiedChallenge(dev.DeviceID, "host", bootID, []string{devicetrust.PermSessionsRead})
	if err != nil {
		t.Fatalf("create bearer: %v", err)
	}
	p := mgr.AuthenticateBearer(rawTok)
	if p == nil {
		t.Fatal("bearer not authenticated pre-revoke")
	}
	closed := make(chan struct{})
	connReg.Register(dev.DeviceID, closerFunc(func() error { close(closed); return nil }))
	if _, _, err := tickets.Issue(p, "host", "session"); err != nil {
		t.Fatalf("issue ticket: %v", err)
	}

	capAudit := &captureAuditTerm{}
	SetPairingContext(nil, reg)
	SetDeviceAdminContext(mgr, capAudit)

	resp := driveIPCJSON(t, map[string]interface{}{"version": 1, "operation": "devices-revoke", "deviceId": dev.DeviceID})
	if status := string(resp["status"]); status != `"revoked"` {
		t.Fatalf("revoke status=%s want \"revoked\"", status)
	}

	// Registry marked revoked; the key can no longer authenticate.
	if _, ok := reg.GetActive(dev.DeviceID); ok {
		t.Fatal("device still active after revoke")
	}
	// Bearer session gone.
	if mgr.AuthenticateBearer(rawTok) != nil {
		t.Fatal("bearer still valid after revoke")
	}
	// onRevoke fired: connection closed and pending ticket purged.
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("revoke did not close the registered connection")
	}
	if tickets.Count() != 0 {
		t.Fatalf("pending tickets not purged: %d", tickets.Count())
	}
	// Audit recorded.
	if len(capAudit.list()) == 0 || capAudit.list()[0].Action != devicetrust.ActionDeviceRevoke {
		t.Fatalf("no device.revoke audit event: %+v", capAudit.list())
	}
}

func TestDevicesIPC_RevokeUnknown(t *testing.T) {
	reg := deviceTrustReg(t)
	SetPairingContext(nil, reg)
	SetDeviceAdminContext(nil, nil)
	resp := driveIPCJSON(t, map[string]interface{}{"version": 1, "operation": "devices-revoke", "deviceId": "nope"})
	if _, ok := resp["error"]; !ok {
		t.Fatalf("expected error for unknown device, got %v", resp)
	}
}

// closerFunc adapts a func to io.Closer.
type closerFunc func() error

func (f closerFunc) Close() error { return f() }

// captureAuditTerm is a minimal in-memory AuditLog for the term IPC tests.
type captureAuditTerm struct {
	mu     sync.Mutex
	events []devicetrust.AuditEvent
}

func (c *captureAuditTerm) Record(ev devicetrust.AuditEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, ev)
}
func (c *captureAuditTerm) List(int) []devicetrust.AuditEvent { return c.list() }
func (c *captureAuditTerm) list() []devicetrust.AuditEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]devicetrust.AuditEvent(nil), c.events...)
}
