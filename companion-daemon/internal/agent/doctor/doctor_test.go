package doctor

import (
	"testing"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
)

// ── Helpers ──

func testDoctor() *Doctor { return New() }

func testDesc(name string, versions []string) contract.AgentAdapterDescriptor {
	return contract.AgentAdapterDescriptor{
		Name:              name,
		Provider:          "TestProvider",
		ContractVersion:   contract.ContractVersion,
		SupportedVersions: versions,
	}
}

func testDescWithContract(name string, versions []string, cv string) contract.AgentAdapterDescriptor {
	return contract.AgentAdapterDescriptor{
		Name:              name,
		Provider:          "TestProvider",
		ContractVersion:   cv,
		SupportedVersions: versions,
	}
}

// ── CheckCompatibility tests ──

func TestCheckCompatibility_ExactMatch(t *testing.T) {
	d := testDoctor()
	desc := testDesc("test-adapter", []string{"1.0.0", "2.0.0"})
	report := d.CheckCompatibility(desc, "1.0.0", "jsonl")
	if !report.Compatible {
		t.Errorf("exact match: Compatible=false, want true")
	}
	if report.DriftCode != CompatOK {
		t.Errorf("exact match: DriftCode=%q, want ok", report.DriftCode)
	}
	if report.AdapterName != "test-adapter" {
		t.Errorf("AdapterName=%q, want test-adapter", report.AdapterName)
	}
}

func TestCheckCompatibility_VersionDrift(t *testing.T) {
	d := testDoctor()
	desc := testDesc("test-adapter", []string{"1.0.0"})
	report := d.CheckCompatibility(desc, "2.0.0", "jsonl")
	if report.Compatible {
		t.Error("version drift: Compatible=true, want false")
	}
	if report.DriftCode != CompatVersionDrift {
		t.Errorf("version drift: DriftCode=%q, want version_drift", report.DriftCode)
	}
	if report.DriftEvidence.ObservedVersion != "2.0.0" {
		t.Errorf("ObservedVersion=%q, want 2.0.0", report.DriftEvidence.ObservedVersion)
	}
}

func TestCheckCompatibility_EmptyVersion(t *testing.T) {
	d := testDoctor()
	desc := testDesc("test-adapter", []string{"1.0.0"})
	report := d.CheckCompatibility(desc, "", "")
	if report.Compatible {
		t.Error("empty version: Compatible=true, want false")
	}
	if report.DriftCode != CompatUnknownShape {
		t.Errorf("empty version: DriftCode=%q, want unknown_shape", report.DriftCode)
	}
}

func TestCheckCompatibility_ContractMismatch(t *testing.T) {
	d := testDoctor()
	desc := testDescWithContract("test-adapter", []string{"1.0.0"}, "old-contract-v1")
	report := d.CheckCompatibility(desc, "1.0.0", "jsonl")
	if report.Compatible {
		t.Error("contract mismatch: Compatible=true, want false")
	}
	if report.DriftCode != CompatVersionDrift {
		t.Errorf("contract mismatch: DriftCode=%q, want version_drift", report.DriftCode)
	}
}

func TestCheckCompatibility_AllFieldsPopulated(t *testing.T) {
	d := testDoctor()
	desc := testDesc("claude", []string{"2.1.202"})
	report := d.CheckCompatibility(desc, "2.1.202", "jsonl")
	if report.Provider != "TestProvider" {
		t.Errorf("Provider=%q", report.Provider)
	}
	if report.ContractVersion != contract.ContractVersion {
		t.Errorf("ContractVersion=%q", report.ContractVersion)
	}
	if len(report.SupportedVersions) != 1 || report.SupportedVersions[0] != "2.1.202" {
		t.Errorf("SupportedVersions=%v", report.SupportedVersions)
	}
	if report.DriftEvidence.AgentVersionSource != "jsonl" {
		t.Errorf("AgentVersionSource=%q", report.DriftEvidence.AgentVersionSource)
	}
}

// ── Evidence collection tests ──

func testRecord(jsonStr string) contract.RawRecord {
	return contract.RawRecord{
		Bytes:      []byte(jsonStr),
		Source:     agent.SourceJSONL,
		Provenance: contract.ProvenanceNativeLog,
	}
}

func TestCollectUnknownDiscriminators_FindsNewTypes(t *testing.T) {
	ec := NewEvidenceCollector()
	recs := []contract.RawRecord{
		testRecord(`{"type":"user","id":"1"}`),
		testRecord(`{"type":"assistant","id":"2"}`),
		testRecord(`{"type":"new_custom_event","id":"3"}`),
	}
	known := []string{"user", "assistant"}
	got := ec.collectUnknownDiscriminators(recs, known)
	if len(got) != 1 {
		t.Fatalf("got %d unknown discriminators, want 1: %v", len(got), got)
	}
	if got[0] != "new_custom_event" {
		t.Errorf("unknown discriminator=%q, want new_custom_event", got[0])
	}
}

func TestCollectUnknownDiscriminators_AllKnown(t *testing.T) {
	ec := NewEvidenceCollector()
	recs := []contract.RawRecord{
		testRecord(`{"type":"user"}`),
		testRecord(`{"type":"assistant"}`),
	}
	known := []string{"user", "assistant"}
	got := ec.collectUnknownDiscriminators(recs, known)
	if len(got) != 0 {
		t.Errorf("all-known produced %d unknowns: %v", len(got), got)
	}
}

func TestCollectUnknownDiscriminators_EmptyRecords(t *testing.T) {
	ec := NewEvidenceCollector()
	got := ec.collectUnknownDiscriminators(nil, nil)
	if len(got) != 0 {
		t.Errorf("empty records produced unknowns: %v", got)
	}
}

func TestCollectUnknownDiscriminators_Capped(t *testing.T) {
	ec := NewEvidenceCollector()
	// Generate more records than the cap.
	var recs []contract.RawRecord
	for i := 0; i < maxDiscriminators+10; i++ {
		// Use fmt.Sprintf via helper to avoid import — we use inline.
		recs = append(recs, testRecord(`{"type":"unknown_`+itoa(i)+`"}`))
	}
	got := ec.collectUnknownDiscriminators(recs, nil)
	if len(got) > maxDiscriminators {
		t.Errorf("capped at %d, got %d entries", maxDiscriminators, len(got))
	}
}

func TestCollectUnknownDiscriminators_Sanitized(t *testing.T) {
	ec := NewEvidenceCollector()
	recs := []contract.RawRecord{
		testRecord(`{"type":"` + "sk" + `-SECRET1234567890"}`),
	}
	got := ec.collectUnknownDiscriminators(recs, nil)
	if len(got) != 1 {
		t.Fatalf("got %d entries", len(got))
	}
	// Should be redacted — "sk-" prefix is stripped.
	if contract.ContainsSensitive(got[0]) {
		t.Errorf("discriminator leaked secret: %q", got[0])
	}
}

func TestCollectFieldShapeHints_FindsMismatch(t *testing.T) {
	ec := NewEvidenceCollector()
	recs := []contract.RawRecord{
		testRecord(`{"version":["not","a","string"]}`),
	}
	known := map[string]string{"version": "string"}
	got := ec.collectFieldShapeHints(recs, known)
	if len(got) != 1 {
		t.Fatalf("got %d hints, want 1: %v", len(got), got)
	}
}

func TestCollectFieldShapeHints_MatchPasses(t *testing.T) {
	ec := NewEvidenceCollector()
	recs := []contract.RawRecord{
		testRecord(`{"version":"2.1.202"}`),
	}
	known := map[string]string{"version": "string"}
	got := ec.collectFieldShapeHints(recs, known)
	if len(got) != 0 {
		t.Errorf("matching field produced hints: %v", got)
	}
}

func TestCollectFieldShapeHints_EmptyKnown(t *testing.T) {
	ec := NewEvidenceCollector()
	recs := []contract.RawRecord{
		testRecord(`{"version":1}`),
	}
	got := ec.collectFieldShapeHints(recs, nil)
	if len(got) != 0 {
		t.Errorf("empty known fields produced hints: %v", got)
	}
}

func TestCollectPathHints_FindsMissing(t *testing.T) {
	ec := NewEvidenceCollector()
	expected := []string{"~/.claude/projects/<PROJECT>/<UUID>.jsonl", "~/.codex/sessions/"}
	observed := []string{"~/.claude/projects/<PROJECT>/<UUID>.jsonl"}
	got := ec.CollectPathHints(expected, observed)
	if len(got) != 1 {
		t.Fatalf("got %d missing paths, want 1: %v", len(got), got)
	}
}

// ── Sandbox tests ──

func TestSandbox_ValidatePatch_Valid(t *testing.T) {
	al := AdapterAllowList("claude", "v2_1_202")
	s := NewSandbox(al)
	err := s.ValidatePatch([]string{
		"internal/agent/adapters/claude/v2_1_202/claude_adapter.go",
		"internal/agent/adapters/claude/v2_1_202/claude_adapter_test.go",
	})
	if err != nil {
		t.Errorf("valid patch rejected: %v", err)
	}
}

func TestSandbox_ValidatePatch_EmptyIsValid(t *testing.T) {
	al := AdapterAllowList("claude", "v2_1_202")
	s := NewSandbox(al)
	if err := s.ValidatePatch(nil); err != nil {
		t.Errorf("empty patch rejected: %v", err)
	}
	if err := s.ValidatePatch([]string{}); err != nil {
		t.Errorf("nil patch rejected: %v", err)
	}
}

func TestSandbox_ValidatePatch_ContractRejected(t *testing.T) {
	al := AdapterAllowList("claude", "v2_1_202")
	s := NewSandbox(al)
	err := s.ValidatePatch([]string{
		"internal/agent/contract/contract.go",
	})
	if err == nil {
		t.Error("contract file should be rejected by deny list")
	}
}

func TestSandbox_ValidatePatch_CrossAdapterRejected(t *testing.T) {
	al := AdapterAllowList("claude", "v2_1_202")
	s := NewSandbox(al)
	err := s.ValidatePatch([]string{
		"internal/agent/adapters/codex/v0_144_1/codex_adapter.go",
	})
	if err == nil {
		t.Error("cross-adapter file should be rejected")
	}
}

func TestSandbox_ValidatePatch_ModelsGoRejected(t *testing.T) {
	al := AdapterAllowList("claude", "v2_1_202")
	s := NewSandbox(al)
	err := s.ValidatePatch([]string{
		"internal/agent/models.go",
	})
	if err == nil {
		t.Error("models.go should be rejected by deny list")
	}
}

func TestSandbox_ValidatePatch_BridgeGoRejected(t *testing.T) {
	al := AdapterAllowList("claude", "v2_1_202")
	s := NewSandbox(al)
	err := s.ValidatePatch([]string{
		"internal/agent/bridge.go",
	})
	if err == nil {
		t.Error("bridge.go should be rejected by deny list")
	}
}

// ── DenyList tests ──

func TestDeniedPath_KnownDenied(t *testing.T) {
	denied := []string{
		"internal/agent/contract",
		"internal/agent/models.go",
		"internal/agent/bridge.go",
		"internal/term",
		"internal/mux",
		"cmd",
		"mobile",
		"docs",
	}
	for _, d := range denied {
		if !DeniedPath(d) {
			t.Errorf("%q should be denied", d)
		}
	}
}

func TestDeniedPath_SubPathDenied(t *testing.T) {
	if !DeniedPath("internal/agent/contract/harness.go") {
		t.Error("sub-path of contract should be denied")
	}
	if !DeniedPath("internal/term/pty.go") {
		t.Error("sub-path of term should be denied")
	}
}

func TestDenyList_ReturnsCopy(t *testing.T) {
	dl := DenyList()
	if len(dl) != len(denyListPaths) {
		t.Errorf("DenyList len=%d, want %d", len(dl), len(denyListPaths))
	}
	// Modifying the copy should not affect the original.
	dl[0] = "modified"
	if denyListPaths[0] == "modified" {
		t.Error("DenyList returned a slice sharing backing array with original")
	}
}

// ── Doctor.ValidatePatch tests ──

func TestDoctor_ValidatePatch_ValidClaude(t *testing.T) {
	d := testDoctor()
	err := d.ValidatePatch(
		"internal/agent/adapters/claude/v2_1_202",
		[]string{"internal/agent/adapters/claude/v2_1_202/claude_adapter.go"},
	)
	if err != nil {
		t.Errorf("valid Claude patch rejected: %v", err)
	}
}

func TestDoctor_ValidatePatch_CrossAdapter(t *testing.T) {
	d := testDoctor()
	err := d.ValidatePatch(
		"internal/agent/adapters/claude/v2_1_202",
		[]string{"internal/agent/adapters/codex/v0_144_1/codex_adapter.go"},
	)
	if err == nil {
		t.Error("cross-adapter patch should be rejected")
	}
}

// ── ObservatoryResult tests ──

func TestObservatoryResult_AllPassed(t *testing.T) {
	or := ObservatoryResult{TotalTests: 10, Passed: 10, Failed: 0}
	if !or.AllPassed() {
		t.Error("AllPassed should be true")
	}
	or2 := ObservatoryResult{TotalTests: 10, Passed: 8, Failed: 2}
	if or2.AllPassed() {
		t.Error("AllPassed should be false with failures")
	}
	or3 := ObservatoryResult{TotalTests: 0, Passed: 0, Failed: 0}
	if or3.AllPassed() {
		t.Error("AllPassed should be false with zero tests")
	}
}

// ── Safety: no leak ──

func TestDoctor_CompatReport_NoSecretLeak(t *testing.T) {
	d := testDoctor()
	desc := testDesc("test", []string{"1.0.0"})
	report := d.CheckCompatibility(desc, "sk"+"-SECRET1234567890", "jsonl")

	if contract.ContainsSensitive(report.ObservedVersion) {
		t.Errorf("ObservedVersion leaked secret: %q", report.ObservedVersion)
	}
	if contract.ContainsSensitive(report.DriftSummary) {
		t.Errorf("DriftSummary leaked secret: %q", report.DriftSummary)
	}
	if contract.ContainsSensitive(report.DriftEvidence.ObservedVersion) {
		t.Errorf("DriftEvidence.ObservedVersion leaked secret: %q", report.DriftEvidence.ObservedVersion)
	}
	if contract.ContainsSensitive(report.DriftEvidence.AgentVersionSource) {
		t.Errorf("AgentVersionSource leaked secret: %q", report.DriftEvidence.AgentVersionSource)
	}
}

func TestDoctor_DriftEvidence_HostileRecordsNoLeak(t *testing.T) {
	ec := NewEvidenceCollector()
	hostile := []contract.RawRecord{
		testRecord(`{"type":"normal"}`),
		testRecord(`{"type":"` + "sk" + `-SUPERSECRET"}`),
		testRecord(`{"type":"/Users/victim/secret"}`),
	}
	got := ec.collectUnknownDiscriminators(hostile, []string{"normal"})
	for _, d := range got {
		if contract.ContainsSensitive(d) {
			t.Errorf("unknown discriminator leaked secret: %q", d)
		}
	}
}

// ── itoa helper (avoids importing fmt in test) ──

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	if s == "" {
		s = "0"
	}
	if neg {
		s = "-" + s
	}
	return s
}
