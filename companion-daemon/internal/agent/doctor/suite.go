package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"devremote/companion-daemon/internal/agent/contract"
)

const (
	MaxFailures     = 32
	suiteTimeout    = 120 * time.Second
	raceTestTimeout = 300 * time.Second
)

type FixedCommand struct {
	Label     string
	PkgPath   string
	RunRegex  string
	MinTests  int
	Race      bool
	VetOnly   bool
	BuildOnly bool
}

type FixedSuite struct{}

func NewFixedSuite() *FixedSuite { return &FixedSuite{} }

// Commands returns explicit immutable suite targets. Does NOT include
// internal/agent/doctor to prevent recursive E2E execution in workspace.
// Every declared label must have a matching command — the manifest
// regression test (TestFixedSuite_HasRequiredCommands) enforces this.
func (fs *FixedSuite) Commands(candidatePkg string) []FixedCommand {
	// Explicit packages only — does NOT include internal/agent/doctor
	// to prevent recursive E2E execution.
	cmds := []FixedCommand{
		// Build
		{Label: "build-adapters", PkgPath: "./internal/agent/adapters/...", BuildOnly: true},
		{Label: "build-contract", PkgPath: "./internal/agent/contract/...", BuildOnly: true},
		// Vet (non-Doctor packages)
		{Label: "vet-contract", PkgPath: "./internal/agent/contract/...", VetOnly: true},
		{Label: "vet-adapters", PkgPath: "./internal/agent/adapters/...", VetOnly: true},
		// T0 frozen harness and regressions
		{Label: "T0-contract-conformance", PkgPath: "./internal/agent/contract/", RunRegex: "Conformance", MinTests: 1},
		{Label: "T0-contract-race", PkgPath: "./internal/agent/contract/", RunRegex: "Conformance", MinTests: 1, Race: true},
		// T1 Codex: build+vet in adapters batch; explicit conformance/race/regression
		{Label: "T1-codex-conformance", PkgPath: "./internal/agent/adapters/codex/v0_144_1/", RunRegex: "Conformance", MinTests: 1},
		{Label: "T1-codex-race", PkgPath: "./internal/agent/adapters/codex/v0_144_1/", RunRegex: "Conformance", MinTests: 1, Race: true},
		{Label: "T1-codex-regression", PkgPath: "./internal/agent/adapters/codex/v0_144_1/", RunRegex: "TestCodexAdapter", MinTests: 5},
		// T2 Claude: build+vet in adapters batch; explicit conformance/race/regression
		{Label: "T2-claude-conformance", PkgPath: "./internal/agent/adapters/claude/v2_1_202/", RunRegex: "Conformance", MinTests: 1},
		{Label: "T2-claude-race", PkgPath: "./internal/agent/adapters/claude/v2_1_202/", RunRegex: "Conformance", MinTests: 1, Race: true},
		{Label: "T2-claude-regression", PkgPath: "./internal/agent/adapters/claude/v2_1_202/", RunRegex: "TestClaudeAdapter", MinTests: 5},
		// Non-Doctor agent race regressions (Claude + Codex parser/detector contract
		// tests only; Antigravity tests legitimately skip capabilities they don't support.)
		{Label: "agent-race-regression", PkgPath: "./internal/agent/", RunRegex: "Test(Claude|Codex|MockDetector|MockParser_Contract_Claude|MockParser_Contract_Codex)_Contract", MinTests: 1, Race: true},
		// Failure-isolation regression (T0 fixture adapter)
		{Label: "failure-isolation", PkgPath: "./internal/agent/contract/", RunRegex: "FixtureAdapter", MinTests: 1},
	}
	if candidatePkg != "" {
		cmds = append(cmds,
			FixedCommand{Label: "candidate-build", PkgPath: candidatePkg, BuildOnly: true},
			FixedCommand{Label: "candidate-vet", PkgPath: candidatePkg, VetOnly: true},
			FixedCommand{Label: "candidate-conformance", PkgPath: candidatePkg, RunRegex: "Conformance", MinTests: 1},
			FixedCommand{Label: "candidate-race", PkgPath: candidatePkg, RunRegex: "Conformance", MinTests: 1, Race: true},
		)
	}
	return cmds
}

type SuiteRunner struct {
	suite *FixedSuite
}

func NewSuiteRunner() *SuiteRunner { return &SuiteRunner{suite: NewFixedSuite()} }

func (sr *SuiteRunner) RunInWorkspace(workspaceRoot, candidatePkg string) ObservatoryResult {
	cmds := sr.suite.Commands(candidatePkg)
	result := ObservatoryResult{AdapterName: "fixed-suite"}
	for _, c := range cmds {
		cr, cmdResult := sr.runInDir(c, workspaceRoot)
		result.TotalTests += cr.TotalTests
		result.Passed += cr.Passed
		result.Failed += cr.Failed
		result.Failures = append(result.Failures, cr.Failures...)
		if len(result.Failures) > MaxFailures {
			result.Failures = result.Failures[:MaxFailures]
		}
		result.CommandResults = append(result.CommandResults, cmdResult)
	}
	return result
}

func (sr *SuiteRunner) runInDir(cmd FixedCommand, dir string) (ObservatoryResult, CommandResult) {
	r := ObservatoryResult{AdapterName: cmd.Label}
	cr := CommandResult{Label: cmd.Label, Command: "go " + shellJoin(cmd.BuildArgs())}

	ctx, cancel := context.WithTimeout(context.Background(), sr.timeoutFor(cmd))
	defer cancel()

	args := cmd.BuildArgs()
	ec := exec.CommandContext(ctx, "go", args...)
	if dir != "" {
		ec.Dir = dir
	}

	var stderrBuf strings.Builder
	ec.Stderr = &stderrBuf

	stdout, err := ec.StdoutPipe()
	if err != nil {
		r.TotalTests = 1
		r.Failed = 1
		r.Failures = append(r.Failures, ObsTestFailure{TestName: cmd.Label, Reason: contract.SanitizeDiagnostic("pipe: " + err.Error())})
		cr.ExitCode = -1
		return r, cr
	}
	if err := ec.Start(); err != nil {
		r.TotalTests = 1
		r.Failed = 1
		r.Failures = append(r.Failures, ObsTestFailure{TestName: cmd.Label, Reason: contract.SanitizeDiagnostic("start: " + err.Error())})
		cr.ExitCode = -1
		return r, cr
	}

	dec := json.NewDecoder(stdout)
	status := map[string]string{}
	pkgFail := false
	decErr := false
	for dec.More() {
		var ev suiteEvent
		if err := dec.Decode(&ev); err != nil {
			decErr = true
			break
		}
		if ev.Test == "" {
			if ev.Action == "fail" {
				pkgFail = true
			}
			continue
		}
		switch ev.Action {
		case "pass", "fail", "skip":
			status[ev.Test] = ev.Action
		}
	}
	waitErr := ec.Wait()
	hadErr := false

	// Capture exit code.
	if ec.ProcessState != nil {
		cr.ExitCode = ec.ProcessState.ExitCode()
	} else if waitErr != nil {
		cr.ExitCode = -1
	}

	if decErr {
		hadErr = true
		r.Failed++
		r.TotalTests++
		r.Failures = append(r.Failures, ObsTestFailure{TestName: cmd.Label, Reason: "JSON decode error"})
	}
	if waitErr != nil {
		hadErr = true
	}
	if ctx.Err() != nil {
		hadErr = true
		r.Failed++
		r.TotalTests++
		r.Failures = append(r.Failures, ObsTestFailure{TestName: cmd.Label, Reason: contract.SanitizeDiagnostic("timeout: " + ctx.Err().Error())})
	}
	if hadErr && len(status) == 0 {
		if r.TotalTests == 0 {
			r.TotalTests = 1
		}
		if r.Failed == 0 {
			r.Failed = 1
		}
		if len(r.Failures) == 0 {
			reason := "process failed"
			if waitErr != nil {
				reason = "process failed: " + waitErr.Error()
			}
			if stderrBuf.Len() > 0 {
				stderr := stderrBuf.String()
				if len(stderr) > 256 {
					stderr = stderr[:256]
				}
				reason += ": " + stderr
			}
			r.Failures = append(r.Failures, ObsTestFailure{TestName: cmd.Label, Reason: contract.SanitizeDiagnostic(reason)})
		}
	}
	skipped := 0
	for name, st := range status {
		r.TotalTests++
		switch st {
		case "pass":
			r.Passed++
		case "fail":
			r.Failed++
			if len(r.Failures) < MaxFailures {
				r.Failures = append(r.Failures, ObsTestFailure{TestName: name, Reason: "test failed"})
			}
		case "skip":
			skipped++
			r.Failed++
			if len(r.Failures) < MaxFailures {
				r.Failures = append(r.Failures, ObsTestFailure{TestName: name, Reason: "required test skipped — treated as failure"})
			}
		}
	}
	cr.TotalTests = r.TotalTests
	cr.Passed = r.Passed
	cr.Failed = r.Failed
	cr.Skipped = skipped
	if pkgFail && r.TotalTests == 0 {
		r.TotalTests = 1
		r.Failed = 1
		r.Failures = append(r.Failures, ObsTestFailure{TestName: cmd.Label, Reason: "package failed"})
		cr.TotalTests = 1
		cr.Failed = 1
	}
	if r.TotalTests == 0 && cmd.MinTests > 0 {
		r.TotalTests = 1
		r.Failed = 1
		r.Failures = append(r.Failures, ObsTestFailure{TestName: cmd.Label, Reason: "zero tests — bypass detected"})
		cr.TotalTests = 1
		cr.Failed = 1
	}
	if cmd.MinTests > 0 && (r.Passed+r.Failed) < cmd.MinTests {
		delta := cmd.MinTests - (r.Passed + r.Failed)
		r.Failed += delta
		r.TotalTests += delta
		cr.Failed += delta
		cr.TotalTests += delta
		r.Failures = append(r.Failures, ObsTestFailure{TestName: cmd.Label, Reason: fmt.Sprintf("min %d tests, got %d", cmd.MinTests, r.Passed+r.Failed-delta)})
	}
	return r, cr
}

// BuildArgs returns the "go" argument list for this command.
func (fc FixedCommand) BuildArgs() []string {
	if fc.BuildOnly {
		return []string{"build", fc.PkgPath}
	}
	if fc.VetOnly {
		return []string{"vet", fc.PkgPath}
	}
	args := []string{"test", "-json"}
	if fc.Race {
		args = append(args, "-race")
	}
	if fc.RunRegex != "" {
		args = append(args, "-run", fc.RunRegex)
	}
	args = append(args, fc.PkgPath)
	return args
}

func (sr *SuiteRunner) timeoutFor(cmd FixedCommand) time.Duration {
	if cmd.Race {
		return raceTestTimeout
	}
	return suiteTimeout
}

func fixedCommandsEqual(a, b []FixedCommand) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Label != b[i].Label || a[i].PkgPath != b[i].PkgPath ||
			a[i].RunRegex != b[i].RunRegex || a[i].Race != b[i].Race ||
			a[i].VetOnly != b[i].VetOnly || a[i].BuildOnly != b[i].BuildOnly {
			return false
		}
	}
	return true
}

func shellJoin(args []string) string {
	var b strings.Builder
	for i, a := range args {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(a)
	}
	return b.String()
}

type suiteEvent struct {
	Action  string  `json:"Action"`
	Test    string  `json:"Test"`
	Package string  `json:"Package"`
	Elapsed float64 `json:"Elapsed"`
	Output  string  `json:"Output"`
}
