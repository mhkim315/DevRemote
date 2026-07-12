package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"devremote/companion-daemon/internal/agent/contract"
)

// ── Suite bounds ──

const (
	MaxFailures     = 32
	suiteTimeout    = 120 * time.Second
	raceTestTimeout = 300 * time.Second
)

// ── FixedCommand ──

// FixedCommand is one fixed test invocation owned by the FixedSuite. The
// caller cannot inject or substitute commands — the suite owns the list.
type FixedCommand struct {
	Label    string // human-readable name for diagnostics
	PkgPath  string // exact package path (e.g. "./internal/agent/contract/")
	RunRegex string // -run pattern (empty = all tests)
	MinTests int    // minimum expected test count (0 = any count allowed)
	Race     bool   // run with -race flag
	VetOnly  bool   // run go vet instead of go test
	BuildOnly bool  // run go build instead of go test
}

// ── FixedSuite ──

// FixedSuite owns the exact command list for D1 verification. The orchestrator
// controls what runs; the caller cannot inject pkgPath, -run patterns, or
// substitute commands.
type FixedSuite struct{}

// NewFixedSuite returns a new FixedSuite.
func NewFixedSuite() *FixedSuite { return &FixedSuite{} }

// Commands returns the fixed, authoritative list of verification commands.
// The list is immutable — no caller customization is accepted.
func (fs *FixedSuite) Commands() []FixedCommand {
	return []FixedCommand{
		// Build and vet gates (repository-wide).
		{Label: "build", PkgPath: "./...", BuildOnly: true, MinTests: 0},
		{Label: "vet", PkgPath: "./...", VetOnly: true, MinTests: 0},

		// T0 contract and harness.
		{Label: "T0-contract-conformance", PkgPath: "./internal/agent/contract/", RunRegex: "Conformance", MinTests: 1},

		// T1 Codex adapter (accepted).
		{Label: "T1-codex-conformance", PkgPath: "./internal/agent/adapters/codex/v0_144_1/", RunRegex: "Conformance", MinTests: 1},
		{Label: "T1-codex-version-gate", PkgPath: "./internal/agent/adapters/codex/v0_144_1/", RunRegex: "VersionGate", MinTests: 1},
		{Label: "T1-codex-no-leak", PkgPath: "./internal/agent/adapters/codex/v0_144_1/", RunRegex: "NoLeak|NoSecret", MinTests: 1},
		{Label: "T1-codex-cursor", PkgPath: "./internal/agent/adapters/codex/v0_144_1/", RunRegex: "Cursor", MinTests: 1},

		// T2 Claude adapter (accepted).
		{Label: "T2-claude-conformance", PkgPath: "./internal/agent/adapters/claude/v2_1_202/", RunRegex: "Conformance", MinTests: 1},
		{Label: "T2-claude-version-gate", PkgPath: "./internal/agent/adapters/claude/v2_1_202/", RunRegex: "VersionGate", MinTests: 1},
		{Label: "T2-claude-no-leak", PkgPath: "./internal/agent/adapters/claude/v2_1_202/", RunRegex: "NoLeak|NoSecret", MinTests: 1},
		{Label: "T2-claude-cursor", PkgPath: "./internal/agent/adapters/claude/v2_1_202/", RunRegex: "Cursor", MinTests: 1},

		// Race tests on all agent packages.
		{Label: "race-agent", PkgPath: "./internal/agent/...", Race: true, MinTests: 1},
	}
}

// ── SuiteRunner ──

// SuiteRunner executes FixedSuite commands via "go test -json" subprocesses
// and collects ObservatoryResults. No caller-supplied paths or patterns are
// accepted — the FixedSuite owns the command list entirely.
type SuiteRunner struct {
	suite *FixedSuite
}

// NewSuiteRunner returns a SuiteRunner backed by a FixedSuite.
func NewSuiteRunner() *SuiteRunner {
	return &SuiteRunner{suite: NewFixedSuite()}
}

// ── Run ──

// Run executes all FixedSuite commands for the given optional candidate
// adapter pkgPath. If candidatePkgPath is non-empty, an extra conformance
// test is appended for that candidate.
func (sr *SuiteRunner) Run(candidatePkgPath string) ObservatoryResult {
	commands := sr.suite.Commands()
	if candidatePkgPath != "" {
		candidateCmd := FixedCommand{
			Label:    "candidate-conformance",
			PkgPath:  candidatePkgPath,
			RunRegex: "Conformance",
			MinTests: 1,
		}
		commands = append(commands, candidateCmd)
	}

	result := ObservatoryResult{AdapterName: "fixed-suite"}
	for _, cmd := range commands {
		cmdResult := sr.runCommand(cmd)
		result.TotalTests += cmdResult.TotalTests
		result.Passed += cmdResult.Passed
		result.Failed += cmdResult.Failed
		result.Failures = append(result.Failures, cmdResult.Failures...)
		if len(result.Failures) > MaxFailures {
			result.Failures = result.Failures[:MaxFailures]
		}
	}
	return result
}

// runCommand executes a single FixedCommand and returns its result.
func (sr *SuiteRunner) runCommand(cmd FixedCommand) ObservatoryResult {
	result := ObservatoryResult{AdapterName: cmd.Label}

	ctx, cancel := context.WithTimeout(context.Background(), sr.timeoutFor(cmd))
	defer cancel()

	args := sr.buildArgs(cmd)
	execCmd := exec.CommandContext(ctx, "go", args...)

	stdout, err := execCmd.StdoutPipe()
	if err != nil {
		result.TotalTests = 1
		result.Failed = 1
		result.Failures = append(result.Failures, ObsTestFailure{
			TestName: cmd.Label,
			Reason:   contract.SanitizeDiagnostic("stdout pipe failed: " + err.Error()),
		})
		return result
	}

	if err := execCmd.Start(); err != nil {
		result.TotalTests = 1
		result.Failed = 1
		result.Failures = append(result.Failures, ObsTestFailure{
			TestName: cmd.Label,
			Reason:   contract.SanitizeDiagnostic("start failed: " + err.Error()),
		})
		return result
	}

	decoder := json.NewDecoder(stdout)
	testStatus := map[string]string{}
	var packageFailed bool
	decodeErr := false

	for decoder.More() {
		var ev suiteEvent
		if err := decoder.Decode(&ev); err != nil {
			decodeErr = true
			break
		}
		if ev.Test == "" {
			if ev.Action == "fail" {
				packageFailed = true
			}
			continue
		}
		switch ev.Action {
		case "pass", "fail", "skip":
			testStatus[ev.Test] = ev.Action
		}
	}

	waitErr := execCmd.Wait()

	// Fail closed on any error condition.
	hadError := false

	// JSON decode interrupted → failure.
	if decodeErr {
		hadError = true
		result.Failed++
		result.TotalTests++
		result.Failures = append(result.Failures, ObsTestFailure{
			TestName: cmd.Label,
			Reason:   "JSON output truncated or undecodable",
		})
	}

	// cmd.Wait() non-zero exit → failure.
	if waitErr != nil {
		hadError = true
	}

	// Context timeout → failure.
	if ctx.Err() != nil {
		hadError = true
		result.Failed++
		result.TotalTests++
		result.Failures = append(result.Failures, ObsTestFailure{
			TestName: cmd.Label,
			Reason:   contract.SanitizeDiagnostic("timeout: " + ctx.Err().Error()),
		})
	}

	// If process failed completely and no tests were recorded, count as failure.
	if hadError && len(testStatus) == 0 {
		if result.TotalTests == 0 {
			result.TotalTests = 1
		}
		if result.Failed == 0 {
			result.Failed = 1
		}
		if len(result.Failures) == 0 {
			result.Failures = append(result.Failures, ObsTestFailure{
				TestName: cmd.Label,
				Reason:   contract.SanitizeDiagnostic("process failed: " + waitErr.Error()),
			})
		}
	}

	// Aggregate per-test results.
	for name, status := range testStatus {
		result.TotalTests++
		switch status {
		case "pass":
			result.Passed++
		case "fail":
			result.Failed++
			if len(result.Failures) < MaxFailures {
				result.Failures = append(result.Failures, ObsTestFailure{
					TestName: name,
					Reason:   "test failed in fixed suite",
				})
			}
		case "skip":
			// Skipped required tests → failure.
			result.Failed++
			if len(result.Failures) < MaxFailures {
				result.Failures = append(result.Failures, ObsTestFailure{
					TestName: name,
					Reason:   "required test was skipped — treated as failure",
				})
			}
		}
	}

	// Package build/run failure with no tests → failure.
	if packageFailed && result.TotalTests == 0 {
		result.TotalTests = 1
		result.Failed = 1
		result.Failures = append(result.Failures, ObsTestFailure{
			TestName: cmd.Label,
			Reason:   "package failed to build or test",
		})
	}

	// Zero executed tests → failure (safety: empty test suite is a bypass).
	if result.TotalTests == 0 && cmd.MinTests > 0 {
		result.TotalTests = 1
		result.Failed = 1
		result.Failures = append(result.Failures, ObsTestFailure{
			TestName: cmd.Label,
			Reason:   "zero tests executed — suite bypass detected",
		})
	}

	// MinTests not met → failure.
	if cmd.MinTests > 0 && (result.Passed+result.Failed) < cmd.MinTests {
		delta := cmd.MinTests - (result.Passed + result.Failed)
		result.Failed += delta
		result.TotalTests += delta
		result.Failures = append(result.Failures, ObsTestFailure{
			TestName: cmd.Label,
			Reason:   fmt.Sprintf("expected at least %d tests, got %d", cmd.MinTests, result.Passed+result.Failed-delta),
		})
	}

	return result
}

func (sr *SuiteRunner) buildArgs(cmd FixedCommand) []string {
	if cmd.BuildOnly {
		return []string{"build", cmd.PkgPath}
	}
	if cmd.VetOnly {
		return []string{"vet", cmd.PkgPath}
	}

	args := []string{"test", "-json"}
	if cmd.Race {
		args = append(args, "-race")
	}
	if cmd.RunRegex != "" {
		args = append(args, "-run", cmd.RunRegex)
	}
	args = append(args, cmd.PkgPath)
	return args
}

func (sr *SuiteRunner) timeoutFor(cmd FixedCommand) time.Duration {
	if cmd.Race {
		return raceTestTimeout
	}
	return suiteTimeout
}

// ── suiteEvent ──

type suiteEvent struct {
	Action  string  `json:"Action"`
	Test    string  `json:"Test"`
	Package string  `json:"Package"`
	Elapsed float64 `json:"Elapsed"`
	Output  string  `json:"Output"`
}
