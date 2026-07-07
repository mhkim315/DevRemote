package agent

import "testing"

type DetectorFactory func(t *testing.T) AgentDetector
type ResolverFactory func(t *testing.T) LogResolver

func RunDetectorContract(t *testing.T, name string, df DetectorFactory, rf ResolverFactory) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		t.Run("Detect_EmptyEvidence", func(t *testing.T) { testDetectEmpty(t, df) })
		t.Run("Detect_UnknownProcess", func(t *testing.T) { testDetectUnknown(t, df) })
		t.Run("Detect_KnownAgent_Claude", func(t *testing.T) { testDetectKnown(t, df, "claude") })
		t.Run("Detect_KnownAgent_Codex", func(t *testing.T) { testDetectKnown(t, df, "codex") })
		t.Run("Detect_FalsePositive", func(t *testing.T) { testDetectFalsePositive(t, df) })
		t.Run("Detect_LowConfidence", func(t *testing.T) { testDetectLowConf(t, df) })
		t.Run("Detect_ManualOverride", func(t *testing.T) { testDetectManual(t, df) })
		t.Run("Resolver_NoLogs", func(t *testing.T) { testResolverNoLogs(t, rf) })
		t.Run("Resolver_Degraded", func(t *testing.T) { testResolverDegraded(t, rf) })
		t.Run("Detect_WithLogs", func(t *testing.T) { testDetectWithLogs(t, df, rf) })
	})
}

func testDetectEmpty(t *testing.T, df DetectorFactory) {
	d := df(t)
	id := d.Detect(DetectionEvidence{})
	assertConfidenceInvariant(t, id)
	if id.Kind == "" {
		t.Error("empty evidence: Kind is empty")
	}
	if id.Confidence > 0.3 {
		t.Errorf("empty evidence: confidence %.2f too high", id.Confidence)
	}
}

func testDetectUnknown(t *testing.T, df DetectorFactory) {
	d := df(t)
	id := d.Detect(DetectionEvidence{ProcessName: "unknown_binary_xyz", CWD: "/tmp"})
	assertConfidenceInvariant(t, id)
	if id.Kind != "unknown" {
		t.Errorf("unknown process: Kind=%q, want unknown", id.Kind)
	}
}

func testDetectKnown(t *testing.T, df DetectorFactory, agent string) {
	d := df(t)
	id := d.Detect(DetectionEvidence{
		ProcessName: agent,
		CWD:         "/Users/test/project",
	})
	assertConfidenceInvariant(t, id)
	if id.Kind == "unknown" {
		t.Errorf("known process %q: got unknown", agent)
	}
	if id.Confidence < 0.5 {
		t.Errorf("known process %q: confidence %.2f < 0.5", agent, id.Confidence)
	}
}

func testDetectFalsePositive(t *testing.T, df DetectorFactory) {
	d := df(t)
	id := d.Detect(DetectionEvidence{ProcessName: "node"})
	assertConfidenceInvariant(t, id)
	if id.Kind != "unknown" {
		t.Errorf("low-confidence process: Kind=%q, want unknown (false positive prevention)", id.Kind)
	}
}

func testDetectLowConf(t *testing.T, df DetectorFactory) {
	d := df(t)
	id := d.Detect(DetectionEvidence{ProcessName: "node"})
	assertConfidenceInvariant(t, id)
	if id.Confidence > 0.7 {
		t.Errorf("node alone: confidence %.2f should be <0.7", id.Confidence)
	}
}

func testDetectManual(t *testing.T, df DetectorFactory) {
	d := df(t)
	id := d.Detect(DetectionEvidence{
		ProcessName: "unknown_process",
		ManualLink:  &ManualEvidence{AgentKind: "codex"},
	})
	assertConfidenceInvariant(t, id)
	if id.Kind != "codex" {
		t.Errorf("manual link: Kind=%q, want codex (manual must override)", id.Kind)
	}
	if id.Confidence < 0.9 {
		t.Errorf("manual link: confidence %.2f < 0.9 (manual should be high confidence)", id.Confidence)
	}
}

func testResolverNoLogs(t *testing.T, rf ResolverFactory) {
	r := rf(t)
	result, err := r.Resolve(DetectionEvidence{CWD: "/nonexistent/path"})
	if err != nil {
		t.Errorf("no logs: unexpected error: %v", err)
	}
	if len(result.Logs) != 0 {
		t.Errorf("no logs: got %d refs, want 0", len(result.Logs))
	}
}

func testResolverDegraded(t *testing.T, rf ResolverFactory) {
	r := rf(t)
	result, err := r.Resolve(DetectionEvidence{
		CWD:         "/root",
		ProcessName: "claude",
	})
	if err != nil {
		t.Errorf("degraded: unexpected error (should set Degraded, not return error): %v", err)
	}
	// Permission denied must set Degraded=true with diagnostics.
	if !result.Degraded {
		t.Error("degraded: Degraded=false on permission denied (want true)")
	}
	if len(result.Diagnostics) == 0 {
		t.Error("degraded: Diagnostics is empty (want non-empty)")
	}
}

func testDetectWithLogs(t *testing.T, df DetectorFactory, rf ResolverFactory) {
	d := df(t)
	r := rf(t)
	result, _ := r.Resolve(DetectionEvidence{
		ProcessName: "claude",
		CWD:         "/Users/test/project",
	})
	// DisplayPath must be non-empty and must not contain raw home paths.
	for _, lr := range result.Logs {
		if lr.DisplayPath == "" {
			t.Error("LogRef.DisplayPath is empty")
		}
		if stringsContain(lr.DisplayPath, "/Users/") {
			t.Errorf("LogRef.DisplayPath contains raw home path: %s", lr.DisplayPath)
		}
	}

	evidence := DetectionEvidence{
		ProcessName: "claude",
		CWD:         "/Users/test/project",
		LogPaths:    result.Logs,
	}
	id := d.Detect(evidence)
	assertConfidenceInvariant(t, id)
	if id.Kind == "unknown" && len(result.Logs) > 0 {
		t.Error("with logs + process name: still unknown")
	}
}

// assertConfidenceInvariant enforces: confidence < 0.5 → Kind == "unknown".
func assertConfidenceInvariant(t *testing.T, id AgentIdentity) {
	t.Helper()
	if id.Confidence < 0.5 && id.Kind != "unknown" {
		t.Errorf("confidence invariant violated: confidence=%.2f < 0.5 but Kind=%q (must be unknown)", id.Confidence, id.Kind)
	}
}

func stringsContain(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// --- Mock implementations ---

type mockDetector struct{}

func (d *mockDetector) Detect(ev DetectionEvidence) AgentIdentity {
	// Manual link has highest priority.
	if ev.ManualLink != nil && ev.ManualLink.AgentKind != "" {
		return AgentIdentity{Kind: ev.ManualLink.AgentKind, DisplayName: ev.ManualLink.AgentKind, Confidence: 1.0}
	}

	confidence := 0.1
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

	if ev.CWD != "" {
		confidence += 0.1
	}
	if len(ev.LogPaths) > 0 {
		confidence += 0.2
	}
	if confidence > 1.0 {
		confidence = 1.0
	}

	// Hard rule: confidence < 0.5 → unknown.
	if confidence < 0.5 {
		kind = "unknown"
	}

	return AgentIdentity{Kind: kind, Confidence: confidence}
}

type mockResolver struct{}

func (r *mockResolver) Resolve(ev DetectionEvidence) (ResolveResult, error) {
	if ev.CWD == "/root" {
		return ResolveResult{
			Degraded:    true,
			Diagnostics: []string{"permission denied: <PATH>"},
		}, nil
	}
	if ev.CWD == "" || ev.CWD == "/nonexistent/path" {
		return ResolveResult{}, nil
	}
	if ev.ProcessName == "claude" {
		return ResolveResult{
			Logs: []LogRef{{
				Path:        ev.CWD + "/.claude/projects/test/log.jsonl",
				DisplayPath: "<PROJECT>/.claude/projects/test/log.jsonl",
				Type:        SourceJSONL,
				Agent:       "claude",
			}},
		}, nil
	}
	return ResolveResult{}, nil
}

func TestMockDetector_Contract(t *testing.T) {
	RunDetectorContract(t, "mock", func(t *testing.T) AgentDetector {
		return &mockDetector{}
	}, func(t *testing.T) LogResolver {
		return &mockResolver{}
	})
}
