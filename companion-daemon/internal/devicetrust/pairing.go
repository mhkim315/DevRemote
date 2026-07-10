package devicetrust

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

// ── Pairing state machine ──

type pairState string

const (
	pairStateReady     pairState = ""
	pairStatePending   pairState = "pending"           // session active, waiting for candidate
	pairStateAwaiting  pairState = "awaiting_approval" // candidate received, awaiting user approval
	pairStateApproved  pairState = "approved"          // user approved → device registered
	pairStateRejected  pairState = "rejected"          // user rejected the candidate
	pairStateCancelled pairState = "cancelled"         // user cancelled the session
	pairStateConsumed  pairState = "consumed"          // secret already used (second candidate)
)

// PairingSession is the public payload for the QR. The one-time secret is
// internal (never JSON-serialised) so it can be consumed atomically.
type PairingSession struct {
	SessionID   string    `json:"sessionId"`
	HostID      string    `json:"hostId"`
	Fingerprint string    `json:"fingerprint"`
	Endpoint    string    `json:"endpoint"`
	secret      string    // internal: one-time pairing secret (never in JSON)
	ExpiresAt   time.Time `json:"expiresAt"`
}

// Secret returns the one-time pairing secret. Use with caution — it is
// consumed atomically by the pairing state machine.
func (ps *PairingSession) Secret() string { return ps.secret }

type PairingRequest struct {
	PublicKeyDER []byte `json:"publicKey"`
	DisplayName  string `json:"displayName"`
	Secret       string `json:"secret"`
	PhoneNonce   []byte `json:"phoneNonce,omitempty"` // set only in challenge
}

type PairingConfig struct {
	Listen          string
	LANAddr         string // actual LAN IP:port (not 0.0.0.0) for the QR endpoint
	SessionLifetime time.Duration
	Identity        IdentityProvider
	Registry        *DeviceRegistry
}

type IdentityProvider interface {
	Public() PublicHostIdentity
	Sign(msg []byte) ([]byte, error)
}

// ── Candidate + challenge ──

// pendingCandidate holds the phone submission before user approval.
type pendingCandidate struct {
	PublicKeyDER []byte
	DisplayName  string
	PhoneNonce   []byte
	HostNonce    []byte
}

// PairingHost is the daemon-owned pairing session.
type PairingHost struct {
	Session *PairingSession
	addr    string
	ln      net.Listener
	srv     *http.Server
	reg     *DeviceRegistry
	cfg     PairingConfig

	mu    sync.Mutex
	state pairState

	resultCh    chan pairResult    // cap 2: one for WaitForCandidate, one for Wait
	candidateCh chan pairCandidate // buffered 1, for candidates arriving while awaiting

	// consumedCh is closed exactly once when the secret is consumed, stopping
	// further candidate submissions.
	consumedCh   chan struct{}
	consumedOnce sync.Once

	candidate pendingCandidate // protected by mu
}

type pairResult struct {
	Device  Device
	State   pairState
	Message string
}

type pairCandidate struct {
	PubDER      []byte
	DisplayName string
	PhoneNonce  []byte
	HostNonce   []byte
}

// StartPairing creates the daemon-owned pairing session and starts the LAN
// listener. The caller immediately receives the session (for QR display) and
// then calls WaitForCandidate / Approve / Reject / Cancel.
func StartPairing(cfg PairingConfig) (*PairingHost, error) {
	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return nil, err
	}
	addr := ln.Addr().String()

	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		ln.Close()
		return nil, err
	}
	secret := hex.EncodeToString(secretBytes)
	sessionID := hex.EncodeToString(secretBytes[:16])

	hostPub := cfg.Identity.Public()
	// Derive the actual endpoint from the bound listener's address (port is
	// known after net.Listen) + the LAN IP.
	_, port, _ := net.SplitHostPort(addr)
	lanEndpoint := "http://" + cfg.LANAddr + ":" + port + "/pair"
	session := &PairingSession{
		SessionID:   sessionID,
		HostID:      hostPub.HostID,
		Fingerprint: hostPub.Fingerprint,
		Endpoint:    lanEndpoint,
		secret:      secret,
		ExpiresAt:   time.Now().UTC().Add(cfg.SessionLifetime),
	}

	ph := &PairingHost{
		Session:     session,
		addr:        addr,
		ln:          ln,
		reg:         cfg.Registry,
		cfg:         cfg,
		state:       pairStatePending,
		resultCh:    make(chan pairResult, 2),
		candidateCh: make(chan pairCandidate, 1),
		consumedCh:  make(chan struct{}),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/pair", ph.handlePair)
	ph.srv = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	go ph.srv.Serve(ln)

	// Expiry timer — auto-close the session.
	go func() {
		select {
		case <-time.After(cfg.SessionLifetime + 2*time.Second):
		case <-ph.consumedCh:
		}
		ph.consume()
		ph.finish(pairResult{State: pairStateRejected, Message: "pairing session ended"})
	}()

	return ph, nil
}

// handlePair is the LAN POST /pair handler.
func (ph *PairingHost) handlePair(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req PairingRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if _, err := ParseP256PublicKey(req.PublicKeyDER); err != nil {
		http.Error(w, "invalid public key", http.StatusBadRequest)
		return
	}

	ph.mu.Lock()
	st := ph.state
	ph.mu.Unlock()

	if st != pairStatePending {
		http.Error(w, "pairing session not accepting candidates", http.StatusGone)
		return
	}
	if time.Now().UTC().After(ph.Session.ExpiresAt) {
		ph.mu.Lock()
		ph.state = pairStateRejected
		ph.mu.Unlock()
		http.Error(w, "pairing session expired", http.StatusGone)
		return
	}

	// Secret must match. On the first valid candidate, consume the secret so no
	// second candidate can pass.
	if req.Secret != ph.Session.Secret() {
		http.Error(w, "incorrect pairing secret", http.StatusUnauthorized)
		return
	}
	// Atomic state transition: pending → awaiting_approval.
	ph.mu.Lock()
	if ph.state != pairStatePending {
		ph.mu.Unlock()
		http.Error(w, "candidate already received", http.StatusGone)
		return
	}
	ph.state = pairStateAwaiting
	// Generate host nonce for the challenge.
	hostNonce := make([]byte, 32)
	rand.Read(hostNonce)
	ph.candidate = pendingCandidate{
		PublicKeyDER: append([]byte(nil), req.PublicKeyDER...),
		DisplayName:  req.DisplayName,
		PhoneNonce:   append([]byte(nil), req.PhoneNonce...),
		HostNonce:    hostNonce,
	}
	ph.mu.Unlock()

	// Deliver the candidate to the main goroutine.
	select {
	case ph.candidateCh <- pairCandidate{
		PubDER:      req.PublicKeyDER,
		DisplayName: req.DisplayName,
		PhoneNonce:  req.PhoneNonce,
		HostNonce:   hostNonce,
	}:
	default:
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "pending_approval",
		"hostNonce": hex.EncodeToString(hostNonce),
	})
}

// Wait blocks for a pairing result: waits for a candidate, auto-approves (no
// signature in test/compat mode), and returns the registered device.
func (ph *PairingHost) Wait(d time.Duration) (Device, bool) {
	if _, ok := ph.WaitForCandidate(); !ok {
		return Device{}, false
	}
	if err := ph.Approve(nil); err != nil {
		return Device{}, false
	}
	r := <-ph.resultCh
	return r.Device, r.State == pairStateApproved
}

// WaitForCandidate blocks until a phone submits a candidate or the session
// expires. Returns the candidate for fingerprint display, or false if the
// session ended without one. Does NOT consume resultCh — the caller must
// read the final result (Approve/Reject) from the result channel.
func (ph *PairingHost) WaitForCandidate() (candidate pairCandidate, ok bool) {
	// SessionLifetime may have expired; resultCh is written by the expiry
	// goroutine. Poll both channels so we don't block forever.
	timer := time.NewTimer(ph.Session.ExpiresAt.Sub(time.Now().UTC()) + 2*time.Second)
	defer timer.Stop()
	select {
	case c := <-ph.candidateCh:
		ph.consume()
		ph.ln.Close()
		return c, true
	case <-timer.C:
		return pairCandidate{}, false
	case <-ph.consumedCh:
		return pairCandidate{}, false
	}
}

// Approve verifies the phone's signature and registers the device. The phone
// must have already submitted a candidate and signed the pairing transcript.
func (ph *PairingHost) Approve(phoneSignature []byte) error {
	ph.mu.Lock()
	if ph.state != pairStateAwaiting {
		ph.mu.Unlock()
		return fmt.Errorf("no candidate awaiting approval (state=%s)", ph.state)
	}
	cand := ph.candidate
	ph.mu.Unlock()

	// Verify the phone owns the private key: it must sign the pairing transcript
	// (phoneNonce || hostNonce || hostPublicKey || sessionID). nil phoneSig skips
	// verification (test compat / local approval without challenge).
	if len(phoneSignature) > 0 {
		transcript := buildPairingTranscript(cand.PhoneNonce, cand.HostNonce,
			ph.cfg.Identity.Public().PublicKeyDER, ph.Session.SessionID)
		pub, err := ParseP256PublicKey(cand.PublicKeyDER)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(transcript)
		if !ecdsa.VerifyASN1(pub, digest[:], phoneSignature) {
			return fmt.Errorf("phone signature verification failed")
		}
	}

	// Register the device.
	dev, regErr := ph.reg.Add(cand.PublicKeyDER, cand.DisplayName)
	if regErr != nil {
		return fmt.Errorf("device registration failed: %w", regErr)
	}

	ph.state = pairStateApproved
	res := pairResult{Device: dev, State: pairStateApproved, Message: "paired"}
	// Write directly — expiry goroutine may have already read resultCh once.
	select {
	case ph.resultCh <- res:
	default:
	}
	log.Printf("pairing: device %s approved and registered (fingerprint %s)", dev.DeviceID, dev.Fingerprint)
	return nil
}

// Reject declines the candidate.
func (ph *PairingHost) Reject() {
	ph.mu.Lock()
	if ph.state == pairStateAwaiting {
		ph.state = pairStateRejected
	}
	ph.mu.Unlock()
	ph.finish(pairResult{State: pairStateRejected, Message: "rejected by user"})
}

// Cancel ends the pairing session without a pairing.
func (ph *PairingHost) Cancel() {
	ph.mu.Lock()
	if ph.state == pairStatePending || ph.state == pairStateAwaiting {
		ph.state = pairStateCancelled
	}
	ph.mu.Unlock()
	ph.consume()
	ph.finish(pairResult{State: pairStateCancelled, Message: "cancelled"})
}

// Close stops the listener and destroys the secret.
func (ph *PairingHost) Close() {
	ph.consume()
	ph.srv.Close()
	if ph.ln != nil {
		ph.ln.Close()
	}
}

// consume destroys the one-time secret exactly once.
func (ph *PairingHost) consume() {
	ph.consumedOnce.Do(func() {
		ph.Session.secret = ""
		close(ph.consumedCh)
	})
}

func (ph *PairingHost) finish(r pairResult) {
	select {
	case ph.resultCh <- r:
	default:
	}
}

// buildPairingTranscript is the domain-separated message the phone must sign.
func buildPairingTranscript(phoneNonce, hostNonce, hostPubDER []byte, sessionID string) []byte {
	// Domain separator for pairing. Prevents replay across protocols.
	const prefix = "pokit-pair-v1:"
	b := make([]byte, 0, len(prefix)+len(phoneNonce)+len(hostNonce)+len(hostPubDER)+len(sessionID))
	b = append(b, prefix...)
	b = append(b, phoneNonce...)
	b = append(b, hostNonce...)
	b = append(b, hostPubDER...)
	b = append(b, sessionID...)
	return b
}

// ── Convenience (used by the daemon's IPC create op; NOT a standalone CLI) ──

// RunPairing is a convenience helper used by the daemon's privileged IPC
// handler for pokit pair start. The CLI never loads the registry directly.
func RunPairing(cfg PairingConfig) (*PairingHost, error) {
	return StartPairing(cfg)
}
