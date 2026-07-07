package agent

import "testing"

func TestClaudeParser_Contract(t *testing.T) {
	RunParserContract(t, "claude", func(t *testing.T) AgentParser {
		return NewClaudeParser()
	})
}

func TestClaudeDetector_Contract(t *testing.T) {
	RunDetectorContract(t, "claude",
		func(t *testing.T) AgentDetector { return NewClaudeDetector() },
		func(t *testing.T) LogResolver { return NewClaudeLogResolver() },
	)
}

// TestClaudeAdapter_E2E_Pipeline proves the full vertical slice:
// detect → parse → status inference → approval detection.
func TestClaudeAdapter_E2E_Pipeline(t *testing.T) {
	lines := loadAllFixtures(t, "claude")
	if len(lines) == 0 {
		t.Fatal("no Claude fixtures")
	}
	meta := loadMeta(t, "claude")

	// 1. Detect: simulated Claude process evidence.
	detector := NewClaudeDetector()
	id := detector.Detect(DetectionEvidence{
		ProcessName: "claude",
		CWD:         "/Users/test/project",
	})
	if id.Kind != "claude" {
		t.Fatalf("detect: Kind=%q, want claude", id.Kind)
	}
	if id.Confidence < 0.5 {
		t.Fatalf("detect: confidence %.2f < 0.5", id.Confidence)
	}

	// 2. Resolve logs.
	resolver := NewClaudeLogResolver()
	result, err := resolver.Resolve(DetectionEvidence{
		ProcessName: "claude",
		CWD:         "/Users/test/project",
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(result.Logs) == 0 {
		t.Error("resolve: no log paths found")
	}
	for _, lr := range result.Logs {
		if lr.DisplayPath == "" {
			t.Error("resolve: DisplayPath is empty")
		}
	}

	// 3. Parse: feed A1 fixtures through ClaudeParser.
	parser := NewClaudeParser()
	pr := parser.ParseBatch(lines, "")
	if pr.Degraded {
		t.Errorf("parse degraded: %v", pr.Diagnostics)
	}
	if len(pr.Events) == 0 {
		t.Fatal("parse: 0 events from fixtures")
	}

	// 4. Verify expected events from metadata.
	gotTypes := make(map[AgentEventType]bool)
	for _, e := range pr.Events {
		gotTypes[e.Type] = true
	}
	for _, want := range meta.ExpectedEvents {
		if !gotTypes[AgentEventType(want)] {
			t.Errorf("event %q not found (got %v)", want, eventTypes(gotTypes))
		}
	}

	// 5. Status inference must be non-empty and valid.
	if pr.Status == "" {
		t.Error("status is empty")
	}
	if pr.Status == StatusUnknown && len(pr.Events) > 0 {
		t.Error("status=unknown despite events present")
	}

	// 6. Approval detection: verify from approval_waiting fixture.
	approvalLines := loadFixtures(t, "claude", "approval_waiting.jsonl")
	if len(approvalLines) > 0 {
		apr := parser.ParseBatch(approvalLines, "")
		if len(apr.Approvals) == 0 {
			t.Error("approval fixture: no approvals detected")
		}
		for _, a := range apr.Approvals {
			if a.ID == "" || a.Status == "" {
				t.Error("approval has empty ID or Status")
			}
		}
	}

	// 7. Resolver path matches A1 log inventory.
	foundClaudeLog := false
	for _, lr := range result.Logs {
		if contains(lr.Path, ".claude/projects") || contains(lr.Path, ".claude/history") {
			foundClaudeLog = true
		}
		if contains(lr.DisplayPath, "/Users/") {
			t.Error("resolver DisplayPath contains raw home directory")
		}
	}
	if !foundClaudeLog {
		t.Error("resolver did not return any .claude/ log path")
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
