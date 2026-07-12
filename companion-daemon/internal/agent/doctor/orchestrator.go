package doctor

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	preDigest := o.activeWorkspace.Digest()
	o.state = StateRunningFixedSuites
	runner := NewSuiteRunner()
	result := runner.RunInWorkspace(o.activeWorkspace.Root, o.activeWorkspace.CandidatePkg)

	// Verify source-tree digest unchanged after suites.
	postDigest := workspaceDigest(o.activeWorkspace.Root)
	if preDigest != postDigest {
		return nil, fmt.Errorf("source-tree mutated during suite: pre=%s post=%s",
			preDigest[:16], postDigest[:16])
	}

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

	// Verify workspace digest — must be non-empty and match post-patch state.
	wsDigest := o.activeWorkspace.Digest()
	if wsDigest == "" {
		return nil, fmt.Errorf("workspace digest is empty")
	}
	currentDigest := workspaceDigest(o.activeWorkspace.Root)
	if currentDigest != wsDigest {
		return nil, fmt.Errorf("workspace digest changed: post-patch=%s current=%s",
			wsDigest[:16], currentDigest[:16])
	}

	// Independently recreate workspace from same baseline + exact patch,
	// verify identical digest.
	recreatedDigest, err := RecreateWorkspace(o.repoRoot, o.activeOps, o.provider, o.targetDir)
	if err != nil {
		return nil, fmt.Errorf("cannot recreate workspace: %w", err)
	}
	if recreatedDigest != wsDigest {
		return nil, fmt.Errorf("recreated workspace digest mismatch: original=%s recreated=%s",
			wsDigest[:16], recreatedDigest[:16])
	}

	evidenceDigest := HashJSON(o.activeEvidence)
	patchDigest := HashBytes(o.activePatchBytes)

	// Scan the exact unified diff for secrets BEFORE any bundle creation.
	// Secrets in preamble, headers, hunk metadata, or filenames must be caught.
	if scanContentForSecrets(string(o.activePatchBytes)) {
		return nil, fmt.Errorf("secret/credential detected in unified diff — review blocked")
	}

	var files []string
	for _, op := range o.activeOps {
		files = append(files, op.DiffPath)
	}

	var cmdLabels []string
	for _, c := range NewFixedSuite().Commands(o.activeWorkspace.CandidatePkg) {
		cmdLabels = append(cmdLabels, c.Label)
	}

	// Derive evidence provenance from actual input records (weakest tier).
	evidenceProv := deriveEvidenceProvenance(o.activeRequest.ObservedRecordSamples)

	// Build real fixture manifest from workspace candidate directory.
	adapterManifest, pokitTestManifest, providerFixtureManifest, redaction := buildFixtureManifest(
		o.activeWorkspace.Root, o.provider, o.targetDir)

	// Fail closed on any redaction finding — secrets must not enter the review bundle.
	if !redaction.Clean {
		return nil, fmt.Errorf("redaction scan failed: %d files scanned, findings present",
			redaction.Scanned)
	}
	if redaction.Scanned == 0 {
		return nil, fmt.Errorf("redaction scan found no files — empty fixture manifest")
	}

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
		o.activePatchBytes,       // unified diff
		files,                    // changed files
		adapterManifest,          // adapter source manifest
		pokitTestManifest,        // Pokit-owned test manifest
		providerFixtureManifest,  // provider fixture manifest
		redaction,                // redaction result
		cmdLabels,                // suite command manifest
		o.activeSuite.deepCopy(), // suite result (deep copy)
		driftEv,                  // drift evidence
		evidenceProv,             // evidence provenance
		wsDigest,                 // workspace digest
		unknowns,                 // remaining unknowns
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

// deriveEvidenceProvenance returns the provenance tier conservatively derived
// from the input records. Uses the WEAKEST provenance tier present (most
// conservative) so mixed authoritative/advisory evidence does not appear
// stronger than it is. Falls back to "unknown" if no records are available.
func deriveEvidenceProvenance(records []contract.RawRecord) string {
	if len(records) == 0 {
		return string(contract.ProvenanceUnknown)
	}
	weakestRank := -1
	weakest := ""
	for _, r := range records {
		p := string(r.Provenance)
		rank := contract.ProvenanceRank(contract.Provenance(p))
		if weakestRank == -1 || rank < weakestRank {
			weakestRank = rank
			weakest = p
		}
	}
	if weakest == "" {
		return string(contract.ProvenanceUnknown)
	}
	return weakest
}

// fixtureManifestEntry is one entry in a categorized manifest.
type fixtureManifestEntry struct {
	Path       string
	Digest     string
	Category   string // "adapter", "pokit_test", "provider_fixture"
	Provenance string
}

// buildFixtureManifest walks the candidate adapter directory, computes a
// digest for every source file, categorizes them (adapter vs test vs fixture),
// and returns typed manifest entries plus a redaction scan result.
func buildFixtureManifest(wsRoot, provider, targetDir string) (adapterManifest []string, pokitTestManifest []string, providerFixtureManifest []string, redaction RedactionResult) {
	redaction.Clean = true // innocent until proven otherwise
	candidateDir := filepath.Join(wsRoot, "internal", "agent", "adapters", provider, targetDir)
	entries, err := os.ReadDir(candidateDir)
	if err != nil {
		redaction.Clean = false
		redaction.Findings = append(redaction.Findings, "cannot read candidate dir")
		return
	}
	for _, e := range entries {
		if e.IsDir() || strings.HasSuffix(e.Name(), ".o") || strings.HasSuffix(e.Name(), ".test") {
			continue
		}
		p := filepath.Join(candidateDir, e.Name())
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		digest := HashBytes(data)
		entry := fmt.Sprintf("%s sha256:%s", e.Name(), digest)

		// Categorize.
		if e.Name() == "conformance_test.go" {
			pokitTestManifest = append(pokitTestManifest, entry)
		} else if strings.HasSuffix(e.Name(), "_test.go") {
			// Should not happen (sandbox rejects), but record if present.
			pokitTestManifest = append(pokitTestManifest, entry)
		} else if isProviderFixture(e.Name()) {
			// Admitted provider fixture formats.
			providerFixtureManifest = append(providerFixtureManifest, entry)
		} else if strings.HasSuffix(e.Name(), ".go") {
			adapterManifest = append(adapterManifest, entry)
		} else {
			// Unclassified file — fail closed.
			redaction.Clean = false
			redaction.Findings = append(redaction.Findings,
				fmt.Sprintf("%s: unclassified file type — must be .go or admitted fixture", e.Name()))
		}

		// Run redaction / secret scan.
		redaction.Scanned++
		content := string(data)
		if scanContentForSecrets(content) {
			redaction.Clean = false
			// Log only bounded metadata, never the raw content.
			redaction.Findings = append(redaction.Findings,
				fmt.Sprintf("%s: sensitive pattern detected", e.Name()))
		}
	}
	if redaction.Scanned == 0 {
		redaction.Clean = false
		redaction.Findings = append(redaction.Findings, "empty fixture manifest")
	}
	return
}

// scanContentForSecrets checks content for credential markers and private paths
// WITHOUT the diagnostic length truncation. Returns true if any pattern is found.
// This supersedes contract.ContainsSensitive which bounds at 256 bytes and is
// unsuitable for scanning full source files or diffs.
func scanContentForSecrets(content string) bool {
	// Credential markers.
	for _, p := range []string{
		"sk-", "ghp_", "xoxb-", "xoxp-", "Bearer ", "AKIA",
		"-----BEGIN RSA PRIVATE KEY-----",
		"-----BEGIN EC PRIVATE KEY-----",
		"-----BEGIN PRIVATE KEY-----",
		"-----BEGIN OPENSSH PRIVATE KEY-----",
		"-----BEGIN PGP PRIVATE KEY BLOCK-----",
		"glpat-",                       // GitLab personal access token
		"github_pat_",                  // GitHub fine-grained token
		"gho_", "ghu_", "ghs_", "ghr_", // GitHub OAuth tokens
		"eyJ", // JWT header heuristic (base64url of {"alg)
	} {
		if strings.Contains(content, p) {
			return true
		}
	}
	// Credential assignment patterns.
	for _, p := range []string{
		"api_key=", "apikey=", "api-key=",
		"token=", "secret=", "password=", "passwd=",
		"AWS_SECRET_ACCESS_KEY",
		"AWS_ACCESS_KEY_ID",
	} {
		if strings.Contains(content, p) {
			return true
		}
	}
	// Private path sentinels.
	for _, p := range []string{
		"/Users/", "/home/",
		"/private/", "/var/root/",
		"C:\\Users\\", "C:\\Documents and Settings\\",
	} {
		if strings.Contains(content, p) {
			return true
		}
	}
	return false
}

// isProviderFixture reports whether a filename is an admitted provider fixture
// format. Provider fixtures carry provider, version, and source provenance.
func isProviderFixture(name string) bool {
	for _, ext := range []string{".json", ".jsonl", ".ndjson", ".log"} {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	return s
}
