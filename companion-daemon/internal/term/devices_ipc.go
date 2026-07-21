package term

import (
	"net"

	"devremote/companion-daemon/internal/devicetrust"
)

// deviceAdminContext returns the daemon-owned trust instances for the local
// device-admin IPC operations. These are reachable only over the 0600 Unix
// socket; there is no remote/tunnel device-management surface.
func deviceAdminContext() (*devicetrust.DeviceRegistry, *devicetrust.DeviceSessionManager, devicetrust.AuditLog) {
	pairingMu.Lock()
	defer pairingMu.Unlock()
	return pairingRegistry, pairingSessions, pairingAudit
}

// handleDevicesList returns the UI-safe view of every paired device (including
// revoked ones), oldest first.
func handleDevicesList(conn net.Conn) {
	reg, _, _ := deviceAdminContext()
	if reg == nil {
		writeIPC(conn, map[string]string{"error": "device trust not configured"})
		return
	}
	devs := reg.List()
	out := make([]devicetrust.PublicDevice, 0, len(devs))
	for _, d := range devs {
		out = append(out, d.Public())
	}
	writeIPC(conn, map[string]interface{}{"devices": out})
}

// handleDevicesRevoke marks a device revoked and immediately invalidates its
// active bearer sessions, pending WS tickets, and live WS connections through
// the production RevokeDevice wiring. Idempotent for an already-revoked device;
// an unknown device returns an error.
func handleDevicesRevoke(conn net.Conn, deviceID string) {
	reg, sessions, audit := deviceAdminContext()
	if reg == nil {
		writeIPC(conn, map[string]string{"error": "device trust not configured"})
		return
	}
	if deviceID == "" {
		writeIPC(conn, map[string]string{"error": "deviceId is required"})
		return
	}
	if err := reg.Revoke(deviceID); err != nil {
		writeIPC(conn, map[string]string{"error": err.Error()})
		return
	}
	// Enforce revocation end-to-end: kill active sessions, revoke pending
	// tickets, and close live connections via the wired onRevoke callback.
	if sessions != nil {
		sessions.RevokeDevice(deviceID)
	}
	if audit != nil {
		audit.Record(devicetrust.AuditEvent{
			DeviceID: deviceID, Action: devicetrust.ActionDeviceRevoke, Result: devicetrust.ResultOK,
		})
	}
	writeIPC(conn, map[string]interface{}{"status": "revoked", "deviceId": deviceID})
}

// handleDevicesRecoverOwner is the host-local owner recovery operation. It
// revokes the old owner, promotes the target member to owner, invalidates
// all sessions/tickets for both, and emits an audit record. Preconditions:
// oldOwnerID must be the current active owner; newOwnerID must be an active
// member. This is reachable only through the 0600 Unix socket.
func handleDevicesRecoverOwner(conn net.Conn, oldOwnerID, newOwnerID string) {
	reg, sessions, audit := deviceAdminContext()
	if reg == nil {
		writeIPC(conn, map[string]string{"error": "device trust not configured"})
		return
	}
	if oldOwnerID == "" || newOwnerID == "" {
		writeIPC(conn, map[string]string{"error": "both --from and --to device IDs are required"})
		return
	}
	if oldOwnerID == newOwnerID {
		writeIPC(conn, map[string]string{"error": "old owner and target must be different devices"})
		return
	}
	if err := reg.RecoverOwner(oldOwnerID, newOwnerID); err != nil {
		writeIPC(conn, map[string]string{"error": err.Error()})
		return
	}
	// Invalidate all sessions/tickets for BOTH the revoked old owner and the
	// promoted target so neither retains a stale bearer with old permissions.
	if sessions != nil {
		sessions.RevokeDevice(oldOwnerID)
		sessions.RevokeDevice(newOwnerID)
	}
	if audit != nil {
		audit.Record(devicetrust.AuditEvent{
			DeviceID: newOwnerID,
			Action:   devicetrust.ActionOwnerRecovery,
			Result:   devicetrust.ResultOK,
		})
	}
	writeIPC(conn, map[string]interface{}{
		"status":       "recovered",
		"oldOwnerId":   oldOwnerID,
		"newOwnerId":   newOwnerID,
		"oldOwnerState": "revoked",
		"newOwnerRole":  "owner",
	})
}

// handleAuditList returns up to limit most-recent redacted audit events.
func handleAuditList(conn net.Conn, limit int) {
	_, _, audit := deviceAdminContext()
	if audit == nil {
		writeIPC(conn, map[string]interface{}{"events": []devicetrust.AuditEvent{}})
		return
	}
	if limit <= 0 {
		limit = 100
	}
	writeIPC(conn, map[string]interface{}{"events": audit.List(limit)})
}
