package agent

import "testing"

func TestAntigravityParser_Contract(t *testing.T) {
	RunParserContract(t, "antigravity", func(t *testing.T) AgentParser {
		return NewAntigravityParser()
	})
}

func TestAntigravityDetector_Contract(t *testing.T) {
	RunDetectorContract(t, "antigravity",
		func(t *testing.T) AgentDetector { return NewAntigravityDetector() },
		func(t *testing.T) LogResolver { return NewAntigravityLogResolver() },
	)
}

// TestAntigravityAdapter_E2E_Pipeline proves the full vertical slice:
// detect → resolve → parse → status inference → system types.
func TestAntigravityAdapter_E2E_Pipeline(t *testing.T) {
	lines := loadAllFixtures(t, "antigravity")
	if len(lines) == 0 {
		t.Fatal("no Antigravity fixtures")
	}
	meta := loadMeta(t, "antigravity")

	// 1. Detect: simulated Antigravity process evidence.
	detector := NewAntigravityDetector()
	id := detector.Detect(DetectionEvidence{
		ProcessName: "antigravity",
		CWD:         "/Users/test/.gemini/antigravity/brain/abc-123/.system_generated/logs",
	})
	if id.Kind != "antigravity" {
		t.Fatalf("detect: Kind=%q, want antigravity", id.Kind)
	}
	if id.Confidence < 0.5 {
		t.Fatalf("detect: confidence %.2f < 0.5", id.Confidence)
	}

	// 2. Detect: gemini process name should also map to antigravity.
	id2 := detector.Detect(DetectionEvidence{
		ProcessName: "gemini",
		CWD:         "/Users/test/project",
	})
	if id2.Kind != "antigravity" {
		t.Fatalf("detect(gemini): Kind=%q, want antigravity", id2.Kind)
	}

	// 3. Resolve logs.
	resolver := NewAntigravityLogResolver()
	result, err := resolver.Resolve(DetectionEvidence{
		ProcessName: "antigravity",
		CWD:         "/Users/test/.gemini/antigravity/brain/abc-123/.system_generated/logs",
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
		if contains(lr.DisplayPath, "/Users/") {
			t.Error("resolve: DisplayPath contains raw home directory")
		}
	}

	// 4. Parse: feed A8 fixtures through AntigravityParser.
	parser := NewAntigravityParser()
	pr := parser.ParseBatch(lines, "")
	if pr.Degraded {
		t.Errorf("parse degraded on valid fixtures: %v", pr.Diagnostics)
	}
	if len(pr.Events) == 0 {
		t.Fatal("parse: 0 events from fixtures")
	}

	// 5. Verify expected events from metadata.
	gotTypes := make(map[AgentEventType]bool)
	for _, e := range pr.Events {
		gotTypes[e.Type] = true
		if e.AgentKind != "antigravity" {
			t.Errorf("event AgentKind=%q, want antigravity", e.AgentKind)
		}
	}
	for _, want := range meta.ExpectedEvents {
		if !gotTypes[AgentEventType(want)] {
			t.Errorf("event %q not found (got %v)", want, eventTypes(gotTypes))
		}
	}

	// 6. Status inference must be non-empty and valid.
	if pr.Status == "" {
		t.Error("status is empty")
	}
	if pr.Status == StatusUnknown && len(pr.Events) > 0 {
		t.Error("status=unknown despite events present")
	}

	// 7. Verify ERROR_MESSAGE produces EventFailed.
	foundFailed := false
	for _, e := range pr.Events {
		if e.Type == EventFailed {
			foundFailed = true
			break
		}
	}
	if !foundFailed {
		t.Error("no EventFailed from ERROR_MESSAGE fixture")
	}

	// 8. Tool names on tool_call_started events.
	for _, e := range pr.Events {
		if e.Type == EventToolCallStarted && e.ToolName == "" {
			t.Error("tool_call_started event has empty ToolName")
		}
	}

	// 9. System types: EPHEMERAL_MESSAGE must be skipped (no event produced for it).
	systemLines := loadFixtures(t, "antigravity", "system_types.jsonl")
	if len(systemLines) > 0 {
		sysResult := parser.ParseBatch(systemLines, "")
		// EPHEMERAL_MESSAGE is skipped; CHECKPOINT+GENERIC produce EventUnknown;
		// ERROR_MESSAGE produces EventFailed. So exactly 3 events.
		if len(sysResult.Events) != 3 {
			t.Errorf("system_types: got %d events, want 3 (EPHEMERAL_MESSAGE skipped)", len(sysResult.Events))
		}
	}

	// 10. Resolver paths match A8 log inventory.
	foundAntigravityLog := false
	for _, lr := range result.Logs {
		if contains(lr.Path, ".gemini/antigravity") || contains(lr.Path, ".system_generated/logs") {
			foundAntigravityLog = true
		}
	}
	if !foundAntigravityLog {
		t.Error("resolver did not return any Antigravity log path")
	}
}
