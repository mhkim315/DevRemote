package devicetrust

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

// ── Pairing types ──

type PairingSession struct {
	SessionID   string    `json:"sessionId"`
	HostID      string    `json:"hostId"`
	Fingerprint string    `json:"fingerprint"`
	Endpoint    string    `json:"endpoint"`
	Secret      string    `json:"secret"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

func (ps *PairingSession) validate(candidateSecret string, candidatePub []byte) error {
	if time.Now().UTC().After(ps.ExpiresAt) {
		return errPairingExpired
	}
	if candidateSecret != ps.Secret {
		return errPairingSecretMismatch
	}
	if _, err := ParseP256PublicKey(candidatePub); err != nil {
		return err
	}
	return nil
}

type PairingRequest struct {
	PublicKeyDER []byte `json:"publicKey"`
	DisplayName  string `json:"displayName"`
	Secret       string `json:"secret"`
}

type PairingConfig struct {
	Listen          string
	SessionLifetime time.Duration
	Identity        IdentityProvider
	Registry        *DeviceRegistry
}

type IdentityProvider interface {
	Public() PublicHostIdentity
}

// ── PairingHost (owns the lifecycle) ──

// PairingHost allows the caller to drive the pairing lifecycle explicitly.
// The HTTP listener is started immediately; the Session is available
// synchronously so callers can show the QR / secret before waiting.
type PairingHost struct {
	Session  *PairingSession
	addr     string
	srv      *http.Server
	reg      *DeviceRegistry
	resultCh chan pairResult
}

type pairResult struct {
	paired Device
	ok     bool
}

// StartPairing creates a pairing session, binds a random port, and starts the
// LAN HTTP listener. Callers read .Session to show the QR, then call Wait()
// to block for a pairing or expiry. Stop() cancels the session early.
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
	session := &PairingSession{
		SessionID:   sessionID,
		HostID:      hostPub.HostID,
		Fingerprint: hostPub.Fingerprint,
		Endpoint:    "http://" + addr + "/pair",
		Secret:      secret,
		ExpiresAt:   time.Now().UTC().Add(cfg.SessionLifetime),
	}

	ph := &PairingHost{
		Session:  session,
		addr:     addr,
		reg:      cfg.Registry,
		resultCh: make(chan pairResult, 1),
	}
	var sessionOnce sync.Once
	consume := func() {
		sessionOnce.Do(func() { session.Secret = "" })
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/pair", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req PairingRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if session.Secret == "" {
			http.Error(w, "pairing session closed", http.StatusGone)
			return
		}
		if verr := session.validate(req.Secret, req.PublicKeyDER); verr != nil {
			code := http.StatusUnauthorized
			if verr == errPairingExpired {
				code = http.StatusGone
			}
			http.Error(w, verr.Error(), code)
			return
		}
		consume()
		dev, err := cfg.Registry.Add(req.PublicKeyDER, req.DisplayName)
		if err == nil {
			log.Printf("pairing: device %s paired (fingerprint %s)", dev.DeviceID, dev.Fingerprint)
			select {
			case ph.resultCh <- pairResult{paired: dev, ok: true}:
			default:
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			json.NewEncoder(w).Encode(map[string]string{"status": "rejected", "error": err.Error()})
		} else {
			json.NewEncoder(w).Encode(map[string]string{"status": "paired"})
		}
	})

	ph.srv = &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go ph.srv.Serve(ln)

	return ph, nil
}

// Wait blocks until a device is paired or the session expires. ok=true on
// successful pairing; the paired Device is returned either way.
func (ph *PairingHost) Wait(expiry time.Duration) (Device, bool) {
	timer := time.NewTimer(expiry + 2*time.Second)
	defer timer.Stop()
	select {
	case r := <-ph.resultCh:
		return r.paired, r.ok
	case <-timer.C:
		return Device{}, false
	}
}

// Close stops the listener and destroys the session secret.
func (ph *PairingHost) Close() {
	ph.Session.Secret = ""
	ph.srv.Close()
}

// ── Convenience: RunPairing for the pokit pair CLI ──

func RunPairing(cfg PairingConfig) (*PairingSession, Device, bool) {
	ph, err := StartPairing(cfg)
	if err != nil {
		log.Printf("pairing: %v", err)
		return nil, Device{}, false
	}
	defer ph.Close()
	d, ok := ph.Wait(cfg.SessionLifetime)
	return ph.Session, d, ok
}

// ── Errors ──

var (
	errPairingExpired        = pairingError("pairing session expired")
	errPairingSecretMismatch = pairingError("incorrect pairing secret")
)

type pairingError string

func (e pairingError) Error() string { return string(e) }
