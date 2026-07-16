// Package term — C1D: daemon-owned private local hook bridge for Claude
// PreToolUse observation. Each managed Claude runtime gets an unguessable
// per-runtime capability token and a private HTTP endpoint on localhost.
//
// The bridge accepts only strict, bounded PreToolUse fields and initially
// returns defer only (C1D observation-only). Unknown fields, oversized input,
// missing capability, and duplicate tool-use IDs fail closed — the hook
// still returns defer (so Claude continues) but no observation is stored.
package term

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// maxPreToolUseBody is the maximum request body size for a PreToolUse hook
	// payload. Larger payloads are rejected before any decode.
	maxPreToolUseBody = 64 << 10 // 64 KiB
	// maxToolInputBytes is the maximum raw tool_input size accepted. Larger
	// inputs fail closed.
	maxToolInputBytes = 32 << 10 // 32 KiB
	// maxToolNameLen bounds the tool_name string.
	maxToolNameLen = 128
	// maxClaudeSessionIDLen bounds the session_id string.
	maxClaudeSessionIDLen = 128
	// maxToolUseIDLen bounds the tool_use_id string.
	maxToolUseIDLen = 128
	// capabilityTokenLen is the byte length of the random capability token
	// (hex-encoded, so 2x in the URL).
	capabilityTokenLen = 32
	// hookBridgeStartTimeout bounds the time to find a free port and start
	// the listener.
	hookBridgeStartTimeout = 5 * time.Second
	// hookDeferDecision is the only response C1D ever returns.
	hookDeferDecision = `{"decision":"defer"}`
)

// claudePreToolUse is the strict, bounded subset of PreToolUse fields accepted
// by the bridge. Only these fields are decoded; everything else is rejected.
type claudePreToolUse struct {
	ToolUseID string `json:"tool_use_id"`
	ToolName  string `json:"tool_name"`
	ToolInput any    `json:"tool_input"`
	SessionID string `json:"session_id"`
}

// claudeHookBridge is a private HTTP server that receives PreToolUse hook
// callbacks from a managed Claude child. Each runtime owns one bridge.
type claudeHookBridge struct {
	token   string
	port    int
	server  *http.Server
	rt      *claudeManagedRuntime // set after runtime creation, before first use
	started chan struct{}         // closed when the listener is accepting

	mu       sync.Mutex
	shutdown bool
}

// newClaudeHookBridge creates a bridge with a random capability token and
// starts listening on a random localhost port. The bridge is ready when
// the returned started channel is closed.
func newClaudeHookBridge() (*claudeHookBridge, string, error) {
	tokenBytes := make([]byte, capabilityTokenLen)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, "", fmt.Errorf("hook bridge token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", fmt.Errorf("hook bridge listen: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	bridge := &claudeHookBridge{
		token:   token,
		port:    port,
		started: make(chan struct{}),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/hook", bridge.handleHook)
	bridge.server = &http.Server{
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		IdleTimeout:  30 * time.Second,
	}

	go func() {
		close(bridge.started)
		_ = bridge.server.Serve(listener)
	}()

	return bridge, token, nil
}

// endpoint returns the hook URL for use in the Claude hook settings.
func (b *claudeHookBridge) endpoint() string {
	return fmt.Sprintf("http://127.0.0.1:%d/hook?token=%s", b.port, b.token)
}

// close shuts down the bridge HTTP server. Safe to call more than once.
func (b *claudeHookBridge) close() {
	b.mu.Lock()
	if b.shutdown {
		b.mu.Unlock()
		return
	}
	b.shutdown = true
	b.mu.Unlock()
	_ = b.server.Close()
}

// handleHook is the single bridge endpoint. It validates the capability token,
// strictly decodes the PreToolUse fields, stores the observation, and returns
// defer. Every failure path returns defer — the bridge never causes Claude to
// fail a tool use; it simply does not create an observation.
func (b *claudeHookBridge) handleHook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeDefer(w)
		return
	}

	// Validate capability token.
	if r.URL.Query().Get("token") != b.token {
		writeDefer(w)
		return
	}

	// Bound the body size before any decode.
	body, err := io.ReadAll(io.LimitReader(r.Body, maxPreToolUseBody+1))
	if err != nil || len(body) > maxPreToolUseBody {
		writeDefer(w)
		return
	}

	// Strict decode: unknown fields rejected.
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	var event claudePreToolUse
	if err := dec.Decode(&event); err != nil {
		writeDefer(w)
		return
	}
	// No trailing content.
	if dec.More() {
		writeDefer(w)
		return
	}

	// Field bounds.
	if event.ToolUseID == "" || len(event.ToolUseID) > maxToolUseIDLen {
		writeDefer(w)
		return
	}
	if event.ToolName == "" || len(event.ToolName) > maxToolNameLen {
		writeDefer(w)
		return
	}
	if event.SessionID == "" || len(event.SessionID) > maxClaudeSessionIDLen {
		writeDefer(w)
		return
	}
	if event.ToolInput == nil {
		writeDefer(w)
		return
	}

	// Canonical input digest: re-marshal the tool_input for a stable hash.
	inputBytes, err := json.Marshal(event.ToolInput)
	if err != nil || len(inputBytes) > maxToolInputBytes {
		writeDefer(w)
		return
	}
	inputDigest := sha256.Sum256(inputBytes)
	inputDigestHex := hex.EncodeToString(inputDigest[:])

	// Compute canonical tool_input digest based on the raw JSON bytes.
	// Only the digest is stored; the raw input never enters any DTO or log.

	rt := b.rt
	if rt == nil {
		writeDefer(w)
		return
	}

	rt.observePreToolUse(event.ToolUseID, event.ToolName, event.SessionID, inputDigestHex)
	writeDefer(w)
}

// writeDefer writes the defer decision JSON. This is the only response C1D
// ever returns — the bridge never allows or denies a tool.
func writeDefer(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(hookDeferDecision))
}
