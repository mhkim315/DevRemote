// Package term — C1D: daemon-owned private local hook bridge for Claude
// PreToolUse observation. Each managed Claude runtime gets an unguessable
// per-runtime capability token and a private HTTP endpoint on localhost.
//
// The bridge uses a CLOSED allowlist of known PreToolUse fields. Unknown
// fields, duplicate keys, and trailing content are rejected BEFORE any
// observation is stored. The hook_event_name must equal "PreToolUse".
//
// Response format (C0D-certified):
//
//	{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"defer"}}
package term

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	maxPreToolUseBody  = 64 << 10
	maxToolInputBytes  = 32 << 10
	maxToolNameLen     = 128
	maxClaudeSessionIDLen    = 128
	maxToolUseIDLen    = 128
	maxCWDLength       = 1024
	capabilityTokenLen = 32
)

// hookDeferResponse is the C0D-certified defer response.
const hookDeferResponse = `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"defer"}}`

// preToolUseAllowlist is the CLOSED set of fields accepted by the bridge.
// Every field the real C0D provider emits is listed; anything else is
// rejected. Fields not needed for identity binding are accepted but not
// decoded beyond verifying they are valid JSON.
var preToolUseAllowlist = map[string]bool{
	"session_id":       true,
	"tool_use_id":      true,
	"tool_name":        true,
	"tool_input":       true,
	"hook_event_name":  true,
	"cwd":              true,
	"transcript_path":  true,
	"prompt_id":        true,
	"permission_mode":  true,
	"effort":           true,
}

// strictPreToolUseDecode decodes raw JSON into a map, rejecting:
//   - non-object input
//   - unknown fields (not in preToolUseAllowlist)
//   - duplicate keys
//   - trailing content after the object
//
// Returns the decoded map keyed by field name, or false on any rejection.
func strictPreToolUseDecode(raw []byte) (map[string]json.RawMessage, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, false
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, false
	}
	out := make(map[string]json.RawMessage, len(preToolUseAllowlist))
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, false
		}
		key, ok := kt.(string)
		if !ok {
			return nil, false
		}
		if !preToolUseAllowlist[key] {
			return nil, false // unknown field
		}
		if _, dup := out[key]; dup {
			return nil, false // duplicate key
		}
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return nil, false
		}
		out[key] = val
	}
	if _, err := dec.Token(); err != nil { // consume '}'
		return nil, false
	}
	if _, err := dec.Token(); err != io.EOF { // trailing content
		return nil, false
	}
	return out, true
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

// handleHook validates the capability token, strictly decodes the PreToolUse
// payload, validates hook_event_name == "PreToolUse", extracts identity fields,
// stores the observation, and returns the C0D-certified defer response.
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

	fields, ok := strictPreToolUseDecode(body)
	if !ok {
		writeHookDefer(w)
		return
	}

	// Validate hook_event_name == "PreToolUse".
	if heName, sok := strictBoundedString(fields["hook_event_name"], 64); !sok || heName != "PreToolUse" {
		writeHookDefer(w)
		return
	}

	// Extract and validate identity fields.
	toolUseID, ok1 := strictBoundedString(fields["tool_use_id"], maxToolUseIDLen)
	toolName, ok2 := strictBoundedString(fields["tool_name"], maxToolNameLen)
	sessionID, ok3 := strictBoundedString(fields["session_id"], maxClaudeSessionIDLen)
	if !ok1 || !ok2 || !ok3 {
		writeHookDefer(w)
		return
	}
	if len(fields["tool_input"]) == 0 {
		writeHookDefer(w)
		return
	}

	// Canonical input digest.
	inputCanon, err := canonicalJSON(fields["tool_input"])
	if err != nil || len(inputCanon) > maxToolInputBytes {
		writeHookDefer(w)
		return
	}
	inputDigest := sha256Hex(inputCanon)

	rt := b.rt
	if rt == nil {
		writeHookDefer(w)
		return
	}

	rt.observePreToolUse(toolUseID, toolName, sessionID, inputDigest)
	writeHookDefer(w)
}

func writeHookDefer(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(hookDeferResponse))
}
