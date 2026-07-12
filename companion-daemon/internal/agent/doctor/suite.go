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

type FixedCommand struct {
	Label    string
	PkgPath  string
	RunRegex string
	MinTests int
	Race     bool
	VetOnly  bool
	BuildOnly bool
}

type FixedSuite struct{}

func NewFixedSuite() *FixedSuite { return &FixedSuite{} }

func (fs *FixedSuite) Commands(candidatePkg string) []FixedCommand {
	cmds := []FixedCommand{
		{Label: "T0-contract-conformance", PkgPath: "./internal/agent/contract/", RunRegex: "Conformance", MinTests: 1},
	}
	if candidatePkg != "" {
		cmds = append(cmds,
			FixedCommand{Label: "candidate-build", PkgPath: candidatePkg, BuildOnly: true},
			FixedCommand{Label: "candidate-vet", PkgPath: candidatePkg, VetOnly: true},
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
		cr := sr.runInDir(c, workspaceRoot)
		result.TotalTests += cr.TotalTests
		result.Passed += cr.Passed
		result.Failed += cr.Failed
		result.Failures = append(result.Failures, cr.Failures...)
		if len(result.Failures) > MaxFailures {
			result.Failures = result.Failures[:MaxFailures]
		}
	}
	return result
}

func (sr *SuiteRunner) runInDir(cmd FixedCommand, dir string) ObservatoryResult {
	r := ObservatoryResult{AdapterName: cmd.Label}
	ctx, cancel := context.WithTimeout(context.Background(), sr.timeoutFor(cmd))
	defer cancel()

	args := sr.buildArgs(cmd)
	ec := exec.CommandContext(ctx, "go", args...)
	if dir != "" {
		ec.Dir = dir
	}

	stdout, err := ec.StdoutPipe()
	if err != nil {
		r.TotalTests = 1
		r.Failed = 1
		r.Failures = append(r.Failures, ObsTestFailure{TestName: cmd.Label, Reason: contract.SanitizeDiagnostic("pipe: " + err.Error())})
		return r
	}
	if err := ec.Start(); err != nil {
		r.TotalTests = 1
		r.Failed = 1
		r.Failures = append(r.Failures, ObsTestFailure{TestName: cmd.Label, Reason: contract.SanitizeDiagnostic("start: " + err.Error())})
		return r
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
			r.Failures = append(r.Failures, ObsTestFailure{TestName: cmd.Label, Reason: contract.SanitizeDiagnostic(reason)})
		}
	}
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
			r.Failed++
			if len(r.Failures) < MaxFailures {
				r.Failures = append(r.Failures, ObsTestFailure{TestName: name, Reason: "required test skipped — treated as failure"})
			}
		}
	}
	if pkgFail && r.TotalTests == 0 {
		r.TotalTests = 1
		r.Failed = 1
		r.Failures = append(r.Failures, ObsTestFailure{TestName: cmd.Label, Reason: "package failed"})
	}
	if r.TotalTests == 0 && cmd.MinTests > 0 {
		r.TotalTests = 1
		r.Failed = 1
		r.Failures = append(r.Failures, ObsTestFailure{TestName: cmd.Label, Reason: "zero tests — bypass detected"})
	}
	if cmd.MinTests > 0 && (r.Passed+r.Failed) < cmd.MinTests {
		delta := cmd.MinTests - (r.Passed + r.Failed)
		r.Failed += delta
		r.TotalTests += delta
		r.Failures = append(r.Failures, ObsTestFailure{TestName: cmd.Label, Reason: fmt.Sprintf("min %d tests, got %d", cmd.MinTests, r.Passed+r.Failed-delta)})
	}
	return r
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
