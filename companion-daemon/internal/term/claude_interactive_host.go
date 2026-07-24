package term

import (
	"bufio"
	"context"
	"crypto/rand"
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
// Claude runs in a PTY (Terminal primary), hooks provide operational evidence
// (observer_only), and private JSONL provides partial/degraded Transcript.
//
// DS-CL2: Provider-Category C. Terminal is the work surface; structured
// evidence is partial. Approval is observer_only.

// ClaudeInteractiveConfig is the pinned Claude configuration.
type ClaudeInteractiveConfig struct {
	Bin              string
	Version          string
	AuthorityVersion string
}

// ClaudeInteractiveHost creates interactive Claude sessions.
type ClaudeInteractiveHost struct {
	cfg           ClaudeInteractiveConfig
	ownedPTY      *OwnedPTYRuntime
	transcriptSvc *transcript.Service
	authorizer    devicetrust.MutationAuthorizer

	mu       sync.Mutex
	runtimes map[string]*claudeInteractiveRuntime
}

type claudeInteractiveRuntime struct {
	sessionID       string
	claudeSessionID string // the --session-id UUID
	bridge          *claudeInteractiveBridge
	normalizer      *claudeJSONLNormalizer
	hookDir         string
}

// NewClaudeInteractiveHost creates the host.
func NewClaudeInteractiveHost(
	cfg ClaudeInteractiveConfig,
	ownedPTY *OwnedPTYRuntime,
	transcriptSvc *transcript.Service,
	authorizer devicetrust.MutationAuthorizer,
) (*ClaudeInteractiveHost, error) {
	if ownedPTY == nil {
		return nil, fmt.Errorf("claude interactive: OwnedPTYRuntime required")
	}
	return &ClaudeInteractiveHost{
		cfg:           cfg,
		ownedPTY:      ownedPTY,
		transcriptSvc: transcriptSvc,
		authorizer:    authorizer,
		runtimes:      make(map[string]*claudeInteractiveRuntime),
	}, nil
}

// Create launches interactive Claude with PTY + hooks + JSONL.
func (h *ClaudeInteractiveHost) Create(ctx context.Context, cwd string, deviceID string, deviceEpoch uint64) (string, error) {
	if cwd == "" {
		cwd = "/"
	}
	if err := validateCWD(cwd); err != nil {
		return "", err
	}

	// Generate POKIT session UUID.
	claudeUUID, err := newClaudeSessionUUID()
	if err != nil {
		return "", fmt.Errorf("claude session uuid: %w", err)
	}

	// Create hook directory.
	hookDir, err := os.MkdirTemp("", "pokit-claude-interactive-*")
	if err != nil {
		return "", err
	}

	// Start observer-only hook bridge.
	bridge, err := startClaudeInteractiveBridge()
	if err != nil {
		os.RemoveAll(hookDir)
		return "", fmt.Errorf("claude hook bridge: %w", err)
	}

	// Write hook settings pointing to our bridge.
	if err := writeInteractiveHookSettings(hookDir, bridge.token, bridge.port); err != nil {
		bridge.stop()
		os.RemoveAll(hookDir)
		return "", fmt.Errorf("claude hook settings: %w", err)
	}

	// Build SpawnConfig for OwnedPTYRuntime.
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
			"--permission-mode", "acceptEdits",
			"--no-session-persistence", // single generation
		},
	}

	// Spawn via OwnedPTYRuntime.
	canonicalID, err := h.ownedPTY.Create(ctx, cfg, "claude", "Claude", deviceID, deviceEpoch)
	if err != nil {
		bridge.stop()
		os.RemoveAll(hookDir)
		return "", fmt.Errorf("claude spawn: %w", err)
	}

	// Start JSONL normalizer.
	normalizer := startClaudeJSONLNormalizer(canonicalID, claudeUUID, h.transcriptSvc)

	// Register.
	h.mu.Lock()
	h.runtimes[canonicalID] = &claudeInteractiveRuntime{
		sessionID:       canonicalID,
		claudeSessionID: claudeUUID,
		bridge:          bridge,
		normalizer:      normalizer,
		hookDir:         hookDir,
	}
	h.mu.Unlock()

	log.Printf("CLAUDE-INTERACTIVE session=%s uuid=%s hooks=:%d", canonicalID, claudeUUID, bridge.port)
	return canonicalID, nil
}

// ── Interactive Hook Bridge (observer_only) ──

type claudeInteractiveBridge struct {
	token  string
	port   int
	server *http.Server
}

func startClaudeInteractiveBridge() (*claudeInteractiveBridge, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, err
	}
	token := fmt.Sprintf("%x", tokenBytes)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	port := listener.Addr().(*net.TCPAddr).Port

	mux := http.NewServeMux()
	bridge := &claudeInteractiveBridge{token: token, port: port}

	mux.HandleFunc("/hook", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", 405)
			return
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer "+token {
			http.Error(w, "unauthorized", 401)
			return
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 65536))
		// Log hook observation for evidence.
		var probe struct {
			HookEventName string `json:"hook_event_name"`
			ToolName      string `json:"tool_name"`
			SessionID     string `json:"session_id"`
		}
		if json.Unmarshal(body, &probe) == nil {
			log.Printf("CLAUDE-HOOK event=%s tool=%s session=%s", probe.HookEventName, probe.ToolName, probe.SessionID)
		}
		// Always defer — observer_only, never answers.
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"defer"}}`))
	})

	bridge.server = &http.Server{Handler: mux}
	go func() { _ = bridge.server.Serve(listener) }()

	return bridge, nil
}

func (b *claudeInteractiveBridge) stop() {
	if b.server != nil {
		b.server.Close()
	}
}

// ── Hook Settings ──

func writeInteractiveHookSettings(hookDir, token string, port int) error {
	url := fmt.Sprintf("http://127.0.0.1:%d/hook", port)
	settings := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []map[string]any{{
				"matcher": "*",
				"hooks": []map[string]any{{
					"type":    "command",
					"command": fmt.Sprintf(`curl -s -X POST %s -H 'Authorization: Bearer %s' -H 'Content-Type: application/json' -d @-`, url, token),
				}},
			}},
			"PostToolUse": []map[string]any{{
				"matcher": "*",
				"hooks": []map[string]any{{
					"type":    "command",
					"command": fmt.Sprintf(`curl -s -X POST %s -H 'Authorization: Bearer %s' -H 'Content-Type: application/json' -d @-`, url, token),
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

	// Set correlation so agent events become primary transcript source.
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
	baseDir := filepath.Join(home, ".claude", "projects")

	// Find the JSONL file by scanning for the session UUID.
	jsonlPath := findClaudeJSONL(baseDir, n.claudeSessionID)
	if jsonlPath == "" {
		log.Printf("CLAUDE-JSONL session=%s uuid=%s: file not found under %s", n.sessionID, n.claudeSessionID, baseDir)
		return
	}
	log.Printf("CLAUDE-JSONL session=%s: tailing %s", n.sessionID, jsonlPath)

	f, err := os.Open(jsonlPath)
	if err != nil {
		log.Printf("CLAUDE-JSONL session=%s: open error: %v", n.sessionID, err)
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
	if len(line) == 0 {
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
			return // one segment per assistant message
		}
	}
}

func (n *claudeJSONLNormalizer) Stop() {
	if n.cancel != nil {
		n.cancel()
	}
	<-n.done
}

// findClaudeJSONL searches for a Claude session JSONL file by UUID.
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
