package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"devremote/companion-daemon/internal/agent/contract"
)

const (
	MaxFailures     = 32
	suiteTimeout    = 120 * time.Second
	raceTestTimeout = 300 * time.Second
)

// FixedCommand is one fixed test invocation owned by the FixedSuite.
type FixedCommand struct {
	Label    string
	PkgPath  string
	RunRegex string
	MinTests int
	Race     bool
	VetOnly  bool
	BuildOnly bool
}

// FixedSuite owns the exact command list for D1 verification.
type FixedSuite struct{}

func NewFixedSuite() *FixedSuite { return &FixedSuite{} }

func (fs *FixedSuite) Commands() []FixedCommand {
	return []FixedCommand{
		{Label: "build", PkgPath: "./...", BuildOnly: true},
		{Label: "vet", PkgPath: "./...", VetOnly: true},
		{Label: "T0-contract-conformance", PkgPath: "./internal/agent/contract/", RunRegex: "Conformance", MinTests: 1},
		{Label: "T1-codex-conformance", PkgPath: "./internal/agent/adapters/codex/v0_144_1/", RunRegex: "Conformance", MinTests: 1},
		{Label: "T1-codex-version-gate", PkgPath: "./internal/agent/adapters/codex/v0_144_1/", RunRegex: "VersionGate", MinTests: 1},
		{Label: "T1-codex-no-leak", PkgPath: "./internal/agent/adapters/codex/v0_144_1/", RunRegex: "NoLeak|NoSecret", MinTests: 1},
		{Label: "T1-codex-cursor", PkgPath: "./internal/agent/adapters/codex/v0_144_1/", RunRegex: "Cursor", MinTests: 1},
		{Label: "T2-claude-conformance", PkgPath: "./internal/agent/adapters/claude/v2_1_202/", RunRegex: "Conformance", MinTests: 1},
		{Label: "T2-claude-version-gate", PkgPath: "./internal/agent/adapters/claude/v2_1_202/", RunRegex: "VersionGate", MinTests: 1},
		{Label: "T2-claude-no-leak", PkgPath: "./internal/agent/adapters/claude/v2_1_202/", RunRegex: "NoLeak|NoSecret", MinTests: 1},
		{Label: "T2-claude-cursor", PkgPath: "./internal/agent/adapters/claude/v2_1_202/", RunRegex: "Cursor", MinTests: 1},
		{Label: "race-agent", PkgPath: "./internal/agent/...", Race: true, MinTests: 1},
	}
}

// SuiteRunner executes FixedSuite commands via "go test -json" subprocesses.
type SuiteRunner struct {
	suite *FixedSuite
}

func NewSuiteRunner() *SuiteRunner {
	return &SuiteRunner{suite: NewFixedSuite()}
}

// RunInWorkspace executes all FixedSuite commands with the workspace directory
// as the working directory. candidatePkgPath is appended as an extra test.
func (sr *SuiteRunner) RunInWorkspace(workspaceRoot, candidatePkgPath string) ObservatoryResult {
	commands := sr.suite.Commands()
	if candidatePkgPath != "" {
		commands = append(commands, FixedCommand{
			Label:    "candidate-conformance",
			PkgPath:  candidatePkgPath,
			RunRegex: "Conformance",
			MinTests: 1,
		})
	}

	result := ObservatoryResult{AdapterName: "fixed-suite"}
	for _, cmd := range commands {
		cmdResult := sr.runCommandInDir(cmd, workspaceRoot)
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

// runCommandInDir executes a single FixedCommand with optional working dir.
func (sr *SuiteRunner) runCommandInDir(cmd FixedCommand, dir string) ObservatoryResult {
	result := ObservatoryResult{AdapterName: cmd.Label}

	ctx, cancel := context.WithTimeout(context.Background(), sr.timeoutFor(cmd))
	defer cancel()

	args := sr.buildArgs(cmd)
	execCmd := exec.CommandContext(ctx, "go", args...)
	if dir != "" {
		execCmd.Dir = dir
	}

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
	hadError := false

	if decodeErr {
		hadError = true
		result.Failed++
		result.TotalTests++
		result.Failures = append(result.Failures, ObsTestFailure{
			TestName: cmd.Label,
			Reason:   "JSON output truncated or undecodable",
		})
	}
	if waitErr != nil {
		hadError = true
	}
	if ctx.Err() != nil {
		hadError = true
		result.Failed++
		result.TotalTests++
		result.Failures = append(result.Failures, ObsTestFailure{
			TestName: cmd.Label,
			Reason:   contract.SanitizeDiagnostic("timeout: " + ctx.Err().Error()),
		})
	}
	if hadError && len(testStatus) == 0 {
		if result.TotalTests == 0 {
			result.TotalTests = 1
		}
		if result.Failed == 0 {
			result.Failed = 1
		}
		if len(result.Failures) == 0 {
			reason := "process failed"
			if waitErr != nil {
				reason = "process failed: " + waitErr.Error()
			}
			result.Failures = append(result.Failures, ObsTestFailure{
				TestName: cmd.Label,
				Reason:   contract.SanitizeDiagnostic(reason),
			})
		}
	}

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
			result.Failed++
			if len(result.Failures) < MaxFailures {
				result.Failures = append(result.Failures, ObsTestFailure{
					TestName: name,
					Reason:   "required test was skipped — treated as failure",
				})
			}
		}
	}

	if packageFailed && result.TotalTests == 0 {
		result.TotalTests = 1
		result.Failed = 1
		result.Failures = append(result.Failures, ObsTestFailure{
			TestName: cmd.Label,
			Reason:   "package failed to build or test",
		})
	}
	if result.TotalTests == 0 && cmd.MinTests > 0 {
		result.TotalTests = 1
		result.Failed = 1
		result.Failures = append(result.Failures, ObsTestFailure{
			TestName: cmd.Label,
			Reason:   "zero tests executed — suite bypass detected",
		})
	}
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

type suiteEvent struct {
	Action  string  `json:"Action"`
	Test    string  `json:"Test"`
	Package string  `json:"Package"`
	Elapsed float64 `json:"Elapsed"`
	Output  string  `json:"Output"`
}
