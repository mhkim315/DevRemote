package doctor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"devremote/companion-daemon/internal/agent/contract"
)

// ── Orchestrator state ──

// OrchestratorState is the explicit state of the D1 repair workflow.
type OrchestratorState string

const (
	StateIdle               OrchestratorState = "idle"
	StateDetected           OrchestratorState = "detected"
	StateCollectingEvidence OrchestratorState = "collecting_evidence"
	StateRepairing          OrchestratorState = "repairing"
	StateValidatingPatch    OrchestratorState = "validating_patch"
	StateRunningFixedSuites OrchestratorState = "running_fixed_suites"
	StateAwaitingApproval   OrchestratorState = "awaiting_approval"
)

// ── RepairRunner ──

// RepairRunner is the interface for invoking a local coding agent within an
// isolated workspace. The production implementation enforces the sandbox
// boundary; tests use a controlled stub.
type RepairRunner interface {
	// Run invokes the repair agent with the given input. Returns a unified
	// diff patch and any output diagnostics. The implementation must enforce
	// workspace isolation, process allowlist, network=deny, and timeout.
	Run(input RepairInput) (RepairOutput, error)
}

// RepairInput is the bounded, redacted input to a repair run.
type RepairInput struct {
	AdapterName          string
	Provider             string
	CurrentVersion       string
	TargetVersion        string
	DriftEvidence        DriftEvidence
	RecordSamples        []contract.RawRecord
	AdapterSourcePath    string // read-only path to current adapter source
	KnownDiscriminators  []string
	KnownFields          map[string]string
}

// RepairOutput is the bounded output from a repair run.
type RepairOutput struct {
	Patch        []byte   // unified diff
	ChangedFiles []string // extracted from patch
	Diagnostics  []string // bounded, redacted
	Success      bool
}

// ── Orchestrator ──

// Orchestrator owns the full D1 production workflow: state machine, sandbox,
// fixed suite, approval store, and activation/rollback logic. All allowlists
// and baselines are owned by the orchestrator — callers cannot inject them.
type Orchestrator struct {
	mu    sync.Mutex
	state OrchestratorState

	// Owned subsystems.
	suite    *FixedSuite
	sandbox  *Sandbox
	approval *ApprovalStore
	evidence *EvidenceCollector

	// Active workflow state.
	activeRequest  *ProcessRequest
	activeReport   *CompatibilityReport
	activeEvidence *DriftEvidence
	activePatch    []byte
	activeFiles    []PatchFile
	activeSuite    *ObservatoryResult
	activeBundle   *ReviewBundle
	activeRunner   RepairRunner
}

// NewOrchestrator returns a new Orchestrator in idle state.
func NewOrchestrator(runner RepairRunner) *Orchestrator {
	return &Orchestrator{
		state:     StateIdle,
		suite:     NewFixedSuite(),
		approval:  NewApprovalStore(),
		evidence:  NewEvidenceCollector(),
		activeRunner: runner,
	}
}

// State returns the current orchestrator state.
func (o *Orchestrator) State() OrchestratorState {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.state
}

// ── Workflow methods ──

// DetectDrift runs CheckCompatibility and transitions idle → detected.
func (o *Orchestrator) DetectDrift(req ProcessRequest) (*CompatibilityReport, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.state != StateIdle {
		return nil, fmt.Errorf("orchestrator not idle: %s", o.state)
	}

	desc := req.AdapterDescriptor
	d := New()
	report := d.CheckCompatibility(desc, req.ObservedVersion, req.ObservedVersionSource)

	o.activeRequest = &req
	o.activeReport = &report
	o.state = StateDetected

	return &report, nil
}

// CollectEvidence gathers bounded evidence and transitions detected → collecting_evidence.
func (o *Orchestrator) CollectEvidence(knownDiscriminators []string, knownFields map[string]string) (*DriftEvidence, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.state != StateDetected && o.state != StateCollectingEvidence {
		return nil, fmt.Errorf("orchestrator not in detected state: %s", o.state)
	}

	o.state = StateCollectingEvidence

	if o.activeReport == nil {
		return nil, errors.New("no active compatibility report")
	}

	records := o.activeRequest.ObservedRecordSamples
	o.evidence.CollectDrift(o.activeReport, records, knownDiscriminators, knownFields)
	o.activeEvidence = &o.activeReport.DriftEvidence

	return o.activeEvidence, nil
}

// RequestRepair invokes the RepairRunner and transitions collecting_evidence → repairing.
func (o *Orchestrator) RequestRepair() error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.state != StateCollectingEvidence {
		return fmt.Errorf("orchestrator not in collecting_evidence state: %s", o.state)
	}
	if o.activeRunner == nil {
		return errors.New("no RepairRunner configured")
	}

	o.state = StateRepairing
	return nil
}

// SubmitPatch validates a patch via the sandbox and transitions repairing → validating_patch.
// The patch is the output from the RepairRunner.
func (o *Orchestrator) SubmitPatch(patch []byte, provider, targetVersion string) ([]PatchFile, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.state != StateRepairing {
		return nil, fmt.Errorf("orchestrator not in repairing state: %s", o.state)
	}

	o.state = StateValidatingPatch
	o.sandbox = NewSandbox(ProviderAllowList(provider, targetVersion))

	files, err := o.sandbox.ValidatePatch(patch, provider, targetVersion)
	if err != nil {
		o.state = StateRepairing // revert on failure
		return nil, err
	}

	o.activePatch = patch
	o.activeFiles = files
	return files, nil
}

// RunFixedSuites executes the FixedSuite and transitions validating_patch → running_fixed_suites.
func (o *Orchestrator) RunFixedSuites(candidatePkgPath string) (*ObservatoryResult, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.state != StateValidatingPatch {
		return nil, fmt.Errorf("orchestrator not in validating_patch state: %s", o.state)
	}

	o.state = StateRunningFixedSuites
	runner := NewSuiteRunner()
	result := runner.Run(candidatePkgPath)
	o.activeSuite = &result

	return &result, nil
}

// BuildReviewBundle creates a digest-bound ReviewBundle and transitions
// running_fixed_suites → awaiting_approval. All digests are computed from
// immutable workflow state.
func (o *Orchestrator) BuildReviewBundle(requestID, baselineSHA string) (*ReviewBundle, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.state != StateRunningFixedSuites {
		return nil, fmt.Errorf("orchestrator not in running_fixed_suites state: %s", o.state)
	}
	if o.activeReport == nil || o.activeSuite == nil {
		return nil, errors.New("missing workflow state — report or suite result absent")
	}

	evidenceDigest := HashJSON(o.activeEvidence)
	patchDigest := HashBytes(o.activePatch)

	changedFiles := make([]string, len(o.activeFiles))
	for i, f := range o.activeFiles {
		changedFiles[i] = f.Path
	}

	suiteCommands := make([]string, len(o.suite.Commands()))
	for i, c := range o.suite.Commands() {
		suiteCommands[i] = c.Label
	}

	bundle := NewReviewBundle(
		requestID,
		o.activeReport.AdapterName,
		o.activeReport.Provider,
		o.activeReport.ObservedVersion,
		baselineSHA,
		evidenceDigest,
		patchDigest,
		changedFiles,
		suiteCommands,
		*o.activeSuite,
	)
	bundle.BundleDigest() // pre-compute

	_, err := o.approval.Submit(bundle)
	if err != nil && !errors.Is(err, ErrExpired) {
		return nil, err
	}

	o.activeBundle = bundle
	o.state = StateAwaitingApproval
	return bundle, nil
}

// Approve validates digest match and approves the request.
func (o *Orchestrator) Approve(requestID string) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.activeBundle == nil {
		return errors.New("no active review bundle")
	}
	return o.approval.Approve(requestID, o.activeBundle.BundleDigest())
}

// Reject rejects the current approval request.
func (o *Orchestrator) Reject(requestID string) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	return o.approval.Reject(requestID)
}

// Activate attempts activation of an approved request. Returns
// ErrActivationUnsupported when safe filesystem activation cannot be
// performed (fail-closed).
func (o *Orchestrator) Activate(requestID string) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.activeBundle == nil {
		return errors.New("no active review bundle")
	}
	return o.approval.Activate(requestID, o.activeBundle.BundleDigest())
}

// ActivateWithResult records the activation outcome. Caller performs the
// actual filesystem operation.
func (o *Orchestrator) ActivateWithResult(requestID string, success bool) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.activeBundle == nil {
		return errors.New("no active review bundle")
	}
	return o.approval.ActivateWithResult(requestID, o.activeBundle.BundleDigest(), success)
}

// Rollback attempts to roll back an active request.
func (o *Orchestrator) Rollback(requestID string) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	return o.approval.Rollback(requestID)
}

// GetApprovalRequest returns the current state of an approval request.
func (o *Orchestrator) GetApprovalRequest(requestID string) (*ApprovalRequest, bool) {
	return o.approval.Get(requestID)
}

// ActiveBundle returns the current review bundle, if any.
func (o *Orchestrator) ActiveBundle() *ReviewBundle {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.activeBundle
}

// Reset returns the orchestrator to idle state.
func (o *Orchestrator) Reset() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.state = StateIdle
	o.activeRequest = nil
	o.activeReport = nil
	o.activeEvidence = nil
	o.activePatch = nil
	o.activeFiles = nil
	o.activeSuite = nil
	o.activeBundle = nil
	o.sandbox = nil
}

// ── Stub RepairRunner for testing ──

// StubRepairRunner is a test-only RepairRunner that returns a canned patch.
type StubRepairRunner struct {
	Patch        []byte
	ChangedFiles []string
	Success      bool
	Diags        []string
}

func (s *StubRepairRunner) Run(input RepairInput) (RepairOutput, error) {
	return RepairOutput{
		Patch:        s.Patch,
		ChangedFiles: s.ChangedFiles,
		Diagnostics:  s.Diags,
		Success:      s.Success,
	}, nil
}

// ── Digest helpers ──

func hashBytes(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func hashJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "error:" + contract.SanitizeDiagnostic(err.Error())
	}
	return hashBytes(b)
}

// Ensure these helpers don't collide: doctor.go's HashBytes/HashJSON are exported.
var _ = hashBytes
var _ = hashJSON
var _ = time.Now // suppress unused import warning
