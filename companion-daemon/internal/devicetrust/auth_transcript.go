package devicetrust

import (
	"encoding/binary"
	"fmt"
)

// AuthTranscript builds the canonical domain-separated transcript that both the
// host and the device sign during M2.5-3 challenge authentication.  Every field
// is length-prefixed so the transcript has unambiguous boundaries — the signer
// never signs a JSON encoding or an unterminated concatenation.
//
// Layout:
//
//	"pokit-device-auth-v1"
//	length(role)      || role        ("host" or "device")  ← first field after domain
//	length(hostId)    || hostId
//	length(deviceId)  || deviceId
//	length(bootId)    || daemonBootId
//	length(chalId)    || challengeId bytes
//	length(clientNC)  || clientNonce bytes
//	length(serverNC)  || serverNonce bytes
//	length(createdAt) || 8-byte big-endian UTC unix milliseconds
//	length(expiresAt) || 8-byte big-endian UTC unix milliseconds
//
// The distinct role field prevents a reflected host signature from being
// replayed as a device signature, or vice versa.
type AuthTranscript struct {
	Role         string // "host" or "device"
	HostID       string
	DeviceID     string
	DaemonBootID string
	ChallengeID  []byte
	ClientNonce  []byte
	ServerNonce  []byte
	CreatedAtMS  int64 // UTC unix milliseconds (issuedAt)
	ExpiresAtMS  int64 // UTC unix milliseconds
}

// Build returns the canonical binary transcript.
func (t AuthTranscript) Build() []byte {
	putField := func(b []byte, s string) []byte {
		b = binary.BigEndian.AppendUint32(b, uint32(len(s)))
		return append(b, s...)
	}
	putBytes := func(b []byte, d []byte) []byte {
		b = binary.BigEndian.AppendUint32(b, uint32(len(d)))
		return append(b, d...)
	}

	b := []byte("pokit-device-auth-v1")
	b = putField(b, t.Role)
	b = putField(b, t.HostID)
	b = putField(b, t.DeviceID)
	b = putField(b, t.DaemonBootID)
	b = putBytes(b, t.ChallengeID)
	b = putBytes(b, t.ClientNonce)
	b = putBytes(b, t.ServerNonce)
	b = appendUint64Field(b, t.CreatedAtMS)
	b = appendUint64Field(b, t.ExpiresAtMS)
	return b
}

func appendUint64Field(b []byte, v int64) []byte {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, uint64(v))
	b = binary.BigEndian.AppendUint32(b, 8)
	return append(b, buf...)
}

// Validate returns an error if required fields are missing or out of bounds.
func (t AuthTranscript) Validate() error {
	if t.Role != "host" && t.Role != "device" {
		return fmt.Errorf("invalid role: %q", t.Role)
	}
	if t.HostID == "" || t.DeviceID == "" || t.DaemonBootID == "" {
		return fmt.Errorf("missing identity field")
	}
	if len(t.ChallengeID) != 32 || len(t.ClientNonce) != 32 || len(t.ServerNonce) != 32 {
		return fmt.Errorf("invalid nonce/challenge length")
	}
	if t.CreatedAtMS <= 0 || t.ExpiresAtMS <= 0 {
		return fmt.Errorf("invalid timestamps")
	}
	return nil
}
