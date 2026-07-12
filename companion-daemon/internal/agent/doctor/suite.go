package doctor

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"

	"devremote/companion-daemon/internal/agent/contract"
)

// ── Suite bounds ──

const (
	// MaxFailures is the maximum number of test failures stored in an
	// ObservatoryResult. Failures beyond this cap are counted but not stored.
	MaxFailures = 32

	// suiteTimeout is the maximum wall-clock time for a single go test run.
	suiteTimeout = 120 * time.Second
)

// ── SuiteRunner ──

// SuiteRunner executes the fixed contract conformance harness for an adapter
// by running "go test -json -run Conformance <pkgPath>" as a subprocess. The
// JSON output is parsed to produce an ObservatoryResult.
//
// The SuiteRunner cannot modify the contract harness — it invokes the test
// binary as-is. A test the harness does not run is not reported; a test the
// harness adds in a future contract revision is automatically included.
type SuiteRunner struct{}

// NewSuiteRunner returns a new SuiteRunner.
func NewSuiteRunner() *SuiteRunner { return &SuiteRunner{} }

// ── Result types ──

// suiteEvent is one JSON line from "go test -json".
type suiteEvent struct {
	Action  string  `json:"Action"` // run, pass, fail, skip, output, pause, cont
	Test    string  `json:"Test"`
	Package string  `json:"Package"`
	Elapsed float64 `json:"Elapsed"`
	Output  string  `json:"Output"`
}

// ── Run ──

// Run executes the fixed conformance suite for the given adapter package and
// returns structured results. It runs:
//
//	go test -json -run Conformance <pkgPath>
//
// Example pkgPath: "./internal/agent/adapters/claude/v2_1_202/"
func (sr *SuiteRunner) Run(adapterName, pkgPath string) ObservatoryResult {
	result := ObservatoryResult{
		AdapterName: adapterName,
	}

	ctx, cancel := context.WithTimeout(context.Background(), suiteTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "test", "-json", "-run", "Conformance", pkgPath)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		result.Failures = append(result.Failures, ObsTestFailure{
			TestName: "suite",
			Reason:   contract.SanitizeDiagnostic("failed to create stdout pipe: " + err.Error()),
		})
		result.Failed = 1
		result.TotalTests = 1
		return result
	}

	if err := cmd.Start(); err != nil {
		result.Failures = append(result.Failures, ObsTestFailure{
			TestName: "suite",
			Reason:   contract.SanitizeDiagnostic("failed to start go test: " + err.Error()),
		})
		result.Failed = 1
		result.TotalTests = 1
		return result
	}

	// Parse JSON lines.
	decoder := json.NewDecoder(stdout)
	testStatus := map[string]string{} // test name → last action
	var packageFailed bool

	for decoder.More() {
		var ev suiteEvent
		if err := decoder.Decode(&ev); err != nil {
			break // truncated output — collect what we have
		}
		if ev.Test == "" {
			// Package-level event.
			if ev.Action == "fail" {
				packageFailed = true
			}
			continue
		}
		// Record the last action per test.
		switch ev.Action {
		case "pass", "fail", "skip":
			testStatus[ev.Test] = ev.Action
		}
	}

	// Wait for completion (ignore error — failures are recorded in the JSON).
	_ = cmd.Wait()

	// Aggregate results.
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
					Reason:   "test failed in fixed conformance suite",
				})
			}
		case "skip":
			// Skipped tests are not passed or failed.
		}
	}

	// If the package itself failed (build error, import cycle, etc.), record it.
	if packageFailed && result.TotalTests == 0 {
		result.TotalTests = 1
		result.Failed = 1
		result.Failures = append(result.Failures, ObsTestFailure{
			TestName: "package",
			Reason:   "package failed to build or test",
		})
	}

	// If we got context timeout.
	if ctx.Err() != nil {
		result.Failed++
		result.TotalTests++
		if len(result.Failures) < MaxFailures {
			result.Failures = append(result.Failures, ObsTestFailure{
				TestName: "suite",
				Reason:   contract.SanitizeDiagnostic("suite timed out: " + ctx.Err().Error()),
			})
		}
	}

	return result
}

// ── RunAll ──

// RunAll runs the full conformance suite (not just Conformance-named tests)
// for the given adapter package. Used for thorough verification.
func (sr *SuiteRunner) RunAll(adapterName, pkgPath string) ObservatoryResult {
	result := ObservatoryResult{
		AdapterName: adapterName,
	}

	ctx, cancel := context.WithTimeout(context.Background(), suiteTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "test", "-json", pkgPath)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		result.Failures = append(result.Failures, ObsTestFailure{
			TestName: "suite",
			Reason:   contract.SanitizeDiagnostic("failed to create stdout pipe: " + err.Error()),
		})
		result.Failed = 1
		result.TotalTests = 1
		return result
	}

	if err := cmd.Start(); err != nil {
		result.Failures = append(result.Failures, ObsTestFailure{
			TestName: "suite",
			Reason:   contract.SanitizeDiagnostic("failed to start go test: " + err.Error()),
		})
		result.Failed = 1
		result.TotalTests = 1
		return result
	}

	decoder := json.NewDecoder(stdout)
	testStatus := map[string]string{}
	var packageFailed bool

	for decoder.More() {
		var ev suiteEvent
		if err := decoder.Decode(&ev); err != nil {
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

	_ = cmd.Wait()

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
					Reason:   "test failed",
				})
			}
		}
	}

	if packageFailed && result.TotalTests == 0 {
		result.TotalTests = 1
		result.Failed = 1
		result.Failures = append(result.Failures, ObsTestFailure{
			TestName: "package",
			Reason:   "package failed to build or test",
		})
	}

	if ctx.Err() != nil {
		result.Failed++
		result.TotalTests++
		if len(result.Failures) < MaxFailures {
			result.Failures = append(result.Failures, ObsTestFailure{
				TestName: "suite",
				Reason:   contract.SanitizeDiagnostic("suite timed out: " + ctx.Err().Error()),
			})
		}
	}

	return result
}

// ── ValidatePatch (Doctor method) ──

// ValidatePatch checks that every changed file is within the adapter's write
// allowlist and not on the deny list. adapterPath is the adapter's version
// directory relative to the repo root (e.g. "internal/agent/adapters/claude/v2_1_202").
func (d *Doctor) ValidatePatch(adapterPath string, changedFiles []string) error {
	al := AllowList{
		WritePaths: []string{adapterPath + "/"},
	}
	sandbox := NewSandbox(al)
	return sandbox.ValidatePatch(changedFiles)
}

// ── Helpers ──

// safeReason bounds and redacts a failure reason string.
func safeReason(s string) string {
	s = contract.SanitizeDiagnostic(s)
	if len(s) > 256 {
		s = s[:256]
	}
	return s
}

// trimSpace trims a string for clean JSON output parsing.
func trimSpace(s string) string {
	return strings.TrimSpace(s)
}
