package doctor

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
)

// ── Helpers ──

func testDoctor() *Doctor { return New() }
func testDesc(name string, versions []string) contract.AgentAdapterDescriptor {
	return contract.AgentAdapterDescriptor{
		Name: name, Provider: "TestProvider", ContractVersion: contract.ContractVersion, SupportedVersions: versions,
	}
}
func testRecord(jsonStr string) contract.RawRecord {
	return contract.RawRecord{Bytes: []byte(jsonStr), Source: agent.SourceJSONL, Provenance: contract.ProvenanceNativeLog}
}

func validPatchBytes() []byte {
	return []byte(`diff --git a/internal/agent/adapters/claude/v3_0_0/adapter.go b/internal/agent/adapters/claude/v3_0_0/adapter.go
new file mode 100644
--- /dev/null
+++ b/internal/agent/adapters/claude/v3_0_0/adapter.go
@@ -0,0 +1,18 @@
+package v3_0_0
+import (
+	"context"
+	"devremote/companion-daemon/internal/agent"
+	"devremote/companion-daemon/internal/agent/contract"
+)
+type Adapter struct{}
+var _ contract.AgentAdapter = (*Adapter)(nil)
+func (a *Adapter) Descriptor() contract.AgentAdapterDescriptor {
+	return contract.AgentAdapterDescriptor{Name: "claude", Provider: "Claude Code", ContractVersion: contract.ContractVersion, SupportedVersions: []string{"3.0.0"}, Capabilities: []contract.AdapterCapability{contract.CapEvents, contract.CapStatus}}
+}
+func (a *Adapter) Detect(_ context.Context, _ contract.SessionContext) (contract.AgentIdentity, error) { return contract.AgentIdentity{Kind: "claude", Confidence: 0.9}, nil }
+func (a *Adapter) DiscoverSessions(_ context.Context, _ contract.DiscoveryInput) ([]contract.DiscoveredSession, error) { return nil, nil }
+func (a *Adapter) ReadEvents(_ context.Context, _ contract.ReadInput) (contract.ReadResult, error) { return contract.ReadResult{}, nil }
+func (a *Adapter) NormalizeEvent(_ context.Context, _ contract.RawRecord) (contract.AgentEvent, contract.DegradedInfo) { return contract.AgentEvent{Type: agent.EventUnknown, Confidence: 0.2}, contract.OK() }
+func (a *Adapter) DetectApproval(_ context.Context, _ []contract.AgentEvent) ([]contract.AgentApproval, error) { return nil, nil }
+func (a *Adapter) GetStatus(_ context.Context, _ contract.StatusInput) (contract.StatusResult, error) { return contract.StatusResult{Status: agent.StatusUnknown}, nil }
+const supportedVersion = "3.0.0"
diff --git a/internal/agent/adapters/claude/v3_0_0/adapter_test.go b/internal/agent/adapters/claude/v3_0_0/adapter_test.go
new file mode 100644
--- /dev/null
+++ b/internal/agent/adapters/claude/v3_0_0/adapter_test.go
@@ -0,0 +1,4 @@
+package v3_0_0
+import "testing"
+func TestConformance(t *testing.T) {}
`)
}

// ── P0: Patch path mismatch ──

func TestPatch_PathMismatchRejected(t *testing.T) {
	// diff header says safe, +++ says escape
	hostile := []byte(`diff --git a/safe b/internal/agent/adapters/claude/v3_0_0/safe.go
new file mode 100644
--- /dev/null
+++ b/../../outside-workspace
@@ -0,0 +1 @@
+hostile
`)
	_, err := ParsePatch(hostile)
	if !errors.Is(err, ErrPathMismatch) {
		t.Errorf("path mismatch: got %v, want ErrPathMismatch", err)
	}
}

func TestPatch_EmptyRejected(t *testing.T) {
	_, err := ParsePatch(nil)
	if !errors.Is(err, ErrEmptyPatch) { t.Errorf("got %v", err) }
	_, err = ParsePatch([]byte{})
	if !errors.Is(err, ErrEmptyPatch) { t.Errorf("got %v", err) }
}

func TestPatch_MalformedRejected(t *testing.T) {
	_, err := ParsePatch([]byte("not a patch\n"))
	if !errors.Is(err, ErrMalformedPatch) { t.Errorf("got %v", err) }
}

func TestPatch_ValidParses(t *testing.T) {
	ops, err := ParsePatch(validPatchBytes())
	if err != nil { t.Fatalf("valid patch: %v", err) }
	if len(ops) < 2 { t.Fatalf("got %d ops, want 2", len(ops)) }
	if ops[0].DiffPath != "internal/agent/adapters/claude/v3_0_0/adapter.go" {
		t.Errorf("path=%q", ops[0].DiffPath)
	}
	if !ops[0].IsNew { t.Error("should be new file") }
}

// ── Sandbox ──

func TestSandbox_ValidateNewPatch(t *testing.T) {
	ops, _ := ParsePatch(validPatchBytes())
	s, _ := NewSandbox(".", "claude", "v3_0_0")
	if err := s.Validate(ops); err != nil { t.Errorf("valid patch rejected: %v", err) }
}

func TestSandbox_AcceptedAdapterRejected(t *testing.T) {
	patch := []byte(`diff --git a/internal/agent/adapters/claude/v2_1_202/claude_adapter.go b/internal/agent/adapters/claude/v2_1_202/claude_adapter.go
--- a/internal/agent/adapters/claude/v2_1_202/claude_adapter.go
+++ b/internal/agent/adapters/claude/v2_1_202/claude_adapter.go
@@ -1,3 +1,3 @@
-old
+new
`)
	ops, _ := ParsePatch(patch)
	s, _ := NewSandbox(".", "claude", "v3_0_0")
	err := s.Validate(ops)
	if !errors.Is(err, ErrAcceptedAdapter) { t.Errorf("got %v, want ErrAcceptedAdapter", err) }
}

func TestSandbox_BinaryRejected(t *testing.T) {
	patch := []byte("diff --git a/internal/agent/adapters/claude/v3_0_0/x.bin b/internal/agent/adapters/claude/v3_0_0/x.bin\nBinary files differ\n")
	ops, _ := ParsePatch(patch)
	s, _ := NewSandbox(".", "claude", "v3_0_0")
	err := s.Validate(ops)
	if !errors.Is(err, ErrBinaryPatch) { t.Errorf("got %v", err) }
}

func TestSandbox_DeleteRejected(t *testing.T) {
	patch := []byte("diff --git a/internal/agent/adapters/claude/v3_0_0/f.go b/internal/agent/adapters/claude/v3_0_0/f.go\ndeleted file mode 100644\n--- a/internal/agent/adapters/claude/v3_0_0/f.go\n+++ /dev/null\n@@ -1 +0,0 @@\n-old\n")
	ops, _ := ParsePatch(patch)
	s, _ := NewSandbox(".", "claude", "v3_0_0")
	err := s.Validate(ops)
	if !errors.Is(err, ErrDeleteRejected) { t.Errorf("got %v", err) }
}

func TestSandbox_RenameRejected(t *testing.T) {
	patch := []byte("diff --git a/internal/agent/adapters/claude/v3_0_0/old.go b/internal/agent/adapters/claude/v3_0_0/new.go\nsimilarity index 100%\nrename from internal/agent/adapters/claude/v3_0_0/old.go\nrename to internal/agent/adapters/claude/v3_0_0/new.go\n")
	ops, _ := ParsePatch(patch)
	s, _ := NewSandbox(".", "claude", "v3_0_0")
	err := s.Validate(ops)
	if !errors.Is(err, ErrRenameRejected) { t.Errorf("got %v", err) }
}

// ── Provider/version validation ──

func TestValidateProvider_ClosedVocab(t *testing.T) {
	if ValidateProvider("claude") != nil { t.Error("claude should be valid") }
	if ValidateProvider("codex") != nil { t.Error("codex should be valid") }
	if ValidateProvider("hacker") == nil { t.Error("hacker should be invalid") }
	if ValidateProvider("") == nil { t.Error("empty should be invalid") }
}

func TestCanonicalVersion(t *testing.T) {
	v, err := CanonicalVersion("3.0.0")
	if err != nil { t.Fatal(err) }
	if v != "v3_0_0" { t.Errorf("got %q", v) }
	_, err = CanonicalVersion("../../etc")
	if err == nil { t.Error("traversal should be rejected") }
	_, err = CanonicalVersion("a/b")
	if err == nil { t.Error("separator should be rejected") }
}

// ── HardenedPath ──

func TestHardenedPath_SymlinkDetection(t *testing.T) {
	tmp, _ := os.MkdirTemp("", "d1-test-*")
	defer os.RemoveAll(tmp)
	target := filepath.Join(tmp, "target")
	link := filepath.Join(tmp, "link")
	os.WriteFile(target, []byte("x"), 0644)
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlinks not supported")
	}
	_, err := HardenedPath(tmp, "link")
	if !errors.Is(err, ErrSymlinkEscape) { t.Errorf("got %v", err) }
}

func TestHardenedPath_AbsoluteParentRejected(t *testing.T) {
	if _, err := HardenedPath(".", "/etc/passwd"); err == nil { t.Error("absolute") }
	if _, err := HardenedPath(".", "../../etc"); err == nil { t.Error("parent-relative") }
}

// ── Workspace escape ──

func TestApplyPatch_WorkspaceEscapeRejected(t *testing.T) {
	tmp, _ := os.MkdirTemp("", "d1-ws-*")
	defer os.RemoveAll(tmp)
	// Patch targeting a path that escapes workspace via Rel.
	ops := []PatchOperation{{
		DiffPath: "../outside",
		NewPath:  "../outside",
		OldPath:  "../outside",
		Hunks:    []PatchHunk{{Lines: []PatchLine{{Kind: '+', Content: "bad"}}}},
	}}
	err := ApplyPatch(tmp, ops)
	if err == nil { t.Error("escape should be rejected") }
}

// ── Compatibility ──

func TestCheckCompatibility_ExactMatch(t *testing.T) {
	report := testDoctor().CheckCompatibility(testDesc("t", []string{"1.0.0"}), "1.0.0", "j")
	if !report.Compatible { t.Error("should be compatible") }
}

func TestCheckCompatibility_VersionDrift(t *testing.T) {
	report := testDoctor().CheckCompatibility(testDesc("t", []string{"1.0.0"}), "2.0.0", "j")
	if report.Compatible { t.Error("should not be compatible") }
}

// ── Evidence ──

func TestCollectDrift_UsesRealRecords(t *testing.T) {
	ec := NewEvidenceCollector()
	recs := []contract.RawRecord{testRecord(`{"type":"user"}`), testRecord(`{"type":"new_type"}`)}
	report := &CompatibilityReport{}
	ec.CollectDrift(report, recs, []string{"user"}, nil)
	if len(report.DriftEvidence.UnknownDiscriminators) != 1 { t.Fatalf("got %d", len(report.DriftEvidence.UnknownDiscriminators)) }
}

// ── Approval ──

func TestApproval_SubmitAndApprove(t *testing.T) {
	store := NewApprovalStore()
	b, _ := NewReviewBundle("r1", "c", "C", "3.0.0", "abc", "ev", "patch", []string{"f.go"}, []string{"T0"}, ObservatoryResult{TotalTests: 1, Passed: 1})
	d := b.BundleDigest()
	store.Submit(b)
	if err := store.Approve("r1", d); err != nil { t.Fatal(err) }
	req, _ := store.Get("r1")
	if req.State() != StateApproved { t.Errorf("state=%s", req.State()) }
}

func TestApproval_UserSuppliedDigestRequired(t *testing.T) {
	store := NewApprovalStore()
	b, _ := NewReviewBundle("r2", "c", "C", "3", "abc", "ev", "patch", []string{"f.go"}, []string{"T0"}, ObservatoryResult{TotalTests: 1, Passed: 1})
	store.Submit(b)
	if err := store.Approve("r2", "wrong"); !errors.Is(err, ErrDigestMismatch) { t.Errorf("got %v", err) }
}

func TestApproval_ActivationAlwaysFailsClosed(t *testing.T) {
	store := NewApprovalStore()
	b, _ := NewReviewBundle("r3", "c", "C", "3", "abc", "ev", "patch", []string{"f.go"}, []string{"T0"}, ObservatoryResult{TotalTests: 1, Passed: 1})
	d := b.BundleDigest()
	store.Submit(b)
	store.Approve("r3", d)
	if err := store.Activate("r3", d); !errors.Is(err, ErrActivationUnsupported) { t.Errorf("got %v", err) }
}

func TestApproval_DigestRecomputed(t *testing.T) {
	b1, _ := NewReviewBundle("r4", "c", "C", "3", "abc", "ev", "patch", []string{"f.go"}, []string{"T0"}, ObservatoryResult{TotalTests: 1, Passed: 1})
	time.Sleep(2 * time.Millisecond)
	b2, _ := NewReviewBundle("r5", "c", "C", "3", "abc", "ev", "patch", []string{"f.go"}, []string{"T0"}, ObservatoryResult{TotalTests: 1, Passed: 1})
	if b1.BundleDigest() == b2.BundleDigest() { t.Error("digests should differ with time") }
}

func TestApproval_ReplayRejected(t *testing.T) {
	store := NewApprovalStore()
	b, _ := NewReviewBundle("r6", "c", "C", "3", "abc", "ev", "patch", []string{"f.go"}, []string{"T0"}, ObservatoryResult{TotalTests: 1, Passed: 1})
	store.Submit(b)
	_, err := store.Submit(b)
	if !errors.Is(err, ErrRequestIDExists) { t.Errorf("got %v", err) }
}

// ── Orchestrator: runner called ──

func TestOrchestrator_RunnerIsCalled(t *testing.T) {
	stub := &StubRepairRunner{Patch: validPatchBytes(), Success: true}
	orch := NewOrchestrator(stub, ".")
	desc := testDesc("claude", []string{"2.1.202"})
	orch.DetectDrift(ProcessRequest{AdapterDescriptor: desc, ObservedVersion: "3.0.0", ObservedVersionSource: "j"})
	orch.CollectEvidence(nil, nil)
	if err := orch.RequestRepair(); err != nil { t.Fatal(err) }
	if stub.CapturedInput == nil { t.Fatal("runner not called") }
}

func TestOrchestrator_RunnerCrash(t *testing.T) {
	stub := &StubRepairRunner{FailErr: errors.New("crash")}
	orch := NewOrchestrator(stub, ".")
	desc := testDesc("claude", []string{"2.1.202"})
	orch.DetectDrift(ProcessRequest{AdapterDescriptor: desc, ObservedVersion: "3.0.0", ObservedVersionSource: "j"})
	orch.CollectEvidence(nil, nil)
	if err := orch.RequestRepair(); err == nil { t.Error("should return error") }
}

func TestOrchestrator_RunnerUnavailable(t *testing.T) {
	orch := NewOrchestrator(nil, ".")
	desc := testDesc("claude", []string{"2.1.202"})
	orch.DetectDrift(ProcessRequest{AdapterDescriptor: desc, ObservedVersion: "3.0.0", ObservedVersionSource: "j"})
	orch.CollectEvidence(nil, nil)
	err := orch.RequestRepair()
	if !errors.Is(err, ErrRunnerUnavailable) { t.Errorf("got %v", err) }
}

// findRepoRoot walks up from the test directory to find go.mod.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, _ := os.Getwd()
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Skip("cannot find repo root (go.mod)")
	return ""
}

// ── E2E: FullWorkflow vertical pipeline ──

func TestFullWorkflow_CompleteVertical(t *testing.T) {
	repoRoot := findRepoRoot(t)

	stub := &StubRepairRunner{Patch: validPatchBytes(), Success: true}
	desc := testDesc("claude", []string{"2.1.202"})

	// Call the test-only production entry.
	actualSHA, shaErr := gitHeadSHA(repoRoot)
	if shaErr != nil || actualSHA == "" { t.Fatalf("git SHA: %v", shaErr) }

	sess, err := fullWorkflowTest(stub, desc, "v3_0_0", "jsonl",
		[]contract.RawRecord{testRecord(`{"type":"user"}`)},
		[]string{"user"}, nil, "fw-e2e", actualSHA, repoRoot)
	if err != nil {
		// Print suite failures for debugging.
		t.Errorf("fullWorkflowTest failed: %v", err)
		// Run suite separately to see failures.
		t.Fatal("E2E workflow failed — check suite results above")
	}
	if sess == nil { t.Fatal("session nil") }
	defer sess.Close()

	bundle := sess.Bundle
	if bundle == nil { t.Fatal("bundle nil") }
	if bundle.RequestID() != "fw-e2e" { t.Errorf("id=%q", bundle.RequestID()) }

	// Verify runner was called.
	if stub.CapturedInput == nil { t.Fatal("runner not called") }

	// Approve with correct digest.
	d := bundle.BundleDigest()
	if d == "" { t.Error("digest empty") }
	if err := sess.Approve(d); err != nil { t.Fatalf("approve: %v", err) }
	ar, _ := sess.GetRequest()
	if ar.State() != StateApproved { t.Errorf("state=%s, want approved", ar.State()) }

	// Wrong digest rejected (already approved, so wrong state).
	if err := sess.Approve("wrong"); err == nil { t.Error("wrong digest should fail") }

	// Activate fail-closed.
	if err := sess.Activate(d); !errors.Is(err, ErrActivationUnsupported) {
		t.Errorf("activate: %v", err)
	}

	// Reject a fresh session.
	stub2 := &StubRepairRunner{Patch: validPatchBytes(), Success: true}
	sess2, err := fullWorkflowTest(stub2, desc, "v3_0_0", "j",
		[]contract.RawRecord{testRecord(`{"type":"user"}`)},
		[]string{"user"}, nil, "fw-reject", actualSHA, repoRoot)
	if err != nil { t.Fatalf("fullWorkflowTest reject: %v", err) }
	defer sess2.Close()
	d2 := sess2.Bundle.BundleDigest()
	if err := sess2.Reject(); err != nil { t.Fatalf("reject: %v", err) }
	ar2, _ := sess2.GetRequest()
	if ar2.State() != StateRejected { t.Errorf("state=%s, want rejected", ar2.State()) }
	// Cannot approve a rejected request.
	if err := sess2.Approve(d2); err == nil { t.Error("approve after reject should fail") }
}

// Test public FullWorkflow returns ErrRunnerUnavailable.
func TestFullWorkflow_RunnerUnavailable(t *testing.T) {
	repoRoot := findRepoRoot(t)
	desc := testDesc("claude", []string{"2.1.202"})
	_, err := FullWorkflow(nil, desc, "v3_0_0", "j",
		[]contract.RawRecord{testRecord(`{"type":"user"}`)},
		[]string{"user"}, nil, "fu-1", "abc", repoRoot)
	if !errors.Is(err, ErrRunnerUnavailable) { t.Errorf("got %v, want ErrRunnerUnavailable", err) }
	// Also with non-nil runner.
	_, err = FullWorkflow(&StubRepairRunner{}, desc, "v3_0_0", "j", nil, nil, nil, "fu-2", "abc", repoRoot)
	if !errors.Is(err, ErrRunnerUnavailable) { t.Errorf("got %v, want ErrRunnerUnavailable", err) }
}

// ── itoa ──

func itoa(i int) string {
	if i == 0 { return "0" }
	s := ""; neg := false
	if i < 0 { neg = true; i = -i }
	for i > 0 { s = string(rune('0'+i%10)) + s; i /= 10 }
	if neg { s = "-" + s }
	return s
}
