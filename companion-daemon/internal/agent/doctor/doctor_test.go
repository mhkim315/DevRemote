package doctor

import (
	"errors"
	"fmt"
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

func testBundle(id, adapter, provider, ver, base, evDig, patchDig string, files, fixtures, cmds []string, sr ObservatoryResult) (*ReviewBundle, error) {
	return NewReviewBundle(id, adapter, provider, ver, base, evDig, patchDig,
		[]byte("test diff"), files, fixtures, "scanned: 1 files, clean", cmds, sr,
		DriftEvidence{}, string(contract.ProvenanceNativeLog), "test-ws-digest", nil)
}

// adapterPatchBytes reads the real adapter source and returns a unified diff
// that creates adapter.go in the candidate directory. This diff IS the runner
// patch — no post-patch file replacement occurs.
func adapterPatchBytes(t *testing.T) []byte {
	t.Helper()
	src, err := os.ReadFile("testdata/candidate_adapter.go")
	if err != nil {
		t.Fatalf("read adapter source: %v", err)
	}
	srcStr := string(src)
	lineCount := 0
	for _, c := range srcStr {
		if c == '\n' {
			lineCount++
		}
	}
	// Prefix each line with "+" for new-file diff.
	var diffLines []string
	for _, line := range splitLines(srcStr) {
		diffLines = append(diffLines, "+"+line)
	}
	diffBody := ""
	for _, dl := range diffLines {
		diffBody += dl + "\n"
	}
	return []byte(fmt.Sprintf(`diff --git a/internal/agent/adapters/claude/v3_0_0/adapter.go b/internal/agent/adapters/claude/v3_0_0/adapter.go
new file mode 100644
--- /dev/null
+++ b/internal/agent/adapters/claude/v3_0_0/adapter.go
@@ -0,0 +1,%d @@
%s`, lineCount, diffBody))
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i, c := range s {
		if c == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// validPatchBytes returns a minimal valid patch for sandbox tests.
func validPatchBytes() []byte {
	return []byte("diff --git a/internal/agent/adapters/claude/v3_0_0/adapter.go b/internal/agent/adapters/claude/v3_0_0/adapter.go\nnew file mode 100644\n--- /dev/null\n+++ b/internal/agent/adapters/claude/v3_0_0/adapter.go\n@@ -0,0 +1,1 @@\n+package v3_0_0\n")
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
	if !errors.Is(err, ErrEmptyPatch) {
		t.Errorf("got %v", err)
	}
	_, err = ParsePatch([]byte{})
	if !errors.Is(err, ErrEmptyPatch) {
		t.Errorf("got %v", err)
	}
}

func TestPatch_MalformedRejected(t *testing.T) {
	_, err := ParsePatch([]byte("not a patch\n"))
	if !errors.Is(err, ErrMalformedPatch) {
		t.Errorf("got %v", err)
	}
}

func TestPatch_ValidParses(t *testing.T) {
	ops, err := ParsePatch(validPatchBytes())
	if err != nil {
		t.Fatalf("valid patch: %v", err)
	}
	if len(ops) < 1 {
		t.Fatalf("got %d ops, want at least 1", len(ops))
	}
	if ops[0].DiffPath != "internal/agent/adapters/claude/v3_0_0/adapter.go" {
		t.Errorf("path=%q", ops[0].DiffPath)
	}
	if !ops[0].IsNew {
		t.Error("should be new file")
	}
}

func TestPatch_CandidateTestFileRejected(t *testing.T) {
	// Candidate must not provide its own _test.go file.
	patch := []byte(`diff --git a/internal/agent/adapters/claude/v3_0_0/adapter_test.go b/internal/agent/adapters/claude/v3_0_0/adapter_test.go
new file mode 100644
--- /dev/null
+++ b/internal/agent/adapters/claude/v3_0_0/adapter_test.go
@@ -0,0 +1,3 @@
+package v3_0_0
+import "testing"
+func TestX(t *testing.T) {}
+`)
	ops, _ := ParsePatch(patch)
	s, _ := NewSandbox(".", "claude", "v3_0_0")
	err := s.Validate(ops)
	if !errors.Is(err, ErrTestFileRejected) {
		t.Errorf("got %v, want ErrTestFileRejected", err)
	}
}

// ── Sandbox ──

func TestSandbox_ValidateNewPatch(t *testing.T) {
	ops, _ := ParsePatch(validPatchBytes())
	s, _ := NewSandbox(".", "claude", "v3_0_0")
	if err := s.Validate(ops); err != nil {
		t.Errorf("valid patch rejected: %v", err)
	}
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
	if !errors.Is(err, ErrAcceptedAdapter) {
		t.Errorf("got %v, want ErrAcceptedAdapter", err)
	}
}

func TestSandbox_BinaryRejected(t *testing.T) {
	patch := []byte("diff --git a/internal/agent/adapters/claude/v3_0_0/x.bin b/internal/agent/adapters/claude/v3_0_0/x.bin\nBinary files differ\n")
	ops, _ := ParsePatch(patch)
	s, _ := NewSandbox(".", "claude", "v3_0_0")
	err := s.Validate(ops)
	if !errors.Is(err, ErrBinaryPatch) {
		t.Errorf("got %v", err)
	}
}

func TestSandbox_DeleteRejected(t *testing.T) {
	patch := []byte("diff --git a/internal/agent/adapters/claude/v3_0_0/f.go b/internal/agent/adapters/claude/v3_0_0/f.go\ndeleted file mode 100644\n--- a/internal/agent/adapters/claude/v3_0_0/f.go\n+++ /dev/null\n@@ -1 +0,0 @@\n-old\n")
	ops, _ := ParsePatch(patch)
	s, _ := NewSandbox(".", "claude", "v3_0_0")
	err := s.Validate(ops)
	if !errors.Is(err, ErrDeleteRejected) {
		t.Errorf("got %v", err)
	}
}

func TestSandbox_RenameRejected(t *testing.T) {
	patch := []byte("diff --git a/internal/agent/adapters/claude/v3_0_0/old.go b/internal/agent/adapters/claude/v3_0_0/new.go\nsimilarity index 100%\nrename from internal/agent/adapters/claude/v3_0_0/old.go\nrename to internal/agent/adapters/claude/v3_0_0/new.go\n")
	ops, _ := ParsePatch(patch)
	s, _ := NewSandbox(".", "claude", "v3_0_0")
	err := s.Validate(ops)
	if !errors.Is(err, ErrRenameRejected) {
		t.Errorf("got %v", err)
	}
}

// ── Provider/version validation ──

func TestValidateProvider_ClosedVocab(t *testing.T) {
	if ValidateProvider("claude") != nil {
		t.Error("claude should be valid")
	}
	if ValidateProvider("codex") != nil {
		t.Error("codex should be valid")
	}
	if ValidateProvider("hacker") == nil {
		t.Error("hacker should be invalid")
	}
	if ValidateProvider("") == nil {
		t.Error("empty should be invalid")
	}
}

func TestCanonicalVersion(t *testing.T) {
	v, err := CanonicalVersion("3.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if v != "v3_0_0" {
		t.Errorf("got %q", v)
	}
	_, err = CanonicalVersion("../../etc")
	if err == nil {
		t.Error("traversal should be rejected")
	}
	_, err = CanonicalVersion("a/b")
	if err == nil {
		t.Error("separator should be rejected")
	}
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
	if !errors.Is(err, ErrSymlinkEscape) {
		t.Errorf("got %v", err)
	}
}

func TestHardenedPath_AbsoluteParentRejected(t *testing.T) {
	if _, err := HardenedPath(".", "/etc/passwd"); err == nil {
		t.Error("absolute")
	}
	if _, err := HardenedPath(".", "../../etc"); err == nil {
		t.Error("parent-relative")
	}
}

// ── Workspace escape ──

func TestApplyPatch_WorkspaceEscapeRejected(t *testing.T) {
	tmp, _ := os.MkdirTemp("", "d1-ws-*")
	defer os.RemoveAll(tmp)
	ops := []PatchOperation{{
		DiffPath: "../outside",
		NewPath:  "../outside",
		OldPath:  "../outside",
		Hunks:    []PatchHunk{{Lines: []PatchLine{{Kind: '+', Content: "bad"}}}},
	}}
	err := ApplyPatch(tmp, ops)
	if err == nil {
		t.Error("escape should be rejected")
	}
}

// ── Compatibility ──

func TestCheckCompatibility_ExactMatch(t *testing.T) {
	report := testDoctor().CheckCompatibility(testDesc("t", []string{"1.0.0"}), "1.0.0", "j")
	if !report.Compatible {
		t.Error("should be compatible")
	}
}

func TestCheckCompatibility_VersionDrift(t *testing.T) {
	report := testDoctor().CheckCompatibility(testDesc("t", []string{"1.0.0"}), "2.0.0", "j")
	if report.Compatible {
		t.Error("should not be compatible")
	}
}

// ── Evidence ──

func TestCollectDrift_UsesRealRecords(t *testing.T) {
	ec := NewEvidenceCollector()
	recs := []contract.RawRecord{testRecord(`{"type":"user"}`), testRecord(`{"type":"new_type"}`)}
	report := &CompatibilityReport{}
	ec.CollectDrift(report, recs, []string{"user"}, nil)
	if len(report.DriftEvidence.UnknownDiscriminators) != 1 {
		t.Fatalf("got %d", len(report.DriftEvidence.UnknownDiscriminators))
	}
}

// ── Approval ──

func TestApproval_SubmitAndApprove(t *testing.T) {
	store := NewApprovalStore()
	b, _ := testBundle("r1", "c", "C", "3.0.0", "abc", "ev", "patch", []string{"f.go"}, []string{"adapter.go"}, []string{"T0"}, ObservatoryResult{TotalTests: 1, Passed: 1})
	d := b.BundleDigest()
	store.Submit(b)
	if err := store.Approve("r1", d); err != nil {
		t.Fatal(err)
	}
	req, _ := store.Get("r1")
	if req.State() != StateApproved {
		t.Errorf("state=%s", req.State())
	}
}

func TestApproval_UserSuppliedDigestRequired(t *testing.T) {
	store := NewApprovalStore()
	b, _ := testBundle("r2", "c", "C", "3", "abc", "ev", "patch", []string{"f.go"}, []string{"adapter.go"}, []string{"T0"}, ObservatoryResult{TotalTests: 1, Passed: 1})
	store.Submit(b)
	if err := store.Approve("r2", "wrong"); !errors.Is(err, ErrDigestMismatch) {
		t.Errorf("got %v", err)
	}
}

func TestApproval_ActivationAlwaysFailsClosed(t *testing.T) {
	store := NewApprovalStore()
	b, _ := testBundle("r3", "c", "C", "3", "abc", "ev", "patch", []string{"f.go"}, []string{"adapter.go"}, []string{"T0"}, ObservatoryResult{TotalTests: 1, Passed: 1})
	d := b.BundleDigest()
	store.Submit(b)
	store.Approve("r3", d)
	if err := store.Activate("r3", d); !errors.Is(err, ErrActivationUnsupported) {
		t.Errorf("got %v", err)
	}
}

func TestApproval_DigestRecomputed(t *testing.T) {
	b1, _ := testBundle("r4", "c", "C", "3", "abc", "ev", "patch", []string{"f.go"}, []string{"adapter.go"}, []string{"T0"}, ObservatoryResult{TotalTests: 1, Passed: 1})
	time.Sleep(2 * time.Millisecond)
	b2, _ := testBundle("r5", "c", "C", "3", "abc", "ev", "patch", []string{"f.go"}, []string{"adapter.go"}, []string{"T0"}, ObservatoryResult{TotalTests: 1, Passed: 1})
	if b1.BundleDigest() == b2.BundleDigest() {
		t.Error("digests should differ with time")
	}
}

func TestApproval_ReplayRejected(t *testing.T) {
	store := NewApprovalStore()
	b, _ := testBundle("r6", "c", "C", "3", "abc", "ev", "patch", []string{"f.go"}, []string{"adapter.go"}, []string{"T0"}, ObservatoryResult{TotalTests: 1, Passed: 1})
	store.Submit(b)
	_, err := store.Submit(b)
	if !errors.Is(err, ErrRequestIDExists) {
		t.Errorf("got %v", err)
	}
}

// ── Orchestrator: runner called ──

func TestOrchestrator_RunnerIsCalled(t *testing.T) {
	stub := &StubRepairRunner{Patch: validPatchBytes(), Success: true}
	orch := NewOrchestrator(stub, ".")
	desc := testDesc("claude", []string{"2.1.202"})
	orch.DetectDrift(ProcessRequest{AdapterDescriptor: desc, ObservedVersion: "3.0.0", ObservedVersionSource: "j"})
	orch.CollectEvidence(nil, nil)
	if err := orch.RequestRepair(); err != nil {
		t.Fatal(err)
	}
	if stub.CapturedInput == nil {
		t.Fatal("runner not called")
	}
}

func TestOrchestrator_RunnerCrash(t *testing.T) {
	stub := &StubRepairRunner{FailErr: errors.New("crash")}
	orch := NewOrchestrator(stub, ".")
	desc := testDesc("claude", []string{"2.1.202"})
	orch.DetectDrift(ProcessRequest{AdapterDescriptor: desc, ObservedVersion: "3.0.0", ObservedVersionSource: "j"})
	orch.CollectEvidence(nil, nil)
	if err := orch.RequestRepair(); err == nil {
		t.Error("should return error")
	}
}

func TestOrchestrator_RunnerUnavailable(t *testing.T) {
	orch := NewOrchestrator(nil, ".")
	desc := testDesc("claude", []string{"2.1.202"})
	orch.DetectDrift(ProcessRequest{AdapterDescriptor: desc, ObservedVersion: "3.0.0", ObservedVersionSource: "j"})
	orch.CollectEvidence(nil, nil)
	err := orch.RequestRepair()
	if !errors.Is(err, ErrRunnerUnavailable) {
		t.Errorf("got %v", err)
	}
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

	// The runner patch IS the real adapter source. No writeCandidateFiles.
	patchBytes := adapterPatchBytes(t)
	stub := &StubRepairRunner{Patch: patchBytes, Success: true}
	desc := testDesc("claude", []string{"2.1.202"})

	actualSHA, shaErr := gitHeadSHA(repoRoot)
	if shaErr != nil || actualSHA == "" {
		t.Fatalf("git SHA: %v", shaErr)
	}

	orch := NewOrchestrator(stub, repoRoot)
	req := ProcessRequest{
		AdapterDescriptor:     desc,
		ObservedVersion:       "v3_0_0",
		ObservedVersionSource: "jsonl",
		ObservedRecordSamples: []contract.RawRecord{testRecord(`{"type":"user"}`)},
	}
	if _, err := orch.DetectDrift(req); err != nil {
		t.Fatalf("detect: %v", err)
	}
	if _, err := orch.CollectEvidence([]string{"user"}, nil); err != nil {
		t.Fatalf("evidence: %v", err)
	}
	if err := orch.RequestRepair(); err != nil {
		t.Fatalf("repair: %v", err)
	}
	if _, err := orch.SubmitPatch(stub.Patch); err != nil {
		t.Fatalf("validate: %v", err)
	}
	ws, err := orch.ApplyPatchToWorkspace()
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	defer ws.Cleanup()

	// Verify workspace digest is non-empty.
	wsDigest := ws.Digest()
	if wsDigest == "" {
		t.Fatal("workspace digest is empty")
	}

	suiteResult, err := orch.RunFixedSuites()
	if err != nil {
		t.Fatalf("suite: %v", err)
	}
	if !suiteResult.AllPassed() {
		for _, f := range suiteResult.Failures {
			t.Errorf("SUITE FAIL: %s — %s", f.TestName, f.Reason)
		}
		t.Fatalf("suite not passed: %d/%d", suiteResult.Passed, suiteResult.TotalTests)
	}

	bundle, err := orch.BuildReviewBundle("fw-e2e", actualSHA)
	if err != nil {
		t.Fatalf("bundle: %v", err)
	}
	if bundle == nil {
		t.Fatal("bundle nil")
	}
	if bundle.RequestID() != "fw-e2e" {
		t.Errorf("id=%q", bundle.RequestID())
	}
	if bundle.WorkspaceDigest() != wsDigest {
		t.Error("bundle workspace digest doesn't match")
	}
	if len(bundle.FixtureManifest()) == 0 {
		t.Error("fixture manifest is empty")
	}

	// Approve with exact digest.
	d := bundle.BundleDigest()
	if d == "" {
		t.Error("digest empty")
	}
	if err := orch.Approve("fw-e2e", d); err != nil {
		t.Fatalf("approve: %v", err)
	}
	ar, _ := orch.GetApprovalRequest("fw-e2e")
	if ar.State() != StateApproved {
		t.Errorf("state=%s", ar.State())
	}

	// Wrong digest rejected.
	if err := orch.Approve("fw-e2e", "wrong"); err == nil {
		t.Error("wrong digest should fail")
	}

	// Activate fail-closed.
	if err := orch.Activate("fw-e2e", d); !errors.Is(err, ErrActivationUnsupported) {
		t.Errorf("activate: %v", err)
	}

	// ── Reject lifecycle: fresh pending session ──

	orch2 := NewOrchestrator(&StubRepairRunner{Patch: patchBytes, Success: true}, repoRoot)
	orch2.DetectDrift(req)
	orch2.CollectEvidence([]string{"user"}, nil)
	orch2.RequestRepair()
	orch2.SubmitPatch(patchBytes)
	ws2, _ := orch2.ApplyPatchToWorkspace()
	defer ws2.Cleanup()
	orch2.RunFixedSuites()
	b2, _ := orch2.BuildReviewBundle("fw-reject", actualSHA)
	d2 := b2.BundleDigest()

	// Reject while pending must succeed.
	if err := orch2.Reject("fw-reject"); err != nil {
		t.Fatalf("reject failed: %v", err)
	}
	ar2, _ := orch2.GetApprovalRequest("fw-reject")
	if ar2.State() != StateRejected {
		t.Errorf("state=%s, want StateRejected", ar2.State())
	}

	// Approve after reject must fail.
	if err := orch2.Approve("fw-reject", d2); err == nil {
		t.Error("approve after reject should fail")
	}

	// Activate after reject must fail.
	if err := orch2.Activate("fw-reject", d2); err == nil {
		t.Error("activate after reject should fail")
	}

	// Replay of rejected request ID must fail.
	b3, _ := NewReviewBundle("fw-reject", "c", "C", "3", actualSHA, "ev", "patch",
		patchBytes, []string{"f.go"}, []string{"adapter.go"}, "scanned: 1 files, clean",
		[]string{"T0"}, ObservatoryResult{TotalTests: 1, Passed: 1},
		DriftEvidence{}, string(contract.ProvenanceNativeLog), "ws-digest", nil)
	if _, err := orch2.approval.Submit(b3); err == nil {
		t.Error("replay of rejected request ID should fail")
	}
}

// Test public FullWorkflow returns ErrRunnerUnavailable.
func TestFullWorkflow_RunnerUnavailable(t *testing.T) {
	repoRoot := findRepoRoot(t)
	desc := testDesc("claude", []string{"2.1.202"})
	_, err := FullWorkflow(nil, desc, "v3_0_0", "j",
		[]contract.RawRecord{testRecord(`{"type":"user"}`)},
		[]string{"user"}, nil, "fu-1", "abc", repoRoot)
	if !errors.Is(err, ErrRunnerUnavailable) {
		t.Errorf("got %v, want ErrRunnerUnavailable", err)
	}
	_, err = FullWorkflow(&StubRepairRunner{}, desc, "v3_0_0", "j", nil, nil, nil, "fu-2", "abc", repoRoot)
	if !errors.Is(err, ErrRunnerUnavailable) {
		t.Errorf("got %v, want ErrRunnerUnavailable", err)
	}
}

// ── Negative: post-patch source mutation detected ──

func TestE2E_PostPatchMutationDetected(t *testing.T) {
	repoRoot := findRepoRoot(t)

	patchBytes := adapterPatchBytes(t)
	stub := &StubRepairRunner{Patch: patchBytes, Success: true}
	desc := testDesc("claude", []string{"2.1.202"})

	orch := NewOrchestrator(stub, repoRoot)
	req := ProcessRequest{
		AdapterDescriptor:     desc,
		ObservedVersion:       "v3_0_0",
		ObservedVersionSource: "jsonl",
	}
	if _, err := orch.DetectDrift(req); err != nil {
		t.Fatalf("detect: %v", err)
	}
	orch.CollectEvidence([]string{"user"}, nil)
	orch.RequestRepair()
	orch.SubmitPatch(stub.Patch)
	ws, err := orch.ApplyPatchToWorkspace()
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	defer ws.Cleanup()

	// Mutate a source file after patch application.
	adapterPath := filepath.Join(ws.Root, "internal/agent/adapters/claude/v3_0_0/adapter.go")
	os.WriteFile(adapterPath, []byte("// corrupted\n"), 0644)

	// Suite must detect the mutation (digest check fails).
	_, err = orch.RunFixedSuites()
	if err == nil {
		t.Error("post-patch mutation should be detected")
	}
}

// ── Negative: no-op candidate fails ──

func TestE2E_NoOpCandidateFails(t *testing.T) {
	repoRoot := findRepoRoot(t)

	// No-op patch: empty adapter with no implementation.
	noopPatch := []byte(`diff --git a/internal/agent/adapters/claude/v3_0_0/adapter.go b/internal/agent/adapters/claude/v3_0_0/adapter.go
new file mode 100644
--- /dev/null
+++ b/internal/agent/adapters/claude/v3_0_0/adapter.go
@@ -0,0 +1,21 @@
+package v3_0_0
+import ("context"; "devremote/companion-daemon/internal/agent/contract"; "devremote/companion-daemon/internal/agent")
+type Adapter struct{}
+func (a *Adapter) Descriptor() contract.AgentAdapterDescriptor { return contract.AgentAdapterDescriptor{Name:"claude",Provider:"C",ContractVersion:contract.ContractVersion,SupportedVersions:[]string{"3.0.0"},Capabilities:[]contract.AdapterCapability{contract.CapEvents,contract.CapStatus}} }
+func (a *Adapter) Detect(_ context.Context, s contract.SessionContext) (contract.AgentIdentity, error) { return contract.AgentIdentity{Kind:"unknown",Confidence:0.1}, nil }
+func (a *Adapter) DiscoverSessions(_ context.Context, _ contract.DiscoveryInput) ([]contract.DiscoveredSession, error) { return nil, nil }
+func (a *Adapter) ReadEvents(_ context.Context, _ contract.ReadInput) (contract.ReadResult, error) { return contract.ReadResult{}, nil }
+func (a *Adapter) NormalizeEvent(_ context.Context, _ contract.RawRecord) (contract.AgentEvent, contract.DegradedInfo) { return contract.AgentEvent{Type:agent.EventUnknown,Confidence:0.2}, contract.OK() }
+func (a *Adapter) DetectApproval(_ context.Context, _ []contract.AgentEvent) ([]contract.AgentApproval, error) { return nil, nil }
+func (a *Adapter) GetStatus(_ context.Context, _ contract.StatusInput) (contract.StatusResult, error) { return contract.StatusResult{Status:agent.StatusUnknown}, nil }
+`)
	stub := &StubRepairRunner{Patch: noopPatch, Success: true}
	desc := testDesc("claude", []string{"2.1.202"})

	orch := NewOrchestrator(stub, repoRoot)
	req := ProcessRequest{AdapterDescriptor: desc, ObservedVersion: "v3_0_0", ObservedVersionSource: "jsonl"}
	orch.DetectDrift(req)
	orch.CollectEvidence([]string{"user"}, nil)
	orch.RequestRepair()
	orch.SubmitPatch(stub.Patch)
	ws, err := orch.ApplyPatchToWorkspace()
	if err != nil {
		// Workspace creation might fail if the patch doesn't compile.
		// That's an acceptable fail-closed path for a no-op candidate.
		return
	}
	defer ws.Cleanup()
	suiteResult, err := orch.RunFixedSuites()
	if err != nil {
		// Digest mismatch or other integrity failure is also acceptable.
		return
	}
	if suiteResult.AllPassed() {
		t.Error("no-op candidate should fail fixed suite")
	}
}

// ── Negative: invalid Go source fails ──

func TestE2E_InvalidGoSourceFails(t *testing.T) {
	repoRoot := findRepoRoot(t)

	invalidPatch := []byte("diff --git a/internal/agent/adapters/claude/v3_0_0/adapter.go b/internal/agent/adapters/claude/v3_0_0/adapter.go\nnew file mode 100644\n--- /dev/null\n+++ b/internal/agent/adapters/claude/v3_0_0/adapter.go\n@@ -0,0 +1,1 @@\n+this is not valid Go syntax !!!\n")
	stub := &StubRepairRunner{Patch: invalidPatch, Success: true}
	desc := testDesc("claude", []string{"2.1.202"})

	orch := NewOrchestrator(stub, repoRoot)
	req := ProcessRequest{AdapterDescriptor: desc, ObservedVersion: "v3_0_0", ObservedVersionSource: "jsonl"}
	orch.DetectDrift(req)
	orch.CollectEvidence([]string{"user"}, nil)
	orch.RequestRepair()
	orch.SubmitPatch(stub.Patch)
	ws, err := orch.ApplyPatchToWorkspace()
	if err != nil {
		return
	}
	defer ws.Cleanup()
	suiteResult, err := orch.RunFixedSuites()
	if err != nil {
		return
	}
	if suiteResult.AllPassed() {
		t.Error("invalid Go source should fail build")
	}
}

// ── Negative: missing fixture blocks review ──

func TestE2E_MissingFixtureBlocksReview(t *testing.T) {
	repoRoot := findRepoRoot(t)

	patchBytes := adapterPatchBytes(t)
	stub := &StubRepairRunner{Patch: patchBytes, Success: true}
	desc := testDesc("claude", []string{"2.1.202"})

	orch := NewOrchestrator(stub, repoRoot)
	req := ProcessRequest{AdapterDescriptor: desc, ObservedVersion: "v3_0_0", ObservedVersionSource: "jsonl"}
	orch.DetectDrift(req)
	orch.CollectEvidence([]string{"user"}, nil)
	orch.RequestRepair()
	orch.SubmitPatch(stub.Patch)
	ws, err := orch.ApplyPatchToWorkspace()
	if err != nil {
		return
	}
	defer ws.Cleanup()

	// Remove the conformance test file to create a missing fixture.
	// This will cause the suite to fail (missing test) OR the source-tree
	// digest check to detect mutation — both are acceptable fail-closed paths.
	os.Remove(filepath.Join(ws.Root, "internal/agent/adapters/claude/v3_0_0/conformance_test.go"))

	suiteResult, err := orch.RunFixedSuites()
	if err != nil {
		// Digest mismatch detected — fail-closed, which is correct.
		return
	}
	if suiteResult.AllPassed() {
		t.Error("missing conformance test should fail suite")
	}
}

// ── Fixed suite manifest regression ──

func TestFixedSuite_HasRequiredCommands(t *testing.T) {
	cmds := NewFixedSuite().Commands("./internal/agent/adapters/claude/v3_0_0/")
	labels := map[string]bool{}
	for _, c := range cmds {
		labels[c.Label] = true
	}

	required := []string{
		"build-adapters", "build-contract",
		"vet-contract", "vet-adapters",
		"T0-contract-conformance", "T0-contract-race",
		"T1-codex-conformance", "T1-codex-race", "T1-codex-regression",
		"T2-claude-conformance", "T2-claude-race", "T2-claude-regression",
		"agent-race-regression", "failure-isolation",
		"candidate-build", "candidate-vet", "candidate-conformance", "candidate-race",
	}
	for _, r := range required {
		if !labels[r] {
			t.Errorf("required suite label %q missing from manifest", r)
		}
	}
}

// ── Negative: incomplete ReviewBundle blocked ──

func TestReviewBundle_IncompleteBlocked(t *testing.T) {
	sr := ObservatoryResult{TotalTests: 1, Passed: 1}
	files := []string{"f.go"}
	fix := []string{"adapter.go"}
	cmds := []string{"T0"}

	_, err := NewReviewBundle("", "a", "p", "v", "abc", "ev", "patch",
		[]byte("diff"), files, fix, "scanned: 1 files, clean", cmds, sr,
		DriftEvidence{}, "native_log", "ws", nil)
	if err == nil {
		t.Error("empty requestID should be rejected")
	}

	_, err = NewReviewBundle("r1", "a", "p", "v", "", "ev", "patch",
		[]byte("diff"), files, fix, "scanned: 1 files, clean", cmds, sr,
		DriftEvidence{}, "native_log", "ws", nil)
	if err == nil {
		t.Error("empty baseline should be rejected")
	}

	_, err = NewReviewBundle("r2", "a", "p", "v", "abc", "ev", "patch",
		[]byte("diff"), nil, fix, "scanned: 1 files, clean", cmds, sr,
		DriftEvidence{}, "native_log", "ws", nil)
	if err == nil {
		t.Error("empty changedFiles should be rejected")
	}

	_, err = NewReviewBundle("r3", "a", "p", "v", "abc", "ev", "patch",
		[]byte("diff"), files, nil, "scanned: 1 files, clean", cmds, sr,
		DriftEvidence{}, "native_log", "ws", nil)
	if err == nil {
		t.Error("empty fixture manifest should be rejected")
	}

	_, err = NewReviewBundle("r4", "a", "p", "v", "abc", "ev", "patch",
		[]byte("diff"), files, fix, "scanned: 1 files, clean", cmds, sr,
		DriftEvidence{}, "native_log", "", nil)
	if err == nil {
		t.Error("empty workspace digest should be rejected")
	}
}

// ── Negative: post-review mutation invalidates approval ──

func TestApproval_PostReviewMutationInvalidates(t *testing.T) {
	store := NewApprovalStore()
	b1, _ := NewReviewBundle("rm1", "c", "C", "3", "abc", "ev", "patch",
		[]byte("diff v1"), []string{"f.go"}, []string{"adapter.go"}, "scanned: 1 files, clean",
		[]string{"T0"}, ObservatoryResult{TotalTests: 1, Passed: 1},
		DriftEvidence{}, "native_log", "ws1", nil)
	d1 := b1.BundleDigest()
	store.Submit(b1)

	// Different diff content → different digest.
	b2, _ := NewReviewBundle("rm2", "c", "C", "3", "abc", "ev", "patch",
		[]byte("diff v2"), []string{"f.go"}, []string{"adapter.go"}, "scanned: 1 files, clean",
		[]string{"T0"}, ObservatoryResult{TotalTests: 1, Passed: 1},
		DriftEvidence{}, "native_log", "ws2", nil)
	d2 := b2.BundleDigest()
	if d1 == d2 {
		t.Error("different diff content should produce different digest")
	}

	// Approving with wrong digest fails.
	store.Submit(b2)
	err := store.Approve("rm2", d1)
	if !errors.Is(err, ErrDigestMismatch) {
		t.Errorf("wrong digest should be rejected: got %v", err)
	}
}
