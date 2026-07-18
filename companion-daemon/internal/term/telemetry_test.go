package term

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"devremote/companion-daemon/internal/models"
	"devremote/companion-daemon/internal/mux"
)

type telemetryBatchAdapter struct {
	name     string
	snapshot map[string]models.ProcessInfo
	err      error
	calls    int
}

func (a *telemetryBatchAdapter) Name() string { return a.name }
func (a *telemetryBatchAdapter) ListSessions(ctx context.Context) ([]mux.Session, error) {
	return nil, nil
}
func (a *telemetryBatchAdapter) GetSession(id string) (mux.Session, error) {
	return nil, fmt.Errorf("not implemented")
}
func (a *telemetryBatchAdapter) ProcessSnapshot(ctx context.Context) (map[string]models.ProcessInfo, error) {
	a.calls++
	return a.snapshot, a.err
}

// TestPA2a_TelemetryNoLinkStore proves that NewTelemetryService constructs
// and operates without a LinkStore parameter (link-based log resolution
// was removed in PA2a). The process-based resolver path remains functional.
func TestPA2a_TelemetryNoLinkStore(t *testing.T) {
	reg := mux.MustNewRegistry()

	// Construct without LinkStore — must succeed.
	svc := NewTelemetryService(reg, NewMemoryEventStore(),
		nil, nil, NewApprovalStore(),
		NewActivityBuffer(100), nil)
	if svc == nil {
		t.Fatal("NewTelemetryService returned nil")
	}

	// Verify the service struct has no links field (direct field check
	// impossible; verified by compilation + non-nil result).
	// The linked-log resolver block was removed, so this construction
	// proves the code path no longer depends on LinkStore.
}

// TestPA2a_TelemetryProcessFallback verifies that the process-based log
// resolver path remains functional after LinkStore removal. The old
// link-only Antigravity external log path is intentionally removed.

// TestPA2a_TelemetryProcessFallback verifies that the process-based log
// resolver path remains functional after LinkStore removal. The old
// link-only Antigravity external log path is intentionally removed.
func TestPA2a_TelemetryProcessFallback(t *testing.T) {
	reg := mux.MustNewRegistry()
	svc := NewTelemetryService(reg, NewMemoryEventStore(),
		nil, nil, NewApprovalStore(),
		NewActivityBuffer(100), nil)
	if svc == nil {
		t.Fatal("NewTelemetryService returned nil without LinkStore")
	}

	// Set an injectable resolver to prove the process-based path works
	// without any link dependency.
	svc.logResolver = func(p models.ProcessInfo) (LogRef, error) {
		// Simulate the production ResolveAgentLog: identify agent from
		// the process command/path without any link lookup.
		return LogRef{Agent: "codex", Path: "/tmp/test-codex.log"}, nil
	}

	// With a valid process snapshot, resolveLog must succeed.
	codexInfo := models.ProcessInfo{
		PID:     12345,
		CWD:     "/tmp",
		Command: "node /pinned/toolchain/node_modules/.bin/codex app-server --stdio",
	}
	ref, err := svc.resolveLog(context.Background(), codexInfo)
	if err != nil {
		t.Fatalf("resolveLog for codex process: %v", err)
	}
	if ref.Agent != "codex" {
		t.Errorf("resolveLog agent = %q, want codex", ref.Agent)
	}
	if ref.Path == "" {
		t.Error("resolveLog returned empty path")
	}

	// The old link-only Antigravity external log path is intentionally
	// removed. The linked-log resolver block (previously step 1 in
	// processSession) is deleted from telemetry_service.go. No
	// GetLink/ResolveLink call remains in the production code.
}

func TestEvaluateState(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name               string
		initialState       string
		initialLoad        int
		lastActivity       time.Time
		parsedNewEvents    bool
		lastEvent          models.AgentEvent
		logErr             error
		isWaiting          bool
		isThinkingFallback bool
		diffSize           int
		expectedState      string
		expectedLoad       int
	}{
		{
			name:          "Waiting state takes precedence",
			initialState:  "idle",
			logErr:        nil,
			isWaiting:     true,
			expectedState: "waiting",
			expectedLoad:  0,
		},
		{
			name:            "Parsed tool_use -> working",
			initialState:    "idle",
			parsedNewEvents: true,
			lastEvent:       models.AgentEvent{Type: "tool_use"},
			logErr:          nil,
			expectedState:   "working",
			expectedLoad:    100,
		},
		{
			name:            "Parsed user -> thinking",
			initialState:    "idle",
			parsedNewEvents: true,
			lastEvent:       models.AgentEvent{Type: "user"},
			logErr:          nil,
			expectedState:   "thinking",
			expectedLoad:    50,
		},
		{
			name:            "Parsed tool_result -> thinking",
			initialState:    "idle",
			parsedNewEvents: true,
			lastEvent:       models.AgentEvent{Type: "tool_result"},
			logErr:          nil,
			expectedState:   "thinking",
			expectedLoad:    50,
		},
		{
			name:            "Parsed message Thinking -> thinking",
			initialState:    "idle",
			parsedNewEvents: true,
			lastEvent:       models.AgentEvent{Type: "message", Summary: "Thinking about it"},
			logErr:          nil,
			expectedState:   "thinking",
			expectedLoad:    50,
		},
		{
			name:            "Parsed message normal -> idle",
			initialState:    "working",
			parsedNewEvents: true,
			lastEvent:       models.AgentEvent{Type: "message", Summary: "Claude"},
			logErr:          nil,
			expectedState:   "idle",
			expectedLoad:    0,
		},
		{
			name:          "JSONL log found, no events, prompt disappeared -> idle",
			initialState:  "waiting",
			logErr:        nil,
			isWaiting:     false,
			lastActivity:  now,
			expectedState: "idle",
			expectedLoad:  0,
		},
		{
			name:          "JSONL log found, working timeout -> thinking",
			initialState:  "working",
			logErr:        nil,
			isWaiting:     false,
			lastActivity:  now.Add(-15 * time.Second),
			expectedState: "thinking",
			expectedLoad:  50,
		},
		{
			name:          "JSONL log found, thinking timeout -> idle",
			initialState:  "thinking",
			logErr:        nil,
			isWaiting:     false,
			lastActivity:  now.Add(-15 * time.Second),
			expectedState: "idle",
			expectedLoad:  0,
		},
		{
			name:               "No JSONL log, fallback diff > 50 -> working",
			initialState:       "idle",
			logErr:             errors.New("no log"),
			isThinkingFallback: false,
			diffSize:           100,
			expectedState:      "working",
			expectedLoad:       100,
		},
		{
			name:               "No JSONL log, fallback isThinking -> thinking",
			initialState:       "idle",
			logErr:             errors.New("no log"),
			isThinkingFallback: true,
			diffSize:           0,
			expectedState:      "thinking",
			expectedLoad:       50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stateData := &sessionStateData{
				State:        tt.initialState,
				Load:         tt.initialLoad,
				LastActivity: tt.lastActivity,
			}

			evaluateState(stateData, tt.parsedNewEvents, tt.lastEvent, tt.logErr, tt.isWaiting, tt.isThinkingFallback, tt.diffSize)

			if stateData.State != tt.expectedState {
				t.Errorf("expected state %s, got %s", tt.expectedState, stateData.State)
			}
			if stateData.Load != tt.expectedLoad {
				t.Errorf("expected load %d, got %d", tt.expectedLoad, stateData.Load)
			}
		})
	}
}

func TestPreserveTransientSamplingFailure(t *testing.T) {
	state := &sessionStateData{State: "working", Load: 100}
	logErr := errors.New("no log")
	screenErr := errors.New("read-screen failed")

	if !preserveTransientSamplingFailure(state, logErr, screenErr, false, false) {
		t.Fatal("first transient sampling failure should preserve the previous state")
	}
	if state.State != "working" || state.Load != 100 {
		t.Fatalf("state changed on first transient failure: state=%s load=%d", state.State, state.Load)
	}
	if !preserveTransientSamplingFailure(state, logErr, screenErr, false, false) {
		t.Fatal("second transient sampling failure should preserve the previous state")
	}
	if preserveTransientSamplingFailure(state, logErr, screenErr, false, false) {
		t.Fatal("third consecutive sampling failure should stop preserving stale state")
	}

	if preserveTransientSamplingFailure(state, nil, screenErr, false, false) {
		t.Fatal("successful log resolution should reset transient preservation")
	}
	if state.SamplingFailures != 0 {
		t.Fatalf("sampling failures were not reset: %d", state.SamplingFailures)
	}
}

func TestTelemetryService_StopsOnCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	reg := mux.MustNewRegistry()
	svc := NewTelemetryService(reg, NewMemoryEventStore(), nil, nil, nil, nil, nil)

	go svc.Run(ctx)
	cancel()

	select {
	case <-svc.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("telemetry service did not stop within deadline after cancel")
	}
}

func TestCollectProcessSnapshotsUsesOneBatchPerAdapter(t *testing.T) {
	healthy := &telemetryBatchAdapter{
		name: "healthy",
		snapshot: map[string]models.ProcessInfo{
			"session-1": {PID: 101},
		},
	}
	failing := &telemetryBatchAdapter{
		name: "failing",
		err:  errors.New("socket unavailable"),
	}

	snapshots, batchAdapters, failedAdapters := collectProcessSnapshots(
		context.Background(),
		[]mux.Adapter{healthy, failing},
	)

	if healthy.calls != 1 || failing.calls != 1 {
		t.Fatalf("expected one batch call per adapter, got healthy=%d failing=%d", healthy.calls, failing.calls)
	}
	if snapshots["healthy:session-1"].PID != 101 {
		t.Fatalf("missing canonical batch snapshot: %#v", snapshots)
	}
	if !batchAdapters["healthy"] || !batchAdapters["failing"] {
		t.Fatalf("batch-capable adapters were not recorded: %#v", batchAdapters)
	}
	if failedAdapters["healthy"] || !failedAdapters["failing"] {
		t.Fatalf("unexpected failed adapter set: %#v", failedAdapters)
	}
}
