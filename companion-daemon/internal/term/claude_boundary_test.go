package term

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/mux"
)

// Phase A5 product-boundary bridge: Claude adapter output flows
// through production telemetry path (TelemetryService + HandleSessionsV2).

func TestClaudeOutput_TelemetryPath(t *testing.T) {
	t.Run("claude_evidence_true_positive", func(t *testing.T) {
		reg := mux.MustNewRegistry(&claudeSessionAdapter{})
		detector := &claudeEvidenceDetector{}

		svc := NewTelemetryService(reg, NewMemoryEventStore(), NewNopLinkStore(), NoopNotifier{}, detector)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		go svc.Run(ctx)
		time.Sleep(100 * time.Millisecond)
		cancel()
		<-svc.Done()

		h := &Handlers{Registry: reg, Events: NewMemoryEventStore(), Telemetry: svc, AgentDetector: detector}
		req := httptest.NewRequest("GET", "/api/sessions", nil)
		rec := httptest.NewRecorder()
		h.HandleSessionsAPI(rec, req)

		var sessions []SessionTelemetry
		json.Unmarshal(rec.Body.Bytes(), &sessions)
		for _, s := range sessions {
			if s.Adapter == "tmux" {
				t.Logf("Claude evidence: AgentKind=%s Status=%s Confidence=%.2f",
					s.AgentKind, s.AgentStatus, s.AgentConfidence)
				if s.AgentKind != "claude" {
					t.Errorf("Claude evidence: AgentKind=%q, want claude", s.AgentKind)
				}
			}
		}
	})

	t.Run("no_evidence_false_positive_prevention", func(t *testing.T) {
		reg := mux.MustNewRegistry(&claudeSessionAdapter{})
		// Production bridge with no Claude evidence → must return unknown.
		detector := agent.NewTermAgentDetector()

		svc := NewTelemetryService(reg, NewMemoryEventStore(), NewNopLinkStore(), NoopNotifier{}, detector)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		go svc.Run(ctx)
		time.Sleep(100 * time.Millisecond)
		cancel()
		<-svc.Done()

		h := &Handlers{Registry: reg, Events: NewMemoryEventStore(), Telemetry: svc, AgentDetector: detector}
		req := httptest.NewRequest("GET", "/api/sessions", nil)
		rec := httptest.NewRecorder()
		h.HandleSessionsAPI(rec, req)

		var sessions []SessionTelemetry
		json.Unmarshal(rec.Body.Bytes(), &sessions)
		for _, s := range sessions {
			if s.Adapter == "tmux" {
				if s.AgentConfidence >= 0.5 && s.AgentKind != "unknown" {
					t.Errorf("no evidence: high confidence Kind=%s (false positive)", s.AgentKind)
				}
			}
		}
	})

	t.Run("nil_detector_backward_compat", func(t *testing.T) {
		reg := mux.MustNewRegistry(&claudeSessionAdapter{})
		svc := NewTelemetryService(reg, NewMemoryEventStore(), NewNopLinkStore(), NoopNotifier{}, nil)
		h := &Handlers{Registry: reg, Events: NewMemoryEventStore(), Telemetry: svc}
		req := httptest.NewRequest("GET", "/api/sessions", nil)
		rec := httptest.NewRecorder()
		h.HandleSessionsAPI(rec, req)
		var sessions []SessionTelemetry
		json.Unmarshal(rec.Body.Bytes(), &sessions)
		for _, s := range sessions {
			if s.AgentKind != "" {
				t.Errorf("nil detector: AgentKind=%q, want empty", s.AgentKind)
			}
		}
	})
}

// claudeEvidenceDetector uses the Claude detector with real Claude evidence.
type claudeEvidenceDetector struct{}

func (d *claudeEvidenceDetector) DetectAgent(sessionID, adapterName, localID string, evidence ProdDetectionEvidence) (string, string, float64) {
	// Simulate production: when Claude process evidence is present, detect Claude.
	// In real production, process name/CWD come from terminal session metadata.
	det := agent.NewClaudeDetector()
	ev := agent.DetectionEvidence{
		ProcessName: evidence.ProcessName,
		CWD:         evidence.CWD,
		TermAdapter: evidence.TermAdapter,
	}
	// Test fixture: the session adapter below represents a Claude session.
	// In production, process info collection would populate this.
	if evidence.ProcessName == "" && adapterName == "tmux" {
		ev.ProcessName = "claude"
		ev.CWD = "/Users/test/project"
	}
	id := det.Detect(ev)
	status := agent.StatusUnknown
	if id.Confidence >= 0.5 && id.Kind != "unknown" {
		status = agent.StatusWorking
	}
	return id.Kind, string(status), id.Confidence
}

func TestClaudeOutput_TruePositive(t *testing.T) {
	// Claude process evidence must correctly detect Claude.
	det := agent.NewClaudeDetector()
	id := det.Detect(agent.DetectionEvidence{
		ProcessName: "claude",
		CWD:         "/Users/test/project",
		TermAdapter: "tmux",
	})
	if id.Kind != "claude" {
		t.Errorf("Claude evidence: Kind=%q, want claude", id.Kind)
	}
	if id.Confidence < 0.5 {
		t.Errorf("Claude evidence: confidence %.2f < 0.5", id.Confidence)
	}
}

func TestClaudeOutput_FalsePositivePrevention(t *testing.T) {
	// Non-Claude session must NOT be detected as Claude.
	// Evidence-based detection returns unknown for insufficient evidence.
	det := agent.NewClaudeDetector()
	id := det.Detect(agent.DetectionEvidence{
		ProcessName: "bash", // plain shell, not an AI agent
		TermAdapter: "tmux",
	})
	if id.Kind == "claude" && id.Confidence >= 0.5 {
		t.Errorf("false positive: bash process detected as claude (confidence=%.2f)", id.Confidence)
	}
	if id.Kind != "unknown" {
		t.Logf("bash process: Kind=%q Confidence=%.2f", id.Kind, id.Confidence)
	}

	// Empty evidence must return unknown.
	id2 := det.Detect(agent.DetectionEvidence{})
	if id2.Kind != "unknown" {
		t.Errorf("empty evidence: Kind=%q, want unknown", id2.Kind)
	}
	if id2.Confidence >= 0.5 {
		t.Errorf("empty evidence: confidence=%.2f >= 0.5", id2.Confidence)
	}
}

// --- helpers ---

type claudeSessionAdapter struct{}

func (a *claudeSessionAdapter) Name() string { return "tmux" }
func (a *claudeSessionAdapter) ListSessions(_ context.Context) ([]mux.Session, error) {
	return []mux.Session{&claudeStubSession{}}, nil
}

type claudeStubSession struct{}

func (s *claudeStubSession) ID() string          { return "claude-session" }
func (s *claudeStubSession) Title() string       { return "Claude Code" }
func (s *claudeStubSession) AdapterName() string { return "tmux" }
