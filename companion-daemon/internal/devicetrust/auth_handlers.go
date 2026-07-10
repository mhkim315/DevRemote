package devicetrust

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

// AuthHandler holds the daemon-owned dependencies for challenge-auth
// endpoints. It is handler-independent — M2.5-4 middleware will use the
// same DeviceSessionManager + DeviceRegistry.
type AuthHandler struct {
	Identity    *HostIdentity         // host key for signing challenges
	Registry    *DeviceRegistry       // paired-device registry
	Challenges  *ChallengeStore       // single-use challenge store
	Sessions    *DeviceSessionManager // session token store
	RateLimiter *challengeRateLimiter // per-device challenge rate limiter
	Audit       AuditLog              // optional; nil ⇒ no audit
}

// audit records a redacted event if an audit log is configured.
func (h *AuthHandler) audit(ev AuditEvent) {
	if h.Audit != nil {
		h.Audit.Record(ev)
	}
}

// ── POST /api/device-auth/challenge ──

type ChallengeRequest struct {
	Version     int    `json:"version"`
	HostID      string `json:"hostId"`
	DeviceID    string `json:"deviceId"`
	ClientNonce string `json:"clientNonce"` // 32-byte hex
}

type AuthChallengeResponse struct {
	Version            int       `json:"version"`
	ChallengeID        string    `json:"challengeId"` // hex
	HostID             string    `json:"hostId"`
	HostKeyFingerprint string    `json:"hostKeyFingerprint"`
	DaemonBootID       string    `json:"daemonBootId"`
	ServerNonce        string    `json:"serverNonce"` // hex
	ExpiresAt          time.Time `json:"expiresAt"`
	HostSignature      string    `json:"hostSignature"` // hex of DER ECDSA
}

// HandleChallenge creates a single-use challenge and returns it signed by the
// host identity. Unknown/revoked devices receive the same non-enumerating
// error response.
func (h *AuthHandler) HandleChallenge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.Identity == nil || h.Registry == nil || h.Sessions == nil {
		http.Error(w, "device auth not configured", http.StatusServiceUnavailable)
		return
	}
	var req ChallengeRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.Version != 1 || req.HostID != h.Identity.HostID {
		http.Error(w, "invalid version or host", http.StatusBadRequest)
		return
	}
	clientNonce, err := hex.DecodeString(req.ClientNonce)
	if err != nil || len(clientNonce) != 32 {
		http.Error(w, "invalid nonce", http.StatusBadRequest)
		return
	}

	// Non-enumerating: unknown/revoked devices get the same 400.
	dev, ok := h.Registry.GetActive(req.DeviceID)
	if !ok {
		http.Error(w, "invalid device", http.StatusBadRequest)
		return
	}

	// Rate limit — bounded per-device, burst=3, rate=10/min.
	if h.RateLimiter != nil && !h.RateLimiter.Allow(time.Now().UTC(), dev.DeviceID) {
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}

	// Generate all random values BEFORE inserting anything. On failure we
	// never consume pending-challenge capacity.
	serverNonce := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, serverNonce); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	challengeID := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, challengeID); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	now := time.Now().UTC()
	expiresAt := now.Add(5 * time.Minute)

	// Build and sign the transcript BEFORE inserting the challenge so a
	// host-signing failure doesn't leak an unusable challenge.
	transcript := AuthTranscript{
		Role: "host", HostID: h.Identity.HostID, DeviceID: dev.DeviceID,
		DaemonBootID: h.Sessions.BootID(),
		ChallengeID:  challengeID, ClientNonce: clientNonce, ServerNonce: serverNonce,
		CreatedAtMS: now.UnixMilli(), ExpiresAtMS: expiresAt.UnixMilli(),
	}
	hostSig, sigErr := h.Identity.Sign(transcript.Build())
	if sigErr != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Atomic insert: all pre-conditions are satisfied.
	ch := &PendingChallenge{
		ChallengeID: challengeID,
		DeviceID:    dev.DeviceID, ClientNonce: clientNonce, ServerNonce: serverNonce,
		HostID: h.Identity.HostID, DaemonBootID: h.Sessions.BootID(),
		IssuedAt: now, ExpiresAt: expiresAt,
	}
	if _, insErr := h.Challenges.Insert(ch); insErr != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(AuthChallengeResponse{
		Version:            1,
		ChallengeID:        hex.EncodeToString(challengeID),
		HostID:             h.Identity.HostID,
		HostKeyFingerprint: h.Identity.Fingerprint(),
		DaemonBootID:       h.Sessions.BootID(),
		ServerNonce:        hex.EncodeToString(serverNonce),
		ExpiresAt:          expiresAt,
		HostSignature:      hex.EncodeToString(hostSig),
	})
}

// ── POST /api/device-auth/verify ──

type VerifyRequest struct {
	Version     int    `json:"version"`
	ChallengeID string `json:"challengeId"` // hex
	DeviceID    string `json:"deviceId"`
	Signature   string `json:"signature"` // hex of DER ECDSA
}

type VerifyResponse struct {
	Token       string    `json:"token"`
	TokenType   string    `json:"tokenType"`
	ExpiresAt   time.Time `json:"expiresAt"`
	DeviceID    string    `json:"deviceId"`
	Permissions []string  `json:"permissions"`
}

// HandleVerify consumes the challenge, verifies the device signature against
// the registered P-256 public key, and issues a short-lived opaque bearer
// token. The raw token is returned exactly once and never stored or logged.
func (h *AuthHandler) HandleVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.Identity == nil || h.Registry == nil || h.Sessions == nil {
		http.Error(w, "device auth not configured", http.StatusServiceUnavailable)
		return
	}
	var req VerifyRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.Version != 1 {
		http.Error(w, "invalid version", http.StatusBadRequest)
		return
	}
	challengeID, err := hex.DecodeString(req.ChallengeID)
	if err != nil || len(challengeID) != 32 {
		http.Error(w, "invalid challenge id", http.StatusBadRequest)
		return
	}
	signature, err := hex.DecodeString(req.Signature)
	if err != nil || len(signature) == 0 {
		http.Error(w, "invalid signature", http.StatusBadRequest)
		return
	}

	// Verify the device signature FIRST (before consuming the challenge) so
	// a single bad signature does not delete the challenge and the
	// attempt-threshold contract is honoured.
	dev, devOK := h.Registry.GetActive(req.DeviceID)
	if !devOK {
		h.audit(AuditEvent{DeviceID: req.DeviceID, Action: ActionAuthVerify, Result: ResultDenied})
		http.Error(w, "device not active", http.StatusUnauthorized)
		return
	}
	pub, pubErr := ParseP256PublicKey(dev.PublicKeyDER)
	if pubErr != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// Look up the challenge data without consuming it yet.
	ch, cherr := h.Challenges.Find(challengeID)
	if cherr != nil {
		http.Error(w, "invalid or expired challenge", http.StatusUnauthorized)
		return
	}
	transcript := AuthTranscript{
		Role: "device", HostID: ch.HostID, DeviceID: dev.DeviceID,
		DaemonBootID: ch.DaemonBootID,
		ChallengeID:  challengeID, ClientNonce: ch.ClientNonce, ServerNonce: ch.ServerNonce,
		CreatedAtMS: ch.IssuedAt.UnixMilli(), ExpiresAtMS: ch.ExpiresAt.UnixMilli(),
	}
	digest := sha256.Sum256(transcript.Build())
	if !ecdsa.VerifyASN1(pub, digest[:], signature) {
		h.Challenges.RecordFailure(challengeID)
		h.audit(AuditEvent{DeviceID: dev.DeviceID, Action: ActionAuthVerify, Result: ResultDenied})
		http.Error(w, "signature verification failed", http.StatusUnauthorized)
		return
	}

	// Signature verified — now atomically consume the challenge.
	ch, cherr = h.Challenges.Consume(challengeID, []byte(req.DeviceID))
	if cherr != nil {
		http.Error(w, "invalid or expired challenge", http.StatusUnauthorized)
		return
	}

	// Role-based authorization: owner gets full, member gets restricted.
	perms := PermissionsForRole(dev.Role)
	if perms == nil {
		http.Error(w, "unknown device role", http.StatusInternalServerError)
		return
	}

	// Issue a session token. The session manager enforces a per-device cap
	// (one active session per device; new auth revokes the previous).
	rawToken, sessionID, expiresAt, tokErr := h.Sessions.CreateAfterVerifiedChallenge(
		dev.DeviceID, ch.HostID, ch.DaemonBootID, perms,
	)
	if tokErr != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.audit(AuditEvent{
		DeviceID: dev.DeviceID, Action: ActionAuthVerify,
		Result: ResultGranted, CorrelationID: sessionID,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(VerifyResponse{
		Token:       rawToken,
		TokenType:   "Bearer",
		ExpiresAt:   expiresAt,
		DeviceID:    dev.DeviceID,
		Permissions: perms,
	})
}
