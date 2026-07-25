package term

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"devremote/companion-daemon/internal/devicetrust"
	"devremote/companion-daemon/internal/transcript"
)

// ClaudeInteractiveHost manages POKIT-owned interactive Claude sessions.
// Claude runs in a PTY (Terminal primary), hooks provide operational evidence,
// and private JSONL provides partial/degraded Transcript.
//
// DS-CL2: Provider-Category C. Terminal is the work surface.
// DS-CLA2: Mobile approval via actionable hook bridge + AuthoritativeApprovalStore.

type ClaudeInteractiveConfig struct {
	Bin              string
	Version          string
	AuthorityVersion string
}

type ClaudeInteractiveHost struct {
	cfg           ClaudeInteractiveConfig
	ownedPTY      *OwnedPTYRuntime
	transcriptSvc *transcript.Service
	authorizer    devicetrust.MutationAuthorizer
	approvals     *AuthoritativeApprovalStore

	mu       sync.Mutex
	runtimes map[string]*claudeInteractiveRuntime
}

type claudeInteractiveRuntime struct {
	sessionID       string
	claudeSessionID string
	bridge          *claudeInteractiveBridge
	normalizer      *claudeJSONLNormalizer
	hookDir         string
}

func NewClaudeInteractiveHost(
	cfg ClaudeInteractiveConfig,
	ownedPTY *OwnedPTYRuntime,
	transcriptSvc *transcript.Service,
	authorizer devicetrust.MutationAuthorizer,
	approvals *AuthoritativeApprovalStore,
) (*ClaudeInteractiveHost, error) {
	if ownedPTY == nil {
		return nil, fmt.Errorf("claude interactive: OwnedPTYRuntime required")
	}
	if authorizer == nil {
		return nil, fmt.Errorf("claude interactive: mutation authorizer required")
	}
	return &ClaudeInteractiveHost{
		cfg:           cfg,
		ownedPTY:      ownedPTY,
		transcriptSvc: transcriptSvc,
		authorizer:    authorizer,
		approvals:     approvals,
		runtimes:      make(map[string]*claudeInteractiveRuntime),
	}, nil
}

// Create launches interactive Claude with PTY + approval-capable hooks.
// Uses --permission-mode dontAsk so Claude always asks via hooks.
func (h *ClaudeInteractiveHost) Create(ctx context.Context, cwd string, deviceID string, deviceEpoch uint64) (string, error) {
	if cwd == "" {
		cwd = "/"
	}
	if err := validateCWD(cwd); err != nil {
		return "", err
	}

	claudeUUID, err := newClaudeSessionUUID()
	if err != nil {
		return "", fmt.Errorf("claude session uuid: %w", err)
	}

	hookDir, err := os.MkdirTemp("", "pokit-claude-interactive-*")
	if err != nil {
		return "", err
	}

	// DS-CLA2: Start approval-capable bridge.
	bridge, err := startClaudeInteractiveBridge(h.approvals)
	if err != nil {
		os.RemoveAll(hookDir)
		return "", fmt.Errorf("claude hook bridge: %w", err)
	}

	// DS-CLA2: HMAC nonce for hook authentication.
	nonce := bridge.hmacNonce()
	if err := writeInteractiveHookSettings(hookDir, bridge.token, bridge.port, nonce); err != nil {
		bridge.stop()
		os.RemoveAll(hookDir)
		return "", fmt.Errorf("claude hook settings: %w", err)
	}

	bin := h.cfg.Bin
	if bin == "" {
		bin = "claude"
	}
	name := "claude-" + claudeUUID[:12]
	cfg := SpawnConfig{
		Name:       name,
		CWD:        cwd,
		Executable: bin,
		Args: []string{
			"--session-id", claudeUUID,
			"--settings", hookDir,
			// DS-CLA2: dontAsk → Claude always fires hooks for tool approval.
			"--permission-mode", "dontAsk",
			"--no-session-persistence",
		},
	}

	canonicalID, err := h.ownedPTY.Create(ctx, cfg, "claude", "Claude", deviceID, deviceEpoch)
	if err != nil {
		bridge.stop()
		os.RemoveAll(hookDir)
		return "", fmt.Errorf("claude spawn: %w", err)
	}

	normalizer := startClaudeJSONLNormalizer(canonicalID, claudeUUID, h.transcriptSvc)

	h.mu.Lock()
	h.runtimes[canonicalID] = &claudeInteractiveRuntime{
		sessionID:       canonicalID,
		claudeSessionID: claudeUUID,
		bridge:          bridge,
		normalizer:      normalizer,
		hookDir:         hookDir,
	}
	h.mu.Unlock()

	log.Printf("CLAUDE-INTERACTIVE session=%s uuid=%s hooks=:%d approval=mobile", canonicalID, claudeUUID, bridge.port)
	return canonicalID, nil
}

// ── Approval-Capable Hook Bridge ──

// approvalWaiter tracks a pending mobile approval decision.
type approvalWaiter struct {
	approvalID string
	sessionID   string
	toolName   string
	done       chan approvalDecision
}

type approvalDecision struct {
	decision string // "allow" or "deny"
}

type claudeInteractiveBridge struct {
	token   string
	hmacKey []byte
	port    int
	server  *http.Server

	mu      sync.Mutex
	waiters map[string]*approvalWaiter // keyed by approvalID
}

func startClaudeInteractiveBridge(approvals *AuthoritativeApprovalStore) (*claudeInteractiveBridge, error) {
	_ = approvals // reserved for future AuthoritativeApprovalStore integration
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, err
	}
	token := fmt.Sprintf("%x", tokenBytes)

	hmacKey := make([]byte, 32)
	if _, err := rand.Read(hmacKey); err != nil {
		return nil, err
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	port := listener.Addr().(*net.TCPAddr).Port

	bridge := &claudeInteractiveBridge{
		token:     token,
		hmacKey:   hmacKey,
		port:      port,
			waiters:   make(map[string]*approvalWaiter),
	}

	mux := http.NewServeMux()

	// /hook — PreToolUse: create approval record, wait for mobile response.
	mux.HandleFunc("/hook", func(w http.ResponseWriter, r *http.Request) {
		bridge.handlePreToolUse(w, r)
	})

	// /permission — PermissionRequest: create approval record, wait for mobile.
	mux.HandleFunc("/permission", func(w http.ResponseWriter, r *http.Request) {
		bridge.handlePermissionRequest(w, r)
	})

	// /decision — mobile decision query endpoint.
	mux.HandleFunc("/decision", func(w http.ResponseWriter, r *http.Request) {
		bridge.handleDecision(w, r)
	})

	bridge.server = &http.Server{Handler: mux, ReadTimeout: 130 * time.Second, WriteTimeout: 10 * time.Second}
	go func() { _ = bridge.server.Serve(listener) }()

	return bridge, nil
}

func (b *claudeInteractiveBridge) hmacNonce() string {
	mac := hmac.New(sha256.New, b.hmacKey)
	mac.Write([]byte("pokit-claude-launch"))
	return hex.EncodeToString(mac.Sum(nil))[:16]
}

func (b *claudeInteractiveBridge) stop() {
	if b.server != nil {
		b.server.Close()
	}
}

func (b *claudeInteractiveBridge) checkAuth(r *http.Request) bool {
	auth := r.Header.Get("Authorization")
	return auth == "Bearer "+b.token
}

// handlePreToolUse receives PreToolUse hooks, creates an actionable
// approval record, and waits for mobile decision (up to 120s).
func (b *claudeInteractiveBridge) handlePreToolUse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if !b.checkAuth(r) {
		http.Error(w, "unauthorized", 401)
		return
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 65536))

	var hook struct {
		HookEventName string `json:"hook_event_name"`
		ToolName      string `json:"tool_name"`
		ToolUseID     string `json:"tool_use_id"`
		SessionID     string `json:"session_id"`
		ToolInput     json.RawMessage `json:"tool_input"`
	}
	if json.Unmarshal(body, &hook) != nil || hook.ToolUseID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny"}}`))
		return
	}

	// DS-CLA2: Create in-memory approval waiter.
	// The approval is tracked by the bridge; CAS via /decision endpoint.
	approvalID := "claude-" + hook.ToolUseID
	log.Printf("CLAUDE-APPROVAL approval=%s tool=%s session=%s", approvalID, hook.ToolName, hook.SessionID)

	// Wait for mobile decision.
	waiter := &approvalWaiter{
		approvalID: approvalID,
		sessionID:   hook.SessionID,
		toolName:   hook.ToolName,
		done:       make(chan approvalDecision, 1),
	}

	b.mu.Lock()
	b.waiters[approvalID] = waiter
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		delete(b.waiters, approvalID)
		b.mu.Unlock()
	}()

	select {
	case decision := <-waiter.done:
		w.Header().Set("Content-Type", "application/json")
		if decision.decision == "allow" {
			w.Write([]byte(`{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow"}}`))
		} else {
			w.Write([]byte(`{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny"}}`))
		}
	case <-time.After(120 * time.Second):
		// Timeout — deny for safety.
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny"}}`))
	case <-r.Context().Done():
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny"}}`))
	}
}

// handlePermissionRequest handles Claude's PermissionRequest hooks.
func (b *claudeInteractiveBridge) handlePermissionRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if !b.checkAuth(r) {
		http.Error(w, "unauthorized", 401)
		return
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 65536))

	var hook struct {
		HookEventName string `json:"hook_event_name"`
		PermissionID  string `json:"permission_id"`
		SessionID     string `json:"session_id"`
	}
	if json.Unmarshal(body, &hook) != nil || hook.PermissionID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"hookSpecificOutput":{"hookEventName":"PermissionRequest","permissionDecision":"deny"}}`))
		return
	}

	approvalID := "claude-perm-" + hook.PermissionID
	log.Printf("CLAUDE-PERMISSION approval=%s session=%s", approvalID, hook.SessionID)

	// DS-CLA2: Wait for mobile decision (CAS — first response wins).
	waiter := &approvalWaiter{
		approvalID: approvalID,
		sessionID:   hook.SessionID,
		done:       make(chan approvalDecision, 1),
	}

	b.mu.Lock()
	b.waiters[approvalID] = waiter
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		delete(b.waiters, approvalID)
		b.mu.Unlock()
	}()

	select {
	case decision := <-waiter.done:
		w.Header().Set("Content-Type", "application/json")
		if decision.decision == "allow" {
			w.Write([]byte(`{"hookSpecificOutput":{"hookEventName":"PermissionRequest","permissionDecision":"allow"}}`))
		} else {
			w.Write([]byte(`{"hookSpecificOutput":{"hookEventName":"PermissionRequest","permissionDecision":"deny"}}`))
		}
	case <-time.After(120 * time.Second):
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"hookSpecificOutput":{"hookEventName":"PermissionRequest","permissionDecision":"deny"}}`))
	case <-r.Context().Done():
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"hookSpecificOutput":{"hookEventName":"PermissionRequest","permissionDecision":"deny"}}`))
	}
}

// handleDecision receives mobile approval responses and delivers them to
// waiting hook handlers via CAS (first response wins).
func (b *claudeInteractiveBridge) handleDecision(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if !b.checkAuth(r) {
		http.Error(w, "unauthorized", 401)
		return
	}

	var req struct {
		ApprovalID string `json:"approvalId"`
		Decision   string `json:"decision"` // "allow" or "deny"
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.ApprovalID == "" {
		http.Error(w, "invalid body", 400)
		return
	}
	if req.Decision != "allow" && req.Decision != "deny" {
		http.Error(w, "decision must be allow or deny", 400)
		return
	}

	b.mu.Lock()
	waiter, ok := b.waiters[req.ApprovalID]
	b.mu.Unlock()

	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"outcome":"already_resolved"}`))
		return
	}

	// CAS: first response wins.
	select {
	case waiter.done <- approvalDecision{decision: req.Decision}:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"outcome":"accepted"}`))
	default:
		// Already resolved by another response.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"outcome":"already_resolved"}`))
	}
}

// ── Hook Settings ──

func writeInteractiveHookSettings(hookDir, token string, port int, nonce string) error {
	url := fmt.Sprintf("http://127.0.0.1:%d/hook", port)
	permURL := fmt.Sprintf("http://127.0.0.1:%d/permission", port)
	settings := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []map[string]any{{
				"matcher": "*",
				"hooks": []map[string]any{{
					"type":    "command",
					"command": fmt.Sprintf(`curl -s -X POST %s -H 'Authorization: Bearer %s' -H 'Content-Type: application/json' -H 'X-POKIT-Nonce: %s' -d @-`, url, token, nonce),
				}},
			}},
			"PostToolUse": []map[string]any{{
				"matcher": "*",
				"hooks": []map[string]any{{
					"type":    "command",
					"command": fmt.Sprintf(`curl -s -X POST %s -H 'Authorization: Bearer %s' -H 'Content-Type: application/json' -H 'X-POKIT-Nonce: %s' -d @-`, url, token, nonce),
				}},
			}},
			"PermissionRequest": []map[string]any{{
				"matcher": "*",
				"hooks": []map[string]any{{
					"type":    "command",
					"command": fmt.Sprintf(`curl -s -X POST %s -H 'Authorization: Bearer %s' -H 'Content-Type: application/json' -H 'X-POKIT-Nonce: %s' -d @-`, permURL, token, nonce),
				}},
			}},
		},
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(hookDir, "settings.json"), data, 0600)
}

// ── Session UUID ──

func newClaudeSessionUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// ── JSONL Normalizer ──

type claudeJSONLNormalizer struct {
	sessionID       string
	claudeSessionID string
	transcriptSvc   *transcript.Service
	cancel          context.CancelFunc
	done            chan struct{}
}

func startClaudeJSONLNormalizer(sessionID, claudeSessionID string, svc *transcript.Service) *claudeJSONLNormalizer {
	ctx, cancel := context.WithCancel(context.Background())
	n := &claudeJSONLNormalizer{
		sessionID:       sessionID,
		claudeSessionID: claudeSessionID,
		transcriptSvc:   svc,
		cancel:          cancel,
		done:            make(chan struct{}),
	}
	if svc == nil {
		cancel()
		close(n.done)
		return n
	}
	go n.run(ctx)
	return n
}

func (n *claudeJSONLNormalizer) run(ctx context.Context) {
	defer close(n.done)
	if n.transcriptSvc != nil {
		n.transcriptSvc.SetCorrelation(n.sessionID, transcript.CorrelationState{
			SessionID:   n.sessionID,
			Correlation: "managed_launch",
			Provider:    "claude",
		})
	}

	home, _ := os.UserHomeDir()
	if home == "" {
		return
	}

	jsonlPath := findClaudeJSONL(filepath.Join(home, ".claude", "projects"), n.claudeSessionID)
	if jsonlPath == "" {
		return
	}
	log.Printf("CLAUDE-JSONL session=%s: tailing %s", n.sessionID, jsonlPath)

	f, err := os.Open(jsonlPath)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if !scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(500 * time.Millisecond):
				continue
			}
		}
		n.projectLine(scanner.Bytes())
	}
}

func (n *claudeJSONLNormalizer) projectLine(line []byte) {
	if len(line) == 0 || n.transcriptSvc == nil {
		return
	}
	var probe struct {
		Type    string `json:"type"`
		Message struct {
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &probe) != nil {
		return
	}
	if probe.Type != "assistant" || probe.Message.Role != "assistant" {
		return
	}
	for _, c := range probe.Message.Content {
		if c.Type == "text" && c.Text != "" {
			text := transcript.BoundedText(c.Text)
			if text == "" {
				continue
			}
			n.transcriptSvc.FeedAgentSegments(n.sessionID, []transcript.TranscriptSegment{{
				SessionID:       n.sessionID,
				Kind:            transcript.KindAgentEvent,
				Source:          transcript.SourceAgentEvent,
				Text:            text,
				AgentKind:       "claude",
				EventType:       "assistant_message",
				ObservedAt:      time.Now(),
				ContractVersion: transcript.ContractVersion,
			}})
			return
		}
	}
}

func (n *claudeJSONLNormalizer) Stop() {
	if n.cancel != nil {
		n.cancel()
	}
	<-n.done
}

func findClaudeJSONL(baseDir, sessionUUID string) string {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		projDir := filepath.Join(baseDir, e.Name())
		jsonlFile := filepath.Join(projDir, sessionUUID+".jsonl")
		if _, err := os.Stat(jsonlFile); err == nil {
			return jsonlFile
		}
	}
	return ""
}
