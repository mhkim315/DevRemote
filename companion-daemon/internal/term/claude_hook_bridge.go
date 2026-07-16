// Package term — C1D: daemon-owned private local hook bridge for Claude
// PreToolUse observation. Each managed Claude runtime gets an unguessable
// per-runtime capability token and a private HTTP endpoint on localhost.
//
// Hook response format matches the C0D-certified shape:
//
//	{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"defer"}}
//
// The bridge strictly decodes the PreToolUse fields needed for identity binding
// (session_id, tool_use_id, tool_name, tool_input) while accepting but not
// rejecting additional fields present in the real provider payload (cwd,
// transcript_path, prompt_id, permission_mode, effort, hook_event_name).
// Unknown fields are ignored — only the binding fields are extracted.
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
	maxPreToolUseBody     = 64 << 10
	maxToolInputBytes     = 32 << 10
	maxToolNameLen        = 128
	maxClaudeSessionIDLen = 128
	maxToolUseIDLen       = 128
	capabilityTokenLen    = 32
)

// hookDeferResponse is the C0D-certified defer response. It is the only
// response C1D ever returns — the bridge never allows or denies.
const hookDeferResponse = `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"defer"}}`

// claudePreToolUse is the minimal set of PreToolUse fields needed for identity
// binding. The real provider payload may include additional fields (cwd,
// transcript_path, prompt_id, permission_mode, effort, hook_event_name) which
// are accepted but not decoded — only the binding fields are extracted.
type claudePreToolUse struct {
	SessionID string          `json:"session_id"`
	ToolUseID string          `json:"tool_use_id"`
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`
}

type claudeHookBridge struct {
	token   string
	port    int
	server  *http.Server
	rt      *claudeManagedRuntime
	started chan struct{}

	mu       sync.Mutex
	shutdown bool
}

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

func (b *claudeHookBridge) endpoint() string {
	return fmt.Sprintf("http://127.0.0.1:%d/hook?token=%s", b.port, b.token)
}

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
// decodes the PreToolUse fields needed for identity binding, stores the
// observation, and returns the C0D-certified defer response. Every failure
// path still returns defer — the bridge never causes Claude to fail a tool
// use; it simply does not create an observation.
func (b *claudeHookBridge) handleHook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeHookDefer(w)
		return
	}
	if r.URL.Query().Get("token") != b.token {
		writeHookDefer(w)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxPreToolUseBody+1))
	if err != nil || len(body) > maxPreToolUseBody {
		writeHookDefer(w)
		return
	}

	// Decode WITHOUT DisallowUnknownFields — the real provider payload carries
	// additional fields (cwd, transcript_path, prompt_id, permission_mode,
	// effort, hook_event_name) that we accept but do not decode.
	dec := json.NewDecoder(strings.NewReader(string(body)))
	var event claudePreToolUse
	if err := dec.Decode(&event); err != nil {
		writeHookDefer(w)
		return
	}
	if dec.More() {
		writeHookDefer(w)
		return
	}

	// Field bounds.
	if event.ToolUseID == "" || len(event.ToolUseID) > maxToolUseIDLen {
		writeHookDefer(w)
		return
	}
	if event.ToolName == "" || len(event.ToolName) > maxToolNameLen {
		writeHookDefer(w)
		return
	}
	if event.SessionID == "" || len(event.SessionID) > maxClaudeSessionIDLen {
		writeHookDefer(w)
		return
	}
	if len(event.ToolInput) == 0 {
		writeHookDefer(w)
		return
	}

	// Canonical input digest: re-marshal tool_input with sorted keys for a
	// stable hash matching the C0D evidence format.
	inputCanonical, err := canonicalJSON(event.ToolInput)
	if err != nil || len(inputCanonical) > maxToolInputBytes {
		writeHookDefer(w)
		return
	}
	inputDigest := sha256.Sum256(inputCanonical)
	inputDigestHex := hex.EncodeToString(inputDigest[:])

	rt := b.rt
	if rt == nil {
		writeHookDefer(w)
		return
	}

	rt.observePreToolUse(event.ToolUseID, event.ToolName, event.SessionID, inputDigestHex)
	writeHookDefer(w)
}

// canonicalJSON re-marshals raw JSON with sorted keys for a stable digest.
func canonicalJSON(raw json.RawMessage) ([]byte, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return json.Marshal(v) // encoding/json sorts map keys by default
}

func writeHookDefer(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(hookDeferResponse))
}
