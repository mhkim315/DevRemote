// Package doctor implements the D1 Adapter Doctor/Repair workflow: a
// user-controlled compatibility check, bounded evidence collection, sandboxed
// repair, and fixed-suite verification for version-specific agent adapters.
//
// Ownership boundary: this package sits BETWEEN the frozen T0 contract
// (internal/agent/contract/) and version-specific adapters
// (internal/agent/adapters/). It may NOT modify the contract, common
// AgentEvent model, approval/lifecycle/auth/Recorder/PTY code. It may
// inspect version-specific adapters and fixtures.
//
// Every exported diagnostic string is bounded and redacted via
// contract.SanitizeDiagnostic. No secret, raw path, prompt, or raw JSONL
// content may leave this package in an exported field.
package doctor

import (
	"errors"
	"fmt"

	"devremote/companion-daemon/internal/agent/contract"
)

// ── Compatibility classification ──

// CompatibilityCode is the closed set of compatibility states. Every code
// maps to a single deterministic UI/CLI decision (e.g. "ok" → proceed,
// "version_drift" → offer repair, "unknown_shape" → degraded-only).
type CompatibilityCode string

const (
	CompatOK           CompatibilityCode = "ok"
	CompatVersionDrift CompatibilityCode = "version_drift"
	CompatMissingPath  CompatibilityCode = "missing_path"
	CompatUnknownShape CompatibilityCode = "unknown_shape"
	CompatFieldChange  CompatibilityCode = "field_changed"
)

// ── Compatibility report ──

// CompatibilityReport is the bounded, redacted output of a compatibility
// check. Every string field is bounded and run through SanitizeDiagnostic;
// no secrets, raw paths, or full JSONL content escapes.
type CompatibilityReport struct {
	AdapterName       string
	Provider          string
	ContractVersion   string
	SupportedVersions []string
	ObservedVersion   string
	Compatible        bool
	DriftCode         CompatibilityCode
	DriftSummary      string
	DriftEvidence     DriftEvidence
}

// ── Drift evidence ──

// DriftEvidence is a bounded, redacted evidence bundle safe for display and
// diagnostics. Every slice is capped; every string is sanitized.
type DriftEvidence struct {
	ObservedVersion       string
	AgentVersionSource    string
	ChangedPaths          []string // max 8
	UnknownDiscriminators []string // max 16
	FieldShapeChanges     []string // max 8
	FixtureTestMismatches []string // max 8
}

// ── Observatory (fixed-suite) result ──

// ObsTestFailure records one harness test failure with a bounded reason.
type ObsTestFailure struct {
	TestName string
	Reason   string // bounded, redacted
}

// ObservatoryResult is the summary of running the fixed conformance suite.
// The suite is immutable — no test can be added, skipped, or weakened.
type ObservatoryResult struct {
	AdapterName    string
	TotalTests     int
	Passed         int
	Failed         int
	Failures       []ObsTestFailure // bounded to MaxFailures
	CommandResults []CommandResult  // per-command results, in execution order
}

// CommandResult is the immutable result of one fixed-suite command.
type CommandResult struct {
	Label      string
	Command    string // exact "go ..." command executed
	TotalTests int
	Passed     int
	Failed     int
	Skipped    int
	ExitCode   int
}

// AllPassed reports whether every harness test passed.
func (or ObservatoryResult) AllPassed() bool { return or.Failed == 0 && or.TotalTests > 0 }

// deepCopy returns an ObservatoryResult with all slice fields independently
// allocated. Mutation of the copy cannot affect the original or vice versa.
// Preserves nil vs empty slice distinction for deterministic JSON hashing.
func (or ObservatoryResult) deepCopy() ObservatoryResult {
	cp := or
	if or.Failures != nil {
		cp.Failures = make([]ObsTestFailure, len(or.Failures))
		copy(cp.Failures, or.Failures)
	}
	if or.CommandResults != nil {
		cp.CommandResults = make([]CommandResult, len(or.CommandResults))
		copy(cp.CommandResults, or.CommandResults)
	}
	return cp
}

// ── Redaction result ──

// RedactionResult is the typed outcome of a secret/private-path scan.
// Clean must be true for review to proceed; a non-clean result fails closed
// before any review bundle is created.
type RedactionResult struct {
	Scanned  int      // number of files scanned
	Clean    bool     // true iff no secrets/paths were found
	Findings []string // bounded, redacted summaries (never raw secrets)
}

// ── Suite evidence validation ──

// ValidateSuiteEvidence verifies that the declared command manifest and actual
// command results match exactly — same label set, no duplicates/missing/extra,
// aggregate totals consistent, every result clean. Used at every approval
// boundary (constructor and store) so partial or forged results cannot pass.
func ValidateSuiteEvidence(manifestLabels []string, results []CommandResult, total, passed, failed int) error {
	if total == 0 {
		return fmt.Errorf("suite has zero tests")
	}
	if failed > 0 || passed == 0 {
		return fmt.Errorf("suite not fully passed: %d/%d", passed, total)
	}
	// Check manifest and result label sets match exactly.
	manifestSet := make(map[string]bool, len(manifestLabels))
	for _, l := range manifestLabels {
		if manifestSet[l] {
			return fmt.Errorf("duplicate manifest label %q", l)
		}
		manifestSet[l] = true
	}
	resultSet := make(map[string]bool, len(results))
	for _, cr := range results {
		if resultSet[cr.Label] {
			return fmt.Errorf("duplicate result label %q", cr.Label)
		}
		resultSet[cr.Label] = true
	}
	if len(manifestSet) != len(resultSet) {
		return fmt.Errorf("manifest has %d labels, results have %d", len(manifestSet), len(resultSet))
	}
	for l := range manifestSet {
		if !resultSet[l] {
			return fmt.Errorf("missing result for manifest label %q", l)
		}
	}
	for l := range resultSet {
		if !manifestSet[l] {
			return fmt.Errorf("unexpected result label %q (not in manifest)", l)
		}
	}
	// Aggregate totals must match.
	var sumTotal, sumPassed, sumFailed int
	for _, cr := range results {
		sumTotal += cr.TotalTests
		sumPassed += cr.Passed
		sumFailed += cr.Failed
		if cr.Failed > 0 || cr.Skipped > 0 || cr.ExitCode != 0 {
			return fmt.Errorf("command %q not clean (exit=%d fail=%d skip=%d)",
				cr.Label, cr.ExitCode, cr.Failed, cr.Skipped)
		}
	}
	if sumTotal != total || sumPassed != passed || sumFailed != failed {
		return fmt.Errorf("aggregate mismatch: suite total=%d/%d/%d, commands sum to %d/%d/%d",
			total, passed, failed, sumTotal, sumPassed, sumFailed)
	}
	return nil
}

// ── Provider fixture admission ──

// AdmittedFixture is one explicitly admitted provider fixture. Extension alone
// is insufficient for classification — the user/workflow must declare each
// fixture with provider, version, and source provenance.
type AdmittedFixture struct {
	Path       string // relative path within candidate directory
	Provider   string
	Version    string
	Provenance string // source provenance tier
}

// ── Process request ──

// ProcessRequest is the input to a Doctor repair workflow. It describes which
// adapter to check and carries the observed evidence and fixtures.
type ProcessRequest struct {
	AdapterDescriptor     contract.AgentAdapterDescriptor
	ObservedVersion       string
	ObservedVersionSource string
	ObservedRecordSamples []contract.RawRecord
	ConformanceFixtures   contract.ConformanceFixtures
	AdmittedFixtures      []AdmittedFixture // explicitly admitted provider fixtures
}

// ── Repair plan ──

// Patch describes one proposed file change within the repair sandbox.
type Patch struct {
	FilePath string // relative to repo root, must be in write allowlist
	OldHash  string // hex content-hash of current file (rollback anchor)
	NewHash  string // hex content-hash of proposed file
	Diff     string // unified diff (bounded)
}

// RepairPlan is the output of a repair workflow, presented for user review
// before any activation. No change is applied until the user explicitly
// approves and the caller activates the plan.
type RepairPlan struct {
	AdapterName          string
	AdapterPath          string
	CurrentVersion       string
	TargetVersion        string
	CompatibilityReport  CompatibilityReport
	PreRepairSuiteResult ObservatoryResult
	Patches              []Patch
	NewFixtures          []string // paths to new fixture files (relative)
	RollbackTarget       string   // prior adapter version hash
	RemainingUnknowns    []string // bounded, redacted
}

// ── Doctor ──

// Doctor orchestrates the D1 repair workflow. It holds no mutable state —
// every method receives its dependencies as parameters so concurrent checks
// are safe and the Doctor is testable without setup/teardown.
type Doctor struct{}

// New returns a new Doctor.
func New() *Doctor { return &Doctor{} }

// ── Session ──

// Session is the local-only workflow controller handle returned by
// FullWorkflow. It retains the orchestrator, review bundle, and approval
// state. Never expose through a remote tunnel.
type Session struct {
	orch   *Orchestrator
	Bundle *ReviewBundle
}

func (s *Session) Approve(userDigest string) error {
	return s.orch.Approve(s.Bundle.RequestID(), userDigest)
}
func (s *Session) Reject() error {
	return s.orch.Reject(s.Bundle.RequestID())
}
func (s *Session) Activate(userDigest string) error {
	return s.orch.Activate(s.Bundle.RequestID(), userDigest)
}
func (s *Session) GetRequest() (*ApprovalRequest, bool) {
	return s.orch.GetApprovalRequest(s.Bundle.RequestID())
}
func (s *Session) Close() { s.orch.Reset() }

// ── FullWorkflow ──

// FullWorkflow is the public production entry. Production repair runner
// is not available — this always returns ErrRunnerUnavailable.
// Use fullWorkflowTest for internal testing with a stub runner.
func FullWorkflow(
	runner RepairRunner,
	desc contract.AgentAdapterDescriptor,
	observedVersion, versionSource string,
	samples []contract.RawRecord,
	knownDiscriminators []string,
	knownFields map[string]string,
	requestID, baselineSHA string,
	repoRoot string,
) (*Session, error) {
	if runner != nil {
		return nil, ErrRunnerUnavailable
	}
	return nil, ErrRunnerUnavailable
}

// fullWorkflowTest is the test-only entry that accepts a runner.
func fullWorkflowTest(
	runner RepairRunner,
	desc contract.AgentAdapterDescriptor,
	observedVersion, versionSource string,
	samples []contract.RawRecord,
	knownDiscriminators []string,
	knownFields map[string]string,
	requestID, baselineSHA string,
	repoRoot string,
) (*Session, error) {
	orch := NewOrchestrator(runner, repoRoot)
	req := ProcessRequest{
		AdapterDescriptor:     desc,
		ObservedVersion:       observedVersion,
		ObservedVersionSource: versionSource,
		ObservedRecordSamples: samples,
	}
	report, err := orch.DetectDrift(req)
	if err != nil {
		orch.Reset()
		return nil, err
	}
	if report.Compatible {
		orch.Reset()
		return nil, nil
	}
	if _, err := orch.CollectEvidence(knownDiscriminators, knownFields); err != nil {
		orch.Reset()
		return nil, err
	}
	if err := orch.RequestRepair(); err != nil {
		orch.Reset()
		return nil, err
	}
	if orch.activeRepairOutput == nil || !orch.activeRepairOutput.Success {
		orch.Reset()
		return nil, errors.New("runner did not produce successful output")
	}
	if _, err := orch.SubmitPatch(orch.activeRepairOutput.Patch); err != nil {
		orch.Reset()
		return nil, err
	}
	if _, err := orch.ApplyPatchToWorkspace(); err != nil {
		orch.Reset()
		return nil, err
	}
	if _, err := orch.RunFixedSuites(); err != nil {
		orch.Reset()
		return nil, err
	}
	bundle, err := orch.BuildReviewBundle(requestID, baselineSHA)
	if err != nil {
		if orch.activeSuite != nil {
			err = fmt.Errorf("%w; suite %d/%d", err, orch.activeSuite.Passed, orch.activeSuite.TotalTests)
		}
		orch.Reset()
		return nil, err
	}
	return &Session{orch: orch, Bundle: bundle}, nil
}
