package doctor

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"devremote/companion-daemon/internal/agent/contract"
)

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

type RepairRunner interface {
	Run(input RepairInput) (RepairOutput, error)
}

type RepairInput struct {
	AdapterName    string
	Provider       string
	CurrentVersion string
	TargetVersion  string
	DriftEvidence  DriftEvidence
}

type RepairOutput struct {
	Patch       []byte
	Diagnostics []string
	Success     bool
}

// ── Orchestrator ──

type Orchestrator struct {
	mu    sync.Mutex
	state OrchestratorState

	provider      string
	targetVersion string // raw observed version
	targetDir     string // canonical directory name
	currentDesc   contract.AgentAdapterDescriptor

	sandbox  *Sandbox
	suite    *FixedSuite
	approval *ApprovalStore
	evidence *EvidenceCollector

	activeRequest      *ProcessRequest
	activeReport       *CompatibilityReport
	activeEvidence     *DriftEvidence
	activeRepairOutput *RepairOutput
	activeOps          []PatchOperation
	activePatchBytes   []byte // original immutable patch bytes from runner
	activeSuite        *ObservatoryResult
	activeBundle       *ReviewBundle
	activeRunner       RepairRunner
	activeWorkspace    *Workspace
	repoRoot           string
}

func NewOrchestrator(runner RepairRunner, repoRoot string) *Orchestrator {
	return &Orchestrator{
		state:        StateIdle,
		suite:        NewFixedSuite(),
		approval:     NewApprovalStore(),
		evidence:     NewEvidenceCollector(),
		activeRunner: runner,
		repoRoot:     repoRoot,
	}
}

func (o *Orchestrator) State() OrchestratorState {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.state
}

// ── Workflow ──

func (o *Orchestrator) DetectDrift(req ProcessRequest) (*CompatibilityReport, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.state != StateIdle {
		return nil, fmt.Errorf("not idle: %s", o.state)
	}
	d := New()
	report := d.CheckCompatibility(req.AdapterDescriptor, req.ObservedVersion, req.ObservedVersionSource)
	if report.Compatible {
		o.state = StateDetected
		return &report, nil
	}

	// Closed vocabulary check.
	if err := ValidateProvider(report.AdapterName); err != nil {
		return nil, err
	}
	dir, err := CanonicalVersion(report.ObservedVersion)
	if err != nil {
		return nil, err
	}

	sb, err := NewSandbox(o.repoRoot, report.AdapterName, report.ObservedVersion)
	if err != nil {
		return nil, err
	}

	o.provider = report.AdapterName
	o.targetVersion = report.ObservedVersion
	o.targetDir = dir
	o.currentDesc = req.AdapterDescriptor
	o.activeRequest = &req
	o.activeReport = &report
	o.sandbox = sb
	o.state = StateDetected
	return &report, nil
}

func (o *Orchestrator) CollectEvidence(knownDiscriminators []string, knownFields map[string]string) (*DriftEvidence, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.state != StateDetected && o.state != StateCollectingEvidence {
		return nil, fmt.Errorf("not in detected: %s", o.state)
	}
	if o.activeReport == nil {
		return nil, errors.New("no report")
	}
	o.state = StateCollectingEvidence
	o.evidence.CollectDrift(o.activeReport, o.activeRequest.ObservedRecordSamples, knownDiscriminators, knownFields)
	o.activeEvidence = &o.activeReport.DriftEvidence
	return o.activeEvidence, nil
}

func (o *Orchestrator) RequestRepair() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.state != StateCollectingEvidence {
		return fmt.Errorf("not in collecting_evidence: %s", o.state)
	}
	if o.activeRunner == nil {
		return ErrRunnerUnavailable
	}
	if o.activeEvidence == nil {
		return errors.New("no evidence")
	}

	o.state = StateRepairing
	curVer := ""
	if len(o.currentDesc.SupportedVersions) > 0 {
		curVer = o.currentDesc.SupportedVersions[0]
	}
	input := RepairInput{
		AdapterName:    o.provider,
		Provider:       o.activeReport.Provider,
		CurrentVersion: curVer,
		TargetVersion:  o.targetVersion,
		DriftEvidence:  *o.activeEvidence,
	}
	output, err := o.activeRunner.Run(input)
	if err != nil {
		return fmt.Errorf("runner failed: %w", err)
	}
	o.activeRepairOutput = &output
	return nil
}

func (o *Orchestrator) SubmitPatch(patch []byte) ([]PatchOperation, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.state != StateRepairing {
		return nil, fmt.Errorf("not in repairing: %s", o.state)
	}
	if o.sandbox == nil {
		return nil, errors.New("no sandbox")
	}

	ops, err := ParsePatch(patch)
	if err != nil {
		return nil, err
	}
	if err := o.sandbox.Validate(ops); err != nil {
		return nil, err
	}
	o.state = StateValidatingPatch
	o.activeOps = ops
	o.activePatchBytes = make([]byte, len(patch))
	copy(o.activePatchBytes, patch)
	return ops, nil
}

func (o *Orchestrator) ApplyPatchToWorkspace() (*Workspace, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.state != StateValidatingPatch {
		return nil, fmt.Errorf("not in validating_patch: %s", o.state)
	}
	if len(o.activeOps) == 0 {
		return nil, errors.New("no patch to apply")
	}
	if o.sandbox == nil {
		return nil, errors.New("no sandbox")
	}

	ws, err := PrepareWorkspace(o.repoRoot, o.activeOps, o.provider, o.targetDir)
	if err != nil {
		return nil, err
	}
	o.activeWorkspace = ws
	return ws, nil
}

func (o *Orchestrator) RunFixedSuites() (*ObservatoryResult, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.state != StateValidatingPatch && o.state != StateRunningFixedSuites {
		return nil, fmt.Errorf("not ready for suite: %s", o.state)
	}
	if o.activeWorkspace == nil {
		return nil, errors.New("no workspace")
	}
	o.state = StateRunningFixedSuites
	runner := NewSuiteRunner()
	result := runner.RunInWorkspace(o.activeWorkspace.Root, o.activeWorkspace.CandidatePkg)
	o.activeSuite = &result
	return &result, nil
}

func (o *Orchestrator) BuildReviewBundle(requestID, baselineSHA string) (*ReviewBundle, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.state != StateRunningFixedSuites {
		return nil, fmt.Errorf("not in running_fixed_suites: %s", o.state)
	}
	if o.activeReport == nil || o.activeSuite == nil {
		return nil, errors.New("missing state")
	}
	if !o.activeSuite.AllPassed() {
		return nil, fmt.Errorf("suite not passed: %d/%d", o.activeSuite.Passed, o.activeSuite.TotalTests)
	}

	// Verify baseline SHA against actual git HEAD.
	actualSHA, err := gitHeadSHA(o.repoRoot)
	if err != nil {
		return nil, fmt.Errorf("cannot verify baseline SHA: %w", err)
	}
	if baselineSHA != "" && actualSHA != baselineSHA {
		return nil, fmt.Errorf("baseline SHA mismatch: expected %s, actual %s", baselineSHA, actualSHA)
	}

	evidenceDigest := HashJSON(o.activeEvidence)
	patchDigest := HashBytes(o.activePatchBytes) // original immutable patch bytes

	var files []string
	for _, op := range o.activeOps {
		files = append(files, op.DiffPath)
	}

	var cmdLabels []string
	for _, c := range NewFixedSuite().Commands(o.activeWorkspace.CandidatePkg) {
		cmdLabels = append(cmdLabels, c.Label)
	}

	evidenceProv := string(contract.ProvenanceNativeLog)
	wsDigest := o.activeWorkspace.Digest()
	driftEv := DriftEvidence{}
	if o.activeEvidence != nil {
		driftEv = *o.activeEvidence
	}
	var unknowns []string
	for _, d := range o.activeReport.DriftEvidence.UnknownDiscriminators {
		unknowns = append(unknowns, d)
	}
	for _, f := range o.activeReport.DriftEvidence.FieldShapeChanges {
		unknowns = append(unknowns, f)
	}

	bundle, err := NewReviewBundle(requestID, o.provider, o.activeReport.Provider, o.targetVersion,
		baselineSHA, evidenceDigest, patchDigest,
		o.activePatchBytes,     // unified diff
		files,                  // changed files
		nil,                    // fixture manifest
		"native_log, redacted", // fixture redaction
		cmdLabels,              // suite command manifest
		*o.activeSuite,         // suite result
		driftEv,                // drift evidence
		evidenceProv,           // evidence provenance
		wsDigest,               // workspace digest
		unknowns,               // remaining unknowns
	)
	if err != nil {
		return nil, err
	}
	if _, err := o.approval.Submit(bundle); err != nil {
		return nil, err
	}
	o.activeBundle = bundle
	o.state = StateAwaitingApproval
	return bundle, nil
}

func (o *Orchestrator) Approve(requestID, userDigest string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.approval.Approve(requestID, userDigest)
}

func (o *Orchestrator) Reject(requestID string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.approval.Reject(requestID)
}

func (o *Orchestrator) Activate(requestID, userDigest string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.approval.Activate(requestID, userDigest)
}

func (o *Orchestrator) GetApprovalRequest(id string) (*ApprovalRequest, bool) {
	return o.approval.Get(id)
}

func (o *Orchestrator) Bundle() *ReviewBundle {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.activeBundle
}

func (o *Orchestrator) Reset() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.activeWorkspace != nil {
		o.activeWorkspace.Cleanup()
	}
	o.state = StateIdle
	o.provider = ""
	o.targetVersion = ""
	o.targetDir = ""
	o.activeRequest = nil
	o.activeReport = nil
	o.activeEvidence = nil
	o.activeRepairOutput = nil
	o.activeOps = nil
	o.activeSuite = nil
	o.activeBundle = nil
	o.activeWorkspace = nil
	o.sandbox = nil
}

// ── Stub runner ──

var ErrRunnerUnavailable = errors.New("runner_unavailable — no safe production runner configured")

type StubRepairRunner struct {
	Patch         []byte
	Success       bool
	Diags         []string
	FailErr       error
	CapturedInput *RepairInput
}

func (s *StubRepairRunner) Run(input RepairInput) (RepairOutput, error) {
	s.CapturedInput = &input
	if s.FailErr != nil {
		return RepairOutput{}, s.FailErr
	}
	return RepairOutput{Patch: s.Patch, Diagnostics: s.Diags, Success: s.Success}, nil
}

func gitHeadSHA(repoRoot string) (string, error) {
	cmd := exec.Command("git", "-C", repoRoot, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
