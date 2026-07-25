package term

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"devremote/companion-daemon/internal/devicetrust"
	"devremote/companion-daemon/internal/transcript"
)

// CodexTUIHost manages POKIT-owned interactive Codex sessions.
// Codex runs in a PTY (Terminal primary) with a JSONL tailer providing
// structured Transcript evidence from the provider's session file.
//
// DS-CX2: one app-server and thread authority via stock Codex binary.
// The interactive `codex` command is the combined app-server + TUI.

// CodexTUIHost creates interactive Codex sessions.
type CodexTUIHost struct {
	bin           string
	ownedPTY      *OwnedPTYRuntime
	transcriptSvc *transcript.Service
	authorizer    devicetrust.MutationAuthorizer

	mu       sync.Mutex
	runtimes map[string]*codexTUIRuntime
}

type codexTUIRuntime struct {
	sessionID string
	tailer    *codexJSONLTailer
}

// NewCodexTUIHost creates the host.
func NewCodexTUIHost(
	bin string,
	ownedPTY *OwnedPTYRuntime,
	transcriptSvc *transcript.Service,
	authorizer devicetrust.MutationAuthorizer,
) (*CodexTUIHost, error) {
	if ownedPTY == nil {
		return nil, fmt.Errorf("codex tui: OwnedPTYRuntime required")
	}
	if bin == "" {
		bin = "codex"
	}
	return &CodexTUIHost{
		bin:           bin,
		ownedPTY:      ownedPTY,
		transcriptSvc: transcriptSvc,
		authorizer:    authorizer,
		runtimes:      make(map[string]*codexTUIRuntime),
	}, nil
}

// Create launches interactive Codex with PTY.
func (h *CodexTUIHost) Create(ctx context.Context, cwd string, deviceID string, deviceEpoch uint64) (string, error) {
	if cwd == "" {
		cwd = "/"
	}
	if err := validateCWD(cwd); err != nil {
		return "", err
	}

	name := "codex-" + randomSuffix()
	cfg := SpawnConfig{
		Name:       name,
		CWD:        cwd,
		Executable: h.bin,
		// No subcommand = interactive TUI mode. The Codex CLI is
		// the combined app-server + TUI — one process, one thread.
		Args: nil,
	}

	canonicalID, err := h.ownedPTY.Create(ctx, cfg, "codex", "Codex", deviceID, deviceEpoch)
	if err != nil {
		return "", fmt.Errorf("codex tui spawn: %w", err)
	}

	// Start JSONL tailer for Transcript projection.
	tailer := startCodexJSONLTailer(canonicalID, h.transcriptSvc)

	// BF-4 #11: register cleanup hook for the JSONL tailer.
	h.ownedPTY.RegisterCleanupHook(canonicalID, func() {
		tailer.Stop()
	})

	h.mu.Lock()
	h.runtimes[canonicalID] = &codexTUIRuntime{
		sessionID: canonicalID,
		tailer:    tailer,
	}
	h.mu.Unlock()

	log.Printf("CODEX-TUI session=%s", canonicalID)
	return canonicalID, nil
}

func randomSuffix() string {
	b := make([]byte, 8)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// ── Codex JSONL Tailer ──

type codexJSONLTailer struct {
	sessionID     string
	transcriptSvc *transcript.Service
	cancel        context.CancelFunc
	done          chan struct{}
}

func startCodexJSONLTailer(sessionID string, svc *transcript.Service) *codexJSONLTailer {
	ctx, cancel := context.WithCancel(context.Background())
	t := &codexJSONLTailer{
		sessionID:     sessionID,
		transcriptSvc: svc,
		cancel:        cancel,
		done:          make(chan struct{}),
	}
	if svc == nil {
		cancel()
		close(t.done)
		return t
	}

	// Set correlation.
	svc.SetCorrelation(sessionID, transcript.CorrelationState{
		SessionID:   sessionID,
		Correlation: "managed_launch",
		Provider:    "codex",
	})

	go t.run(ctx)
	return t
}

func (t *codexJSONLTailer) run(ctx context.Context) {
	defer close(t.done)

	home, _ := os.UserHomeDir()
	if home == "" {
		return
	}
	baseDir := filepath.Join(home, "Library", "Application Support", "orca",
		"codex-runtime-home", "home", "sessions")

	// Wait for the session directory to appear.
	jsonlPath := ""
	for i := 0; i < 120; i++ {
		select {
		case <-ctx.Done():
			return
		default:
		}
		jsonlPath = findCodexJSONL(baseDir)
		if jsonlPath != "" {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if jsonlPath == "" {
		log.Printf("CODEX-JSONL session=%s: no session file found under %s", t.sessionID, baseDir)
		return
	}
	log.Printf("CODEX-JSONL session=%s: tailing %s", t.sessionID, jsonlPath)

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
		t.projectLine(scanner.Bytes())
	}
}

func (t *codexJSONLTailer) projectLine(line []byte) {
	if len(line) == 0 || t.transcriptSvc == nil {
		return
	}

	// Codex JSONL uses "type" field: assistant, response_item, event_msg, etc.
	var probe struct {
		Type    string `json:"type"`
		Message struct {
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
		Payload struct {
			Type string `json:"type"`
			Text string `json:"text"`
			Role string `json:"role"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &probe) != nil {
		return
	}

	var text string
	var eventType string

	switch {
	case probe.Type == "assistant" && probe.Message.Role == "assistant":
		eventType = "assistant_message"
		for _, c := range probe.Message.Content {
			if c.Type == "text" && c.Text != "" {
				text = c.Text
				break
			}
		}
	case probe.Type == "response_item" && probe.Payload.Role == "assistant":
		eventType = "assistant_message"
		text = probe.Payload.Text
	default:
		return // not assistant content
	}

	if text == "" {
		return
	}

	text = transcript.BoundedText(text)
	if text == "" {
		return
	}

	t.transcriptSvc.FeedAgentSegments(t.sessionID, []transcript.TranscriptSegment{{
		SessionID:       t.sessionID,
		Kind:            transcript.KindAgentEvent,
		Source:          transcript.SourceAgentEvent,
		Text:            text,
		AgentKind:       "codex",
		EventType:       eventType,
		ObservedAt:      time.Now(),
		ContractVersion: transcript.ContractVersion,
	}})
}

func (t *codexJSONLTailer) Stop() {
	if t.cancel != nil {
		t.cancel()
	}
	<-t.done
}

// findCodexJSONL searches for the most recent Codex session JSONL file.
func findCodexJSONL(baseDir string) string {
	var newest string
	var newestTime time.Time

	filepath.WalkDir(baseDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".jsonl") {
			info, err := d.Info()
			if err != nil {
				return nil
			}
			if info.ModTime().After(newestTime) {
				newest = path
				newestTime = info.ModTime()
			}
		}
		return nil
	})
	return newest
}
