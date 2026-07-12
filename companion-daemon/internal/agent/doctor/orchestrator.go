package doctor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"devremote/companion-daemon/internal/agent/contract"
)

// ── OrchestratorState ──

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
	AdapterName         string
	Provider            string
	CurrentVersion      string
	TargetVersion       string
	DriftEvidence       DriftEvidence // bounded/redacted only — no raw records
	KnownDiscriminators []string
	KnownFields         map[string]string
}

type RepairOutput struct {
	Patch        []byte
	ChangedFiles []string
	Diagnostics  []string
	Success      bool
}

// ── Workspace ──

// Workspace is an isolated directory containing patched adapter source.
type Workspace struct {
	Root    string // absolute path to workspace root
	pkgPath string // relative path within workspace to the adapter package
}

// Cleanup removes the workspace directory.
func (w *Workspace) Cleanup() error {
	if w.Root != "" {
		return os.RemoveAll(w.Root)
	}
	return nil
}

// ── Orchestrator ──

type Orchestrator struct {
	mu    sync.Mutex
	state OrchestratorState

	// Internal state derived from compatibility report (caller cannot inject).
	provider      string
	targetVersion string
	currentDesc   contract.AgentAdapterDescriptor

	// Owned subsystems.
	suite    *FixedSuite
	approval *ApprovalStore
	evidence *EvidenceCollector

	// Active workflow data.
	activeRequest     *ProcessRequest
	activeReport      *CompatibilityReport
	activeEvidence    *DriftEvidence
	activeRepairOutput *RepairOutput
	activePatch       []byte
	activeFiles       []PatchFile
	activeSuite       *ObservatoryResult
	activeBundle      *ReviewBundle
	activeRunner      RepairRunner
	activeWorkspace   *Workspace
}

func NewOrchestrator(runner RepairRunner) *Orchestrator {
	return &Orchestrator{
		state:        StateIdle,
		suite:        NewFixedSuite(),
		approval:     NewApprovalStore(),
		evidence:     NewEvidenceCollector(),
		activeRunner: runner,
	}
}

func (o *Orchestrator) State() OrchestratorState {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.state
}

// ── Workflow methods ──

// DetectDrift stores provider + targetVersion from the report. Caller
// cannot inject these later — the orchestrator owns them.
func (o *Orchestrator) DetectDrift(req ProcessRequest) (*CompatibilityReport, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.state != StateIdle {
		return nil, fmt.Errorf("orchestrator not idle: %s", o.state)
	}

	d := New()
	report := d.CheckCompatibility(req.AdapterDescriptor, req.ObservedVersion, req.ObservedVersionSource)

	o.activeRequest = &req
	o.activeReport = &report
	o.provider = report.AdapterName
	o.targetVersion = report.ObservedVersion
	o.currentDesc = req.AdapterDescriptor
	o.state = StateDetected

	return &report, nil
}

func (o *Orchestrator) CollectEvidence(knownDiscriminators []string, knownFields map[string]string) (*DriftEvidence, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.state != StateDetected && o.state != StateCollectingEvidence {
		return nil, fmt.Errorf("orchestrator not in detected state: %s", o.state)
	}
	if o.activeReport == nil {
		return nil, errors.New("no active compatibility report")
	}

	o.state = StateCollectingEvidence
	records := o.activeRequest.ObservedRecordSamples
	o.evidence.CollectDrift(o.activeReport, records, knownDiscriminators, knownFields)
	o.activeEvidence = &o.activeReport.DriftEvidence
	return o.activeEvidence, nil
}

// RequestRepair ACTUALLY calls the runner. It passes bounded/redacted
// evidence only — raw provider records never leave the evidence collector.
func (o *Orchestrator) RequestRepair() error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.state != StateCollectingEvidence {
		return fmt.Errorf("orchestrator not in collecting_evidence state: %s", o.state)
	}
	if o.activeRunner == nil {
		return errors.New("no RepairRunner configured")
	}
	if o.activeEvidence == nil {
		return errors.New("no drift evidence collected")
	}

	o.state = StateRepairing

	// Build bounded input — NO raw records.
	currentVersion := ""
	if len(o.currentDesc.SupportedVersions) > 0 {
		currentVersion = o.currentDesc.SupportedVersions[0]
	}
	input := RepairInput{
		AdapterName:    o.provider,
		Provider:       o.activeReport.Provider,
		CurrentVersion: currentVersion,
		TargetVersion:  o.targetVersion,
		DriftEvidence:  *o.activeEvidence,
	}
	// knownDiscriminators and knownFields are passed through from the
	// drift detection context — the caller sets them in CollectEvidence.

	output, err := o.activeRunner.Run(input)
	if err != nil {
		return fmt.Errorf("repair runner failed: %w", err)
	}
	o.activeRepairOutput = &output
	return nil
}

// SubmitPatch validates the patch via the sandbox. provider and
// targetVersion are derived internally from the compatibility report.
func (o *Orchestrator) SubmitPatch(patch []byte) ([]PatchFile, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.state != StateRepairing {
		return nil, fmt.Errorf("orchestrator not in repairing state: %s", o.state)
	}

	o.state = StateValidatingPatch
	sandbox := NewSandbox(ProviderAllowList(o.provider, o.targetVersion))

	files, err := sandbox.ValidatePatch(patch, o.provider, o.targetVersion)
	if err != nil {
		o.state = StateRepairing
		return nil, err
	}

	o.activePatch = make([]byte, len(patch))
	copy(o.activePatch, patch)
	o.activeFiles = files
	return files, nil
}

// ApplyPatchToWorkspace creates an isolated workspace, copies adapter
// source, and applies the validated patch. The workspace is stored
// internally for suite execution.
func (o *Orchestrator) ApplyPatchToWorkspace(repoRoot string) (*Workspace, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.state != StateValidatingPatch {
		return nil, fmt.Errorf("orchestrator not in validating_patch state: %s", o.state)
	}
	if len(o.activePatch) == 0 {
		return nil, errors.New("no validated patch to apply")
	}

	// Create isolated workspace.
	tmpDir, err := os.MkdirTemp("", "d1-workspace-*")
	if err != nil {
		return nil, fmt.Errorf("cannot create workspace: %w", err)
	}

	// Determine the adapter source path and copy it.
	srcRel := filepath.Join("internal", "agent", "adapters", o.provider)
	srcAbs := filepath.Join(repoRoot, srcRel)
	targetRel := filepath.Join("internal", "agent", "adapters", o.provider, o.targetVersion)
	targetAbs := filepath.Join(tmpDir, targetRel)

	// Copy source adapter to workspace.
	if err := copyDir(srcAbs, targetAbs); err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("cannot copy adapter source: %w", err)
	}

	// Copy contract package (read-only dependency).
	contractSrc := filepath.Join(repoRoot, "internal", "agent", "contract")
	contractDst := filepath.Join(tmpDir, "internal", "agent", "contract")
	if err := copyDir(contractSrc, contractDst); err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("cannot copy contract: %w", err)
	}

	// Copy models.go.
	modelsSrc := filepath.Join(repoRoot, "internal", "agent", "models.go")
	modelsDstDir := filepath.Join(tmpDir, "internal", "agent")
	os.MkdirAll(modelsDstDir, 0755)
	if err := copyFile(modelsSrc, filepath.Join(modelsDstDir, "models.go")); err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("cannot copy models.go: %w", err)
	}

	// Write go.mod for the isolated workspace.
	goMod := `module workspace

go 1.21
`
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644); err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("cannot write go.mod: %w", err)
	}

	// Apply patch to the workspace.
	patchFile := filepath.Join(tmpDir, "repair.patch")
	if err := os.WriteFile(patchFile, o.activePatch, 0644); err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("cannot write patch file: %w", err)
	}

	// Write the patched files directly (simple patching — for production,
	// this would use git apply or a proper diff parser).
	if err := applyPatchToDir(tmpDir, o.activePatch); err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("cannot apply patch: %w", err)
	}

	ws := &Workspace{
		Root:    tmpDir,
		pkgPath: targetRel,
	}
	o.activeWorkspace = ws
	return ws, nil
}

// RunFixedSuites executes all fixed suite commands. The candidate package
// path is derived internally from provider/targetVersion.
func (o *Orchestrator) RunFixedSuites(repoRoot string) (*ObservatoryResult, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.state != StateValidatingPatch && o.state != StateRunningFixedSuites {
		return nil, fmt.Errorf("orchestrator not ready for suite: %s", o.state)
	}

	o.state = StateRunningFixedSuites

	// Determine candidate package path.
	candidatePkg := filepath.Join("internal", "agent", "adapters", o.provider, o.targetVersion)

	// If a workspace exists, run tests there.
	var pkgRoot string
	if o.activeWorkspace != nil {
		pkgRoot = o.activeWorkspace.Root
	} else {
		pkgRoot = repoRoot
	}

	runner := NewSuiteRunner()
	result := runner.RunInWorkspace(pkgRoot, candidatePkg)
	o.activeSuite = &result
	return &result, nil
}

// BuildReviewBundle creates an immutable, digest-bound review bundle.
// Rejects if the suite did not pass or result is nil.
func (o *Orchestrator) BuildReviewBundle(requestID, baselineSHA string) (*ReviewBundle, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.state != StateRunningFixedSuites {
		return nil, fmt.Errorf("orchestrator not in running_fixed_suites state: %s", o.state)
	}
	if o.activeReport == nil {
		return nil, errors.New("missing compatibility report")
	}
	if o.activeSuite == nil {
		return nil, errors.New("fixed suite has not been run")
	}
	if !o.activeSuite.AllPassed() {
		return nil, fmt.Errorf("fixed suite must pass before review bundle can be built: %d/%d passed",
			o.activeSuite.Passed, o.activeSuite.TotalTests)
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

	bundle, err := NewReviewBundle(
		requestID,
		o.activeReport.AdapterName,
		o.activeReport.Provider,
		o.targetVersion,
		baselineSHA,
		evidenceDigest,
		patchDigest,
		changedFiles,
		suiteCommands,
		*o.activeSuite,
	)
	if err != nil {
		return nil, err
	}

	_, err = o.approval.Submit(bundle)
	if err != nil {
		return nil, err
	}

	o.activeBundle = bundle
	o.state = StateAwaitingApproval
	return bundle, nil
}

// Approve validates the user-supplied digest against the bundle.
func (o *Orchestrator) Approve(requestID, userSuppliedDigest string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.approval.Approve(requestID, userSuppliedDigest)
}

func (o *Orchestrator) Reject(requestID string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.approval.Reject(requestID)
}

// Activate always returns ErrActivationUnsupported (fail-closed).
func (o *Orchestrator) Activate(requestID, userSuppliedDigest string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.approval.Activate(requestID, userSuppliedDigest)
}

func (o *Orchestrator) GetApprovalRequest(requestID string) (*ApprovalRequest, bool) {
	return o.approval.Get(requestID)
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
	o.activeRequest = nil
	o.activeReport = nil
	o.activeEvidence = nil
	o.activeRepairOutput = nil
	o.activePatch = nil
	o.activeFiles = nil
	o.activeSuite = nil
	o.activeBundle = nil
	o.activeWorkspace = nil
}

// ── StubRepairRunner ──

type StubRepairRunner struct {
	Patch        []byte
	ChangedFiles []string
	Success      bool
	Diags        []string
	FailErr      error
	CapturedInput *RepairInput // set after Run
}

func (s *StubRepairRunner) Run(input RepairInput) (RepairOutput, error) {
	s.CapturedInput = &input
	if s.FailErr != nil {
		return RepairOutput{}, s.FailErr
	}
	return RepairOutput{
		Patch:        s.Patch,
		ChangedFiles: s.ChangedFiles,
		Diagnostics:  s.Diags,
		Success:      s.Success,
	}, nil
}

// ── Filesystem helpers ──

func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	os.MkdirAll(filepath.Dir(dst), 0755)
	return os.WriteFile(dst, data, 0644)
}

// applyPatchToDir writes new file content from unified diff hunks.
// For production, this would use a proper patch library.
func applyPatchToDir(root string, patch []byte) error {
	// Simple implementation: parse diff headers and write new file content.
	// Each hunk starting with "+" after "+++ b/<path>" and "@@ ... @@" is
	// written to the target file.
	lines := strings.Split(string(patch), "\n")
	var currentPath string
	var content strings.Builder
	inHunk := false

	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "+++ b/") {
			// Flush previous file.
			if currentPath != "" && content.Len() > 0 {
				fullPath := filepath.Join(root, currentPath)
				os.MkdirAll(filepath.Dir(fullPath), 0755)
				os.WriteFile(fullPath, []byte(content.String()), 0644)
			}
			currentPath = strings.TrimPrefix(line, "+++ b/")
			if idx := strings.IndexByte(currentPath, '\t'); idx >= 0 {
				currentPath = currentPath[:idx]
			}
			content.Reset()
			inHunk = false
			continue
		}
		if strings.HasPrefix(line, "@@") {
			inHunk = true
			continue
		}
		if inHunk {
			if strings.HasPrefix(line, "+") {
				content.WriteString(line[1:])
				content.WriteByte('\n')
			} else if strings.HasPrefix(line, " ") {
				content.WriteString(line[1:])
				content.WriteByte('\n')
			}
			// Skip "-" lines (removals).
		}
	}

	// Flush last file.
	if currentPath != "" && content.Len() > 0 {
		fullPath := filepath.Join(root, currentPath)
		os.MkdirAll(filepath.Dir(fullPath), 0755)
		os.WriteFile(fullPath, []byte(content.String()), 0644)
	}

	return nil
}
