package devicetrust

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

// ── State machine ──

// pairState is internal state; PairingState is the public API view.
type pairState string

const (
	pairStatePending               pairState = "pending"
	pairStateChallenged            pairState = "challenged"
	pairStateProofVerified         pairState = "proof_verified"
	pairStateAwaitingLocalApproval pairState = "awaiting_local_approval"
	pairStateApproved              pairState = "approved"
	pairStateRejected              pairState = "rejected"
	pairStateExpired               pairState = "expired"
)

// PairingState is the client-facing lifecycle enum.
type PairingState string

const (
	PairingStatePending          PairingState = "pending"
	PairingStateAwaitingApproval PairingState = "awaiting_approval"
	PairingStateApproved         PairingState = "approved"
	PairingStateRejected         PairingState = "rejected"
	PairingStateExpired          PairingState = "expired"
)

func stateForClient(s pairState) PairingState {
	switch s {
	case pairStatePending, pairStateChallenged:
		return PairingStatePending
	case pairStateProofVerified, pairStateAwaitingLocalApproval:
		return PairingStateAwaitingApproval
	case pairStateApproved:
		return PairingStateApproved
	case pairStateRejected:
		return PairingStateRejected
	default:
		return PairingStateExpired
	}
}

// ── Public types ──

type PairingSession struct {
	SessionID      string    `json:"sessionId"`
	HostID         string    `json:"hostId"`
	Fingerprint    string    `json:"fingerprint"`
	HostPubKeyB64  string    `json:"hostPubKey"`
	BootstrapToken string    `json:"bootstrapToken"`
	Endpoint       string    `json:"endpoint"`
	ExpiresAt      time.Time `json:"expiresAt"`
}

type PairingRequest struct {
	PublicKeyDER   []byte `json:"publicKey"`
	DisplayName    string `json:"displayName"`
	PhoneNonce     []byte `json:"phoneNonce"`
	BootstrapToken string `json:"bootstrapToken"` // QR secret — gates Phase 1
	QRHostID       string `json:"qrHostId,omitempty"`
	QRDaemonBootID string `json:"qrDaemonBootId,omitempty"`
	QRChallengeID  string `json:"qrChallengeId,omitempty"`
	QRExpiresAt    string `json:"qrExpiresAt,omitempty"`
}

type ChallengeResponse struct {
	HostNonce       []byte `json:"hostNonce"`
	HostPublicDER   []byte `json:"hostPublicKey"`
	HostFingerprint string `json:"hostFingerprint"`
	ExpiresAt       string `json:"expiresAt"`
}

type Confirmation struct {
	PhoneSignature []byte `json:"phoneSignature"` // ECDSA ASN.1 over pairing transcript
}

type PairingConfig struct {
	Listen          string
	LANAddr         string
	SessionLifetime time.Duration
	Identity        IdentityProvider
	Signer          HostSigner // mandatory: signs host proof for phone verification
	Registry        *DeviceRegistry
}

type IdentityProvider interface {
	Public() PublicHostIdentity
}

// HostSigner is an IdentityProvider that can also sign data with the host's
// private key (host key-possession proof for phone verification).
type HostSigner interface {
	IdentityProvider
	Sign(msg []byte) ([]byte, error)
}

type Candidate struct {
	PubDER         []byte
	DisplayName    string
	Fingerprint    string
	PhoneNonce     []byte
	HostNonce      []byte
	QRHostID       string
	QRDaemonBootID string
	QRChallengeID  string
	QRExpiresAt    string
}

// ── PairingHost (daemon-owned, fully serialized) ──

type PairingHost struct {
	Session *PairingSession
	addr    string // listener address (ip:port)
	ln      net.Listener
	srv     *http.Server
	reg     *DeviceRegistry
	cfg     PairingConfig

	mu           sync.Mutex
	state        pairState
	candidate    pendingCandidate
	pairedDevice Device

	// Events: one-shot channels closed on transitions.
	proofVerifiedCh chan struct{} // closed when phone key-possession is proven
	doneCh          chan struct{} // closed when session completes
	expiryTimer     *time.Timer
}

type pendingCandidate struct {
	PublicKeyDER   []byte
	DisplayName    string
	PhoneNonce     []byte
	HostNonce      []byte
	QRHostID       string
	QRDaemonBootID string
	QRChallengeID  string
	QRExpiresAt    string
}

// StartPairing creates the pairing session and starts the LAN listener.
// Caller receives session immediately for QR display.
func StartPairing(cfg PairingConfig) (*PairingHost, error) {
	if cfg.Signer == nil {
		return nil, fmt.Errorf("host signer is required (host key-possession proof)")
	}
	// If not explicitly set, auto-detect the private LAN address. 127.0.0.1
	// is accepted for tests and the fallback single-machine case.
	if cfg.LANAddr == "" {
		cfg.LANAddr = lanIP()
		if cfg.LANAddr == "" {
			cfg.LANAddr = "127.0.0.1"
		}
	}

	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return nil, fmt.Errorf("pairing listen: %w", err)
	}
	// Derive the local address for test/local connections. The QR endpoint
	// advertises the LAN IP; the listener addr is for same-machine callers.
	listenAddr := ln.Addr().String()
	_, port, _ := net.SplitHostPort(listenAddr)

	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		ln.Close()
		return nil, err
	}
	sessionID := hex.EncodeToString(secretBytes[:16])
	// bootstrapToken gates Phase 1 — only the QR scanner knows it. Without it
	// an attacker on the LAN could preempt the session with their own candidate.
	bootstrapToken := hex.EncodeToString(secretBytes[16:])

	hostPub := cfg.Identity.Public()
	session := &PairingSession{
		SessionID:      sessionID,
		HostID:         hostPub.HostID,
		Fingerprint:    hostPub.Fingerprint,
		HostPubKeyB64:  hex.EncodeToString(hostPub.PublicKeyDER),
		BootstrapToken: bootstrapToken,
		// QR endpoint: LAN IP for the phone; same-machine tests use ph.addr.
		Endpoint:  "http://" + cfg.LANAddr + ":" + port + "/pair",
		ExpiresAt: time.Now().UTC().Add(cfg.SessionLifetime),
	}

	ph := &PairingHost{
		Session:         session,
		addr:            "127.0.0.1:" + port, // localhost for same-machine callers
		ln:              ln,
		reg:             cfg.Registry,
		cfg:             cfg,
		state:           pairStatePending,
		proofVerifiedCh: make(chan struct{}),
		doneCh:          make(chan struct{}),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/pair", ph.handleCandidate)       // phase 1
	mux.HandleFunc("/pair/confirm", ph.handleConfirm) // phase 2
	mux.HandleFunc("/pair/result", ph.handleResult)   // poll for CLI approval
	ph.srv = &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	go ph.srv.Serve(ln)

	// Store the timer under the mutex so expire() can safely read/stop it.
	ph.mu.Lock()
	ph.expiryTimer = time.AfterFunc(cfg.SessionLifetime, ph.expire)
	ph.mu.Unlock()
	return ph, nil
}

// ── LAN endpoints (2-phase) ──

// Phase 1: phone submits candidate → host returns challenge + host proof.
func (ph *PairingHost) handleCandidate(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req PairingRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if _, err := ParseP256PublicKey(req.PublicKeyDER); err != nil {
		http.Error(w, "invalid public key", http.StatusBadRequest)
		return
	}
	if len(req.PhoneNonce) < 8 || len(req.PhoneNonce) > 256 {
		http.Error(w, "invalid phone nonce", http.StatusBadRequest)
		return
	}
	// QR bootstrap: only the QR scanner knows the bootstrap token, so an
	// attacker on the LAN cannot preempt the session.
	if subtle.ConstantTimeCompare([]byte(req.BootstrapToken), []byte(ph.Session.BootstrapToken)) != 1 {
		http.Error(w, "invalid bootstrap token", http.StatusUnauthorized)
		return
	}

	ph.mu.Lock()
	// Check expiry first — the timer goroutine may race.
	if time.Now().UTC().After(ph.Session.ExpiresAt) {
		ph.state = pairStateExpired
		ph.mu.Unlock()
		log.Printf("PAIR-410-EXPIRED: now=%v expires=%v state=%v", time.Now().UTC(), ph.Session.ExpiresAt, ph.state)
		http.Error(w, "session expired", http.StatusGone)
		return
	}
	if ph.state != pairStatePending {
		ph.mu.Unlock()
		log.Printf("PAIR-410-DUPCANDIDATE: state=%v", ph.state)
		http.Error(w, "candidate already received", http.StatusGone)
		return
	}
	ph.state = pairStateChallenged

	hostNonce := make([]byte, 32)
	if _, err := rand.Read(hostNonce); err != nil {
		ph.mu.Unlock()
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	hostPub := ph.cfg.Identity.Public()
	ph.candidate = pendingCandidate{
		PublicKeyDER:   append([]byte(nil), req.PublicKeyDER...),
		DisplayName:    req.DisplayName,
		PhoneNonce:     append([]byte(nil), req.PhoneNonce...),
		HostNonce:      append([]byte(nil), hostNonce...),
		QRHostID:       req.QRHostID,
		QRDaemonBootID: req.QRDaemonBootID,
		QRChallengeID:  req.QRChallengeID,
		QRExpiresAt:    req.QRExpiresAt,
	}
	ph.mu.Unlock()

	// Do NOT notify the CLI yet — candidate is only submitted, not proven.
	// Notification happens in handleConfirm after proof_verified.

	resp := ChallengeResponse{
		HostNonce:       hostNonce,
		HostPublicDER:   hostPub.PublicKeyDER,
		HostFingerprint: hostPub.Fingerprint,
		ExpiresAt:       ph.Session.ExpiresAt.Format(time.RFC3339),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// Phase 2: phone signs the transcript → verify → awaiting local approval.
func (ph *PairingHost) handleConfirm(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req Confirmation
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if len(req.PhoneSignature) == 0 {
		http.Error(w, "phone signature required", http.StatusBadRequest)
		return
	}

	ph.mu.Lock()
	if ph.state != pairStateChallenged {
		ph.mu.Unlock()
		log.Printf("PAIR-410-NOCHALLENGE: state=%v", ph.state)
		http.Error(w, "no challenge in progress", http.StatusGone)
		return
	}
	cand := ph.candidate

	// Verify phone key-possession: phone must sign the pairing transcript
	// (phoneNonce || hostNonce || hostPubKey || sessionId).
	transcript := buildPairingTranscript(cand.PhoneNonce, cand.HostNonce,
		ph.cfg.Identity.Public().PublicKeyDER, ph.Session.SessionID)
	pub, err := ParseP256PublicKey(cand.PublicKeyDER)
	if err != nil {
		ph.mu.Unlock()
		http.Error(w, "invalid candidate key", http.StatusBadRequest)
		return
	}
	digest := sha256.Sum256(transcript)
	if !ecdsa.VerifyASN1(pub, digest[:], req.PhoneSignature) {
		ph.mu.Unlock()
		http.Error(w, "signature verification failed", http.StatusUnauthorized)
		return
	}

	// Host identity proof: sign the same transcript so the phone can verify
	// the host key matches the QR fingerprint (host pinning). This MUST run
	// BEFORE the state transition — a signing failure leaves the session in
	// "challenged" and the proofVerifiedCh closed, so the CLI never sees a
	// candidate and cannot approve.
	hostProofDER, signErr := ph.cfg.Signer.Sign(transcript)
	if signErr != nil {
		ph.mu.Unlock()
		http.Error(w, "host signing failed", http.StatusInternalServerError)
		return
	}
	// Self-verify: the host proof must validate against the Identity's public
	// key (the same key pinned in the QR). This catches a mismatch between
	// the Identity and Signer fields in PairingConfig.
	if !VerifySignature(ph.cfg.Identity.Public().PublicKeyDER, transcript, hostProofDER) {
		ph.mu.Unlock()
		http.Error(w, "host proof verification failed", http.StatusInternalServerError)
		return
	}

	// Only now — phone key-possession + host key-possession both proven.
	ph.state = pairStateProofVerified
	close(ph.proofVerifiedCh)
	ph.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "proof_verified",
		"hostProof": hex.EncodeToString(hostProofDER),
	})
}

// handleResult is a polling endpoint the phone calls after confirm to learn the
// operator's approval decision. Does not consume the session.
func (ph *PairingHost) handleResult(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Basic session binding: the caller must know the session ID (obtained from
	// the QR). A third party on the same LAN without the QR cannot poll.
	if r.URL.Query().Get("session") != ph.Session.SessionID {
		http.Error(w, "session mismatch", http.StatusForbidden)
		return
	}
	ph.mu.Lock()
	dev := ph.pairedDevice
	state := stateForClient(ph.state)
	ph.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	switch state {
	case PairingStatePending, PairingStateAwaitingApproval:
		json.NewEncoder(w).Encode(map[string]string{"status": "pending"})
	case PairingStateApproved:
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":      "approved",
			"deviceId":    dev.DeviceID,
			"fingerprint": dev.Fingerprint,
			"role":        dev.Role,
		})
	case PairingStateRejected:
		json.NewEncoder(w).Encode(map[string]string{"status": "rejected"})
	case PairingStateExpired:
		json.NewEncoder(w).Encode(map[string]string{"status": "expired"})
	default:
		json.NewEncoder(w).Encode(map[string]string{"status": "error"})
	}
}

// ── State transitions (called by IPC handler) ──

// WaitForCandidate blocks until the phone has proven key-possession
// (proof_verified) or the session expires. The candidate is returned only
// after the proof — the CLI shows the fingerprint at approval time.
func (ph *PairingHost) WaitForCandidate() (Candidate, bool) {
	select {
	case <-ph.proofVerifiedCh:
		ph.mu.Lock()
		c := Candidate{
			PubDER:         ph.candidate.PublicKeyDER,
			DisplayName:    ph.candidate.DisplayName,
			Fingerprint:    Fingerprint(ph.candidate.PublicKeyDER),
			PhoneNonce:     ph.candidate.PhoneNonce,
			HostNonce:      ph.candidate.HostNonce,
			QRHostID:       ph.candidate.QRHostID,
			QRDaemonBootID: ph.candidate.QRDaemonBootID,
			QRChallengeID:  ph.candidate.QRChallengeID,
			QRExpiresAt:    ph.candidate.QRExpiresAt,
		}
		ph.mu.Unlock()
		return c, true
	case <-ph.doneCh:
		return Candidate{}, false
	}
}

// Approve transitions to approved and registers the device. Must only be
// called after state = proof_verified (or in test compat mode).
func (ph *PairingHost) Approve() error {
	ph.mu.Lock()
	defer ph.mu.Unlock()

	if ph.state != pairStateProofVerified {
		if ph.state == pairStateChallenged {
			// Phone submitted candidate but hasn't confirmed yet.
			return fmt.Errorf("phone has not completed key-possession proof")
		}
		return fmt.Errorf("session is not in a state that allows approval")
	}

	dev, err := ph.reg.Add(ph.candidate.PublicKeyDER, ph.candidate.DisplayName)
	if err != nil {
		return fmt.Errorf("device registration failed: %w", err)
	}
	ph.state = pairStateApproved
	ph.pairedDevice = dev

	log.Printf("pairing: device %s approved and registered (fingerprint %s)",
		dev.DeviceID, dev.Fingerprint)

	// Keep the listener alive briefly so the phone can poll /pair/result.
	go func() { time.AfterFunc(10*time.Second, ph.Close) }()
	return nil
}

// Reject declines the candidate.
func (ph *PairingHost) Reject() {
	ph.mu.Lock()
	if ph.state != pairStateApproved && ph.state != pairStateRejected {
		ph.state = pairStateRejected
	}
	ph.mu.Unlock()
	// Same grace period as approve so the phone can poll /pair/result.
	go func() { time.AfterFunc(10*time.Second, ph.Close) }()
}

func (ph *PairingHost) expire() {
	ph.mu.Lock()
	if ph.state == pairStatePending || ph.state == pairStateChallenged ||
		ph.state == pairStateProofVerified || ph.state == pairStateAwaitingLocalApproval {
		ph.state = pairStateExpired
	}
	ph.mu.Unlock()
	close(ph.doneCh)
	go ph.Close()
}

// Close stops the listener and server. Idempotent.
func (ph *PairingHost) Close() {
	ph.ln.Close()
	ph.srv.Close()
}

// Addr returns the localhost address for same-machine tests.
func (ph *PairingHost) Addr() string { return ph.addr }

// Result returns the final pairing outcome.
func (ph *PairingHost) Result() (Device, PairingState) {
	ph.mu.Lock()
	defer ph.mu.Unlock()
	return ph.pairedDevice, stateForClient(ph.state)
}

// ── helpers ──

func buildPairingTranscript(phoneNonce, hostNonce, hostPubDER []byte, sessionID string) []byte {
	prefix := []byte("pokit-pair-v1:")
	b := make([]byte, 0, len(prefix)+len(phoneNonce)+len(hostNonce)+len(hostPubDER)+len(sessionID))
	b = append(b, prefix...)
	b = append(b, phoneNonce...)
	b = append(b, hostNonce...)
	b = append(b, hostPubDER...)
	b = append(b, sessionID...)
	return b
}

func lanIP() string {
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			if ipnet.IP.IsPrivate() {
				return ipnet.IP.String()
			}
		}
	}
	return ""
}
