package term

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"devremote/companion-daemon/internal/devicetrust"
)

// ── Input-B versioned control-request protocol (§6.2-6.3) ──

const (
	inputMaxRawFrame = 8192
	inputMaxDecoded  = 4096
	inputIDLen       = 64
	inputCacheSize   = 64
	inputCacheTTL    = 30 * time.Second
)

// inputControlRequest is the versioned terminal-input control message sent as a
// WebSocket TextMessage. Every field is validated before any write.
type inputControlRequest struct {
	Type       string `json:"type"`    // "terminal_input"
	Version    int    `json:"version"` // 1
	SessionID  string `json:"sessionId"`
	Generation int64  `json:"generation"`
	InputID    string `json:"inputId"` // exactly 64 lowercase hex
	Payload    string `json:"payload"` // base64-encoded input bytes
}

// inputResult is the closed result vocabulary returned to the client.
type inputResult struct {
	Type         string `json:"type"` // "input_result"
	InputID      string `json:"inputId"`
	ConnectionID string `json:"connectionId,omitempty"`
	SessionID    string `json:"sessionId,omitempty"`
	Generation   int64  `json:"generation"`
	Sequence     uint64 `json:"sequence,omitempty"`
	Outcome      string `json:"outcome"`
}

// validInputID returns true when s is exactly 64 lowercase hex characters.
func validInputID(s string) bool {
	if len(s) != inputIDLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// parseInputControlRequest validates and decodes a raw JSON text frame into a
// control request. Unknown fields, trailing data, and malformed input cause
// rejection with distinct error values so callers can map to protocol outcomes.
func parseInputControlRequest(raw []byte) (*inputControlRequest, []byte, error) {
	if len(raw) > inputMaxRawFrame {
		return nil, nil, fmt.Errorf("input_too_large")
	}
	var req inputControlRequest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return nil, nil, fmt.Errorf("invalid_request")
	}
	// Reject trailing data.
	if dec.More() {
		return nil, nil, fmt.Errorf("invalid_request")
	}
	if req.Type != "terminal_input" || req.Version != 1 {
		return &req, nil, fmt.Errorf("invalid_request")
	}
	if !validInputID(req.InputID) {
		return &req, nil, fmt.Errorf("invalid_request")
	}
	decoded, err := base64.StdEncoding.DecodeString(req.Payload)
	if err != nil {
		return &req, nil, fmt.Errorf("invalid_request")
	}
	if len(decoded) > inputMaxDecoded {
		return &req, nil, fmt.Errorf("input_too_large")
	}
	return &req, decoded, nil
}

// inputCacheEntry stores a prior result for duplicate detection.
type inputCacheEntry struct {
	digest   [32]byte
	outcome  string
	sequence uint64
	expires  time.Time
}

// inputRecentCache is a bounded per-connection cache keyed by inputID.
// Duplicate requests return the stored outcome without re-writing.
type inputRecentCache struct {
	mu      sync.Mutex
	entries map[string]*inputCacheEntry
	order   []string // LRU eviction order
}

func newInputRecentCache() *inputRecentCache {
	return &inputRecentCache{entries: make(map[string]*inputCacheEntry, inputCacheSize)}
}

// requestDigest computes a deterministic hash of execution-relevant fields.
func requestDigest(req *inputControlRequest) [32]byte {
	h := sha256.New()
	h.Write([]byte(req.SessionID))
	h.Write([]byte{byte(req.Generation >> 56), byte(req.Generation >> 48), byte(req.Generation >> 40), byte(req.Generation >> 32),
		byte(req.Generation >> 24), byte(req.Generation >> 16), byte(req.Generation >> 8), byte(req.Generation)})
	h.Write([]byte(req.Payload))
	var d [32]byte
	h.Sum(d[:0])
	return d
}

// get returns a cached outcome if inputID matches with identical digest.
func (c *inputRecentCache) get(inputID string, digest [32]byte) (string, uint64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[inputID]
	if !ok || e.digest != digest {
		return "", 0, false
	}
	if time.Now().After(e.expires) {
		c.evictLocked(inputID)
		return "", 0, false
	}
	return e.outcome, e.sequence, true
}

// getConflict returns true when inputID exists with a DIFFERENT digest.
func (c *inputRecentCache) getConflict(inputID string, digest [32]byte) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[inputID]
	if !ok {
		return false
	}
	if time.Now().After(e.expires) {
		c.evictLocked(inputID)
		return false
	}
	return e.digest != digest
}

// store records a result under inputID. Evicts oldest entry if at capacity.
func (c *inputRecentCache) store(inputID string, digest [32]byte, outcome string, sequence uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Purge expired entries.
	c.purgeLocked()
	// Evict oldest if at capacity.
	for len(c.entries) >= inputCacheSize && len(c.order) > 0 {
		c.evictLocked(c.order[0])
	}
	c.entries[inputID] = &inputCacheEntry{
		digest:   digest,
		outcome:  outcome,
		sequence: sequence,
		expires:  time.Now().Add(inputCacheTTL),
	}
	c.order = append(c.order, inputID)
}

// clear removes all entries (connection close).
func (c *inputRecentCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*inputCacheEntry, inputCacheSize)
	c.order = nil
}

func (c *inputRecentCache) evictLocked(inputID string) {
	delete(c.entries, inputID)
	for i, id := range c.order {
		if id == inputID {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
}

func (c *inputRecentCache) purgeLocked() {
	now := time.Now()
	var keep []string
	for _, id := range c.order {
		e := c.entries[id]
		if now.After(e.expires) {
			delete(c.entries, id)
		} else {
			keep = append(keep, id)
		}
	}
	c.order = keep
}

// handleTerminalInput processes a versioned terminal_input control request
// and returns the result JSON bytes to send, or nil if nothing to send.
func handleTerminalInput(
	msg []byte,
	session string,
	inputGeneration int64,
	inputTransport *TerminalTransport,
	transcriptSvc interface{ BeginInput(string, time.Time) },
	inputSequence *uint64,
	ticketPrincipal interface{},
	recentCache *inputRecentCache,
	connID string,
) []byte {
	mkResult := func(req *inputControlRequest, outcome string, seq uint64) []byte {
		b, _ := json.Marshal(inputResult{
			Type: "input_result", InputID: req.InputID,
			ConnectionID: connID, SessionID: req.SessionID,
			Generation: req.Generation, Sequence: seq, Outcome: outcome,
		})
		return b
	}

	req, decoded, parseErr := parseInputControlRequest(msg)
	if parseErr != nil {
		outcome := "invalid_request"
		if parseErr.Error() == "input_too_large" {
			outcome = "input_too_large"
		}
		result := inputResult{Type: "input_result", ConnectionID: connID, Outcome: outcome}
		if req != nil {
			result.InputID = req.InputID
			result.SessionID = req.SessionID
			result.Generation = req.Generation
		}
		b, _ := json.Marshal(result)
		return b
	}

	tp, _ := ticketPrincipal.(*devicetrust.Principal)
	if tp != nil && !hasTicketPerm(tp, string(devicetrust.PermTerminalInput)) {
		return mkResult(req, "permission_denied", 0)
	}

	if req.SessionID != session {
		b, _ := json.Marshal(inputResult{
			Type: "input_result", InputID: req.InputID,
			ConnectionID: connID, SessionID: req.SessionID,
			Generation: req.Generation, Outcome: "session_not_found",
		})
		return b
	}
	if req.Generation != inputGeneration {
		b, _ := json.Marshal(inputResult{
			Type: "input_result", InputID: req.InputID,
			ConnectionID: connID, SessionID: req.SessionID,
			Generation: inputGeneration, Outcome: "stale_generation",
		})
		return b
	}

	digest := requestDigest(req)
	if outcome, seq, ok := recentCache.get(req.InputID, digest); ok {
		return mkResult(req, outcome, seq)
	}
	if recentCache.getConflict(req.InputID, digest) {
		return mkResult(req, "invalid_request", 0)
	}

	if inputTransport == nil {
		return mkResult(req, "transport_closed", 0)
	}

	if transcriptSvc != nil {
		transcriptSvc.BeginInput(session, time.Now())
	}

	written, inErr := inputTransport.WriteInput(decoded)
	var outcome string
	var seq uint64
	if inErr != nil || written != len(decoded) {
		outcome = "write_failed"
	} else {
		outcome = "accepted"
		*inputSequence++
		seq = *inputSequence
	}

	recentCache.store(req.InputID, digest, outcome, seq)
	return mkResult(req, outcome, seq)
}
