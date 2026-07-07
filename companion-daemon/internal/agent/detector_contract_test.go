package agent

import "testing"

type DetectorFactory func(t *testing.T) AgentDetector
type ResolverFactory func(t *testing.T) LogResolver

func RunDetectorContract(t *testing.T, name string, df DetectorFactory, rf ResolverFactory) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		t.Run("Detect_EmptyEvidence", func(t *testing.T) { testDetectEmpty(t, df) })
		t.Run("Detect_UnknownProcess", func(t *testing.T) { testDetectUnknown(t, df) })
		t.Run("Detect_KnownAgent", func(t *testing.T) { testDetectKnown(t, df) })
		t.Run("Detect_LowConfidence", func(t *testing.T) { testDetectLowConf(t, df) })
		t.Run("Resolver_NoLogs", func(t *testing.T) { testResolverNoLogs(t, rf) })
		t.Run("Resolver_ReturnsEmptyOnMissing", func(t *testing.T) { testResolverEmpty(t, rf) })
		t.Run("Detect_WithLogs", func(t *testing.T) { testDetectWithLogs(t, df, rf) })
	})
}

func testDetectEmpty(t *testing.T, df DetectorFactory) {
	d := df(t)
	id := d.Detect(DetectionEvidence{})
	if id.Kind == "" {
		t.Error("empty evidence: Kind is empty")
	}
	if id.Confidence > 0.3 {
		t.Errorf("empty evidence: confidence %.2f too high for no evidence", id.Confidence)
	}
}

func testDetectUnknown(t *testing.T, df DetectorFactory) {
	d := df(t)
	id := d.Detect(DetectionEvidence{
		ProcessName: "unknown_binary_xyz",
		CWD:         "/tmp",
	})
	if id.Kind != "unknown" {
		t.Errorf("unknown process: Kind=%q, want unknown", id.Kind)
	}
	if id.Confidence > 0.5 {
		t.Errorf("unknown process: confidence %.2f should be <0.5", id.Confidence)
	}
}

func testDetectKnown(t *testing.T, df DetectorFactory) {
	d := df(t)
	id := d.Detect(DetectionEvidence{
		ProcessName: "claude",
		CWD:         "/Users/test/project",
	})
	if id.Kind == "unknown" {
		t.Error("known process 'claude': got unknown")
	}
}

func testDetectLowConf(t *testing.T, df DetectorFactory) {
	d := df(t)
	// Process name alone, no cwd/logs → should be low confidence.
	id := d.Detect(DetectionEvidence{
		ProcessName: "node",
	})
	if id.Confidence > 0.7 {
		t.Errorf("node process alone: confidence %.2f should be <0.7", id.Confidence)
	}
}

func testResolverNoLogs(t *testing.T, rf ResolverFactory) {
	r := rf(t)
	logs, err := r.Resolve(DetectionEvidence{
		CWD: "/nonexistent/path",
	})
	if err != nil {
		t.Errorf("no logs: unexpected error: %v", err)
	}
	if len(logs) != 0 {
		t.Errorf("no logs: got %d refs, want 0", len(logs))
	}
}

func testResolverEmpty(t *testing.T, rf ResolverFactory) {
	r := rf(t)
	logs, err := r.Resolve(DetectionEvidence{})
	if err != nil {
		t.Errorf("empty evidence: unexpected error: %v", err)
	}
	if len(logs) != 0 {
		t.Errorf("empty evidence: got %d refs, want 0", len(logs))
	}
}

func testDetectWithLogs(t *testing.T, df DetectorFactory, rf ResolverFactory) {
	d := df(t)
	r := rf(t)
	// With log paths in evidence, confidence should increase.
	logs, _ := r.Resolve(DetectionEvidence{
		ProcessName: "claude",
		CWD:         "/Users/test/project",
	})
	evidence := DetectionEvidence{
		ProcessName: "claude",
		CWD:         "/Users/test/project",
		LogPaths:    logs,
	}
	id := d.Detect(evidence)
	if id.Kind == "unknown" && len(logs) > 0 {
		t.Error("with logs + process name: still unknown")
	}
}

// --- Mock implementations for self-test ---

type mockDetector struct{}

func (d *mockDetector) Detect(ev DetectionEvidence) AgentIdentity {
	confidence := 0.1 // base for empty evidence
	kind := "unknown"

	switch ev.ProcessName {
	case "claude":
		kind = "claude"
		confidence = 0.6
	case "codex":
		kind = "codex"
		confidence = 0.6
	case "node":
		kind = "unknown"
		confidence = 0.2
	default:
		kind = "unknown"
		confidence = 0.1
	}

	// CWD evidence boosts confidence.
	if ev.CWD != "" {
		confidence += 0.1
	}
	// Log paths are strong evidence.
	if len(ev.LogPaths) > 0 {
		confidence += 0.2
	}
	if confidence > 1.0 {
		confidence = 1.0
	}

	return AgentIdentity{
		Kind:       kind,
		Confidence: confidence,
	}
}

type mockResolver struct{}

func (r *mockResolver) Resolve(ev DetectionEvidence) ([]LogRef, error) {
	if ev.CWD == "" || ev.CWD == "/nonexistent/path" {
		return nil, nil
	}
	// Mock: return a log path if CWD looks like a project.
	if ev.ProcessName == "claude" {
		return []LogRef{{Path: ev.CWD + "/.claude/projects/test/log.jsonl", Type: SourceJSONL, Agent: "claude"}}, nil
	}
	return nil, nil
}

func TestMockDetector_Contract(t *testing.T) {
	RunDetectorContract(t, "mock", func(t *testing.T) AgentDetector {
		return &mockDetector{}
	}, func(t *testing.T) LogResolver {
		return &mockResolver{}
	})
}
