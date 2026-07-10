package devicetrust

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"
)

// AuthHandler holds the daemon-owned dependencies for challenge-auth
// endpoints. It is handler-independent — M2.5-4 middleware will use the
// same DeviceSessionManager + DeviceRegistry.
type AuthHandler struct {
	Identity   *HostIdentity         // host key for signing challenges
	Registry   *DeviceRegistry       // paired-device registry
	Challenges *ChallengeStore       // single-use challenge store
	Sessions   *DeviceSessionManager // session token store
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

	serverNonce := make([]byte, 32)
	rand.Read(serverNonce)
	now := time.Now().UTC()
	expiresAt := now.Add(5 * time.Minute)

	ch := &PendingChallenge{
		DeviceID:     dev.DeviceID,
		ClientNonce:  clientNonce,
		ServerNonce:  serverNonce,
		HostID:       h.Identity.HostID,
		DaemonBootID: h.Sessions.BootID(),
		IssuedAt:     now,
		ExpiresAt:    expiresAt,
	}
	challengeID, err := h.Challenges.Insert(ch)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Host signs the challenge transcript (role="host").
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

	// Atomic consume: exactly one caller gets to verify this challenge.
	ch, cherr := h.Challenges.Consume(challengeID, []byte(req.DeviceID))
	if cherr != nil {
		http.Error(w, "invalid or expired challenge", http.StatusUnauthorized)
		return
	}

	// Verify the device signature (role="device").
	dev, devOK := h.Registry.GetActive(req.DeviceID)
	if !devOK {
		http.Error(w, "device not active", http.StatusUnauthorized)
		return
	}
	transcript := AuthTranscript{
		Role: "device", HostID: ch.HostID, DeviceID: dev.DeviceID,
		DaemonBootID: ch.DaemonBootID,
		ChallengeID:  challengeID, ClientNonce: ch.ClientNonce, ServerNonce: ch.ServerNonce,
		CreatedAtMS: ch.IssuedAt.UnixMilli(), ExpiresAtMS: ch.ExpiresAt.UnixMilli(),
	}
	pub, pubErr := ParseP256PublicKey(dev.PublicKeyDER)
	if pubErr != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	digest := sha256.Sum256(transcript.Build())
	if !ecdsa.VerifyASN1(pub, digest[:], signature) {
		http.Error(w, "signature verification failed", http.StatusUnauthorized)
		return
	}

	// Issue a session token.
	rawToken, _, expiresAt, tokErr := h.Sessions.CreateAfterVerifiedChallenge(
		dev.DeviceID, ch.HostID, ch.DaemonBootID,
		[]string{"sessions:read", "terminal:input"},
	)
	if tokErr != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(VerifyResponse{
		Token:       rawToken,
		TokenType:   "Bearer",
		ExpiresAt:   expiresAt,
		DeviceID:    dev.DeviceID,
		Permissions: []string{"sessions:read", "terminal:input"},
	})
}
