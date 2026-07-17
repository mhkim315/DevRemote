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
	maxPreToolUseBody     = 64 << 10
	maxToolInputBytes     = 32 << 10
	maxToolNameLen        = 128
	maxClaudeSessionIDLen = 128
	maxToolUseIDLen       = 128
	maxCWDLength          = 1024
	capabilityTokenLen    = 32
)

// hookDeferResponse is the C0D-certified defer response.
const hookDeferResponse = `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"defer"}}`

// preToolUseAllowlist is the CLOSED set of fields accepted by the bridge.
// Every field the real C0D provider emits is listed; anything else is
// rejected. Fields not needed for identity binding are accepted but not
// decoded beyond verifying they are valid JSON.
var preToolUseAllowlist = map[string]bool{
	"session_id":      true,
	"tool_use_id":     true,
	"tool_name":       true,
	"tool_input":      true,
	"hook_event_name": true,
	"cwd":             true,
	"transcript_path": true,
	"prompt_id":       true,
	"permission_mode": true,
	"effort":          true,
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

// resumeContext is the immutable state installed in the bridge BEFORE the
// resume process spawns. It carries the original approval target identity
// and claim ownership. The resume process has its own epoch, but witness
// validation uses the original RuntimeRef from this context.
type resumeContext struct {
	coordinator      *claudeResumeCoordinator
	claimToken       string
	resumeNonce      string
	originalRuntime  RuntimeRef // the approval target, not the resume attempt
	pokitSessionID   string
	claudeSessionID  string
	toolUseID        string
	toolName         string
	inputDigest      string
	expectedDecision string // "allow" or "deny"
	originalCWD      string // bound from original managed session
}

type claudeHookBridge struct {
	token   string
	port    int
	server  *http.Server
	rt      *claudeManagedRuntime
	started chan struct{}

	// resumeCtx is set before the resume process spawns. If nil, the
	// bridge is in C1D observation mode (/hook only, always returns defer).
	resumeCtx *resumeContext

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
	mux.HandleFunc("/resume", bridge.handleResume)
	mux.HandleFunc("/posttool", bridge.handlePostTool)
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

func (b *claudeHookBridge) resumeEndpoint(claimToken, nonce string) string {
	return fmt.Sprintf("http://127.0.0.1:%d/resume?token=%s&claim=%s&nonce=%s", b.port, b.token, claimToken, nonce)
}

func (b *claudeHookBridge) posttoolEndpoint(claimToken string) string {
	return fmt.Sprintf("http://127.0.0.1:%d/posttool?token=%s&claim=%s", b.port, b.token, claimToken)
}

// installResumeContext sets the immutable resume context. Must be called
// before the resume process is spawned. Nil context clears resume mode.
func (b *claudeHookBridge) installResumeContext(ctx *resumeContext) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.resumeCtx = ctx
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

// getRT returns the current managed runtime pointer under the bridge mutex.
func (b *claudeHookBridge) getRT() *claudeManagedRuntime {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.rt
}

// publishRuntime makes a FULLY-initialized runtime visible to the hook
// handlers. Must be called after every identity/authority field of the
// runtime is assigned; hooks that arrive before publication observe nil and
// answer the safe defer.
func (b *claudeHookBridge) publishRuntime(rt *claudeManagedRuntime) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rt = rt
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

	// P2A: classify the tool_input against the frozen catalog.
	// Classification occurs exactly once at the provider boundary while
	// the raw bytes are still available; after this the raw input is
	// discarded and only the digest survives. getRT returns under the
	// bridge mutex — the handler is safe against a runtime that was
	// not yet published (nil) and against the one-time publication.
	catalogActionID := ""
	if rt := b.getRT(); rt != nil {
		cid, classifierDigest, matched := classifyCatalogAction(fields["tool_input"], claudeHeadlessAdapter, rt.authorityVersion, toolName)
		catalogActionID = selectCatalogActionID(cid, classifierDigest, inputDigest, matched)
	}

	rt := b.getRT()
	if rt == nil {
		writeHookDefer(w)
		return
	}

	rt.observePreToolUse(toolUseID, toolName, sessionID, inputDigest, catalogActionID)
	writeHookDefer(w)
}

// handleResume is the C2D-C resume hook handler. It receives the repeated
// PreToolUse when Claude resumes a deferred session. It validates the
// nonce, calls ClaimWrite, encodes the C0D-certified response from
// WriteHandle.Decision(), writes the HTTP response, and calls ConfirmWrite.
func (b *claudeHookBridge) handleResume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeHookDefer(w)
		return
	}
	if r.URL.Query().Get("token") != b.token {
		writeHookDefer(w)
		return
	}
	claimToken := r.URL.Query().Get("claim")
	resumeNonce := r.URL.Query().Get("nonce")
	if claimToken == "" || resumeNonce == "" {
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
	if heName, sok := strictBoundedString(fields["hook_event_name"], 64); !sok || heName != "PreToolUse" {
		writeHookDefer(w)
		return
	}

	// Strict decode of identity fields — all must parse, but for the
	// ClaimWrite call we use the ORIGINAL identity values from the resume
	// context, not the body's. Real Claude resume generates a new
	// tool_use_id (+ potentially new session ID) for the retried tool
	// call; ClaimWrite validates against the entry reserved from the
	// original identity. We still validate that the body fields parse so
	// a malformed hook body fails closed.
	toolUseIDBody, ok1 := strictBoundedString(fields["tool_use_id"], maxToolUseIDLen)
	toolNameBody, ok2 := strictBoundedString(fields["tool_name"], maxToolNameLen)
	sessionIDBody, ok3 := strictBoundedString(fields["session_id"], maxClaudeSessionIDLen)
	if !ok1 || !ok2 || !ok3 {
		writeHookDefer(w)
		return
	}
	if len(fields["tool_input"]) == 0 {
		writeHookDefer(w)
		return
	}
	// Canonical recomputation is also a validation step — it can fail.
	inputCanon, err := canonicalJSON(fields["tool_input"])
	if err != nil || len(inputCanon) > maxToolInputBytes {
		writeHookDefer(w)
		return
	}
	_ = sha256Hex(inputCanon) // digest recomputation validates, value unused — ctx supplies it
	_, _, _ = toolUseIDBody, toolNameBody, sessionIDBody

	// B2 + R4-1: use the immutable resume context under the bridge mutex.
	b.mu.Lock()
	ctx := b.resumeCtx
	b.mu.Unlock()
	if ctx == nil || ctx.coordinator == nil {
		writeHookDefer(w)
		return
	}
	// Verify claim/nonce against the context.
	if ctx.claimToken != claimToken || ctx.resumeNonce != resumeNonce {
		writeHookDefer(w)
		return
	}

	// Use the original identity fields from the resume context, not the
	// hook body's. Real Claude resume generates a NEW tool_use_id and
	// potentially a new session ID for the retried tool call; ClaimWrite
	// validates against the entry reserved from the original identity.
	// The tool_name (from the classifier) and canonical input digest
	// (recomputed deterministically) are the real action identity and
	// should match between the context and the hook body.
	origToolUseID, origToolName, origInputDigest := ctx.toolUseID, ctx.toolName, ctx.inputDigest
	wh, outcome := ctx.coordinator.ClaimWrite(claimToken, resumeNonce, ctx.claudeSessionID, origToolUseID, origToolName, origInputDigest)
	if outcome != outcomeWritten {
		writeHookDefer(w)
		return
	}

	// Encode the exact C0D-certified hook response and write it.
	decision := wh.Decision()
	resp := claudeHookResponseBytes(decision)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	n, writeErr := w.Write(resp)
	if writeErr != nil || n != len(resp) {
		ctx.coordinator.ConfirmWrite(claimToken, false)
		return
	}
	ctx.coordinator.ConfirmWrite(claimToken, true)
}

// handlePostTool is the C2D-C PostToolUse witness handler. It uses the
// ORIGINAL RuntimeRef from the resume context (not the resume epoch).
func (b *claudeHookBridge) handlePostTool(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.URL.Query().Get("token") != b.token {
		w.WriteHeader(http.StatusOK)
		return
	}
	claimToken := r.URL.Query().Get("claim")
	if claimToken == "" {
		w.WriteHeader(http.StatusOK)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxPreToolUseBody+1))
	if err != nil || len(body) > maxPreToolUseBody {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Strict decode PostToolUse fields.
	fields, ok := strictPreToolUseDecode(body)
	if !ok {
		w.WriteHeader(http.StatusOK)
		return
	}
	heName, sok := strictBoundedString(fields["hook_event_name"], 64)
	if !sok || heName != "PostToolUse" {
		w.WriteHeader(http.StatusOK)
		return
	}

	toolUseID, ok1 := strictBoundedString(fields["tool_use_id"], maxToolUseIDLen)
	toolName, ok2 := strictBoundedString(fields["tool_name"], maxToolNameLen)
	sessionID, ok3 := strictBoundedString(fields["session_id"], maxClaudeSessionIDLen)
	if !ok1 || !ok2 || !ok3 {
		w.WriteHeader(http.StatusOK)
		return
	}
	inputDigest := ""
	if raw, ok := fields["tool_input"]; ok && len(raw) > 0 {
		if canon, err := canonicalJSON(raw); err == nil && len(canon) <= maxToolInputBytes {
			inputDigest = sha256Hex(canon)
		}
	}

	b.mu.Lock()
	ctx := b.resumeCtx
	b.mu.Unlock()
	if ctx == nil {
		w.WriteHeader(http.StatusOK)
		return
	}
	// R6-A3: PostToolUse ALWAYS proves allow only. Deny must come from
	// the permission_denials stream decoder, never from this hook.
	ctx.coordinator.MarkWitnessed(claimToken, WitnessPostToolUse, sessionID, toolUseID, toolName, inputDigest, ctx.originalRuntime)
	w.WriteHeader(http.StatusOK)
}

func writeHookDefer(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(hookDeferResponse))
}
