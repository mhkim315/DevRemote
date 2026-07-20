package term

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"devremote/companion-daemon/internal/agent"
	claude "devremote/companion-daemon/internal/agent/adapters/claude/v2_1_202"
	codex "devremote/companion-daemon/internal/agent/adapters/codex/v0_144_1"
	"devremote/companion-daemon/internal/agent/contract"
	"devremote/companion-daemon/internal/mux"
	"devremote/companion-daemon/internal/transcript"
)

// TelemetryService owns the telemetry state machine and background sampling loop.
type TelemetryService struct {
	reg        *mux.Registry
	notifier   Notifier
	approvals  *AuthoritativeApprovalStore // A1: generation-bound approval store
	transcript *transcript.Service         // T3: AgentEvent → Transcript projection
	interval   time.Duration
	// statusStore is the S1 session-owned agent-activity store.
	statusStore *AgentStatusStore

	// A1 R3-C: the per-session runtime delivery gate.
	deliveryGate *RuntimeDeliveryGate

	mu            sync.Mutex
	adapterStates map[string]*adapterState // PA3: retained accepted-adapter ingestion state
	done          chan struct{}
}

// NewTelemetryService creates a TelemetryService. Call Run() to start sampling.
func NewTelemetryService(reg *mux.Registry, notifier Notifier, approvals *AuthoritativeApprovalStore, transcriptSvc *transcript.Service) *TelemetryService {
	if notifier == nil {
		notifier = NoopNotifier{}
	}
	if approvals == nil {
		approvals = NewAuthoritativeApprovalStore()
	}
	return &TelemetryService{
		reg:           reg,
		notifier:      notifier,
		approvals:     approvals,
		transcript:    transcriptSvc,
		interval:      2 * time.Second,
		statusStore:   NewAgentStatusStore(),
		adapterStates: make(map[string]*adapterState),
		done:          make(chan struct{}),
	}
}

// Run starts the background telemetry sampling loop. Blocks until ctx is cancelled.
func (s *TelemetryService) Run(ctx context.Context) {
	defer close(s.done)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	// PB.2b: Legacy process discovery + session observation removed.
	// TelemetryService now handles only managed approval ingestion
	// (ingestApprovals), which is driven by push, not polling.
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		// Managed sessions are updated via Catalog push; no polling needed.
	}
}

// PB.2b: reconcileSessions removed — legacy observer deleted.
// PB.2b: logResolver removed — legacy observer deleted.
// SetLogResolver overrides it for testing.

// resolveLog resolves an agent log reference from process info.

// Done returns a channel that closes when the sampling loop has exited.
func (s *TelemetryService) Done() <-chan struct{} { return s.done }

// Snapshot returns a copy of the current telemetry state for all sessions.
func (s *TelemetryService) Snapshot(reg *mux.Registry) []SessionTelemetry {
	sessions := reg.Sessions(context.Background())

	res := make([]SessionTelemetry, 0)
	for _, sess := range sessions {
		compoundID := sess.AdapterName() + ":" + sess.ID()
		snap, _ := reg.Snapshot(sess.AdapterName())
		var errStr string
		if snap.LastError != nil {
			errStr = snap.LastError.Error()
		}
		isStale := snap.LastError != nil

		// PA3 Step 2 R1: legacy State/Load/Runner/RunnerColor/Events retained
		// as compatibility stubs (json:"-"), populated with defaults for test
		// continuity. Removed from JSON serialization. Full removal in Step 4.

		res = append(res, SessionTelemetry{
			ID: compoundID, DisplayID: sess.ID(), Adapter: sess.AdapterName(),
			State: "idle", Load: 0, Runner: "agent", RunnerColor: "#58a6ff",
			Events:              nil,
			Capabilities:        sessionCapabilities(sess),
			AdapterCapabilities: adapterCapabilityStrings(reg, sess.AdapterName()),
			Stale:               isStale, LastSuccessAt: snap.LastSuccessAt, LastError: errStr,
		})
	}
	sortTelemetry(res)
	return res
}

// acceptedAdapterFor returns a fresh accepted version-specific adapter instance.
func acceptedAdapterFor(kind string) contract.AgentAdapter {
	switch kind {
	case "codex":
		return &codex.Adapter{}
	case "claude":
		return &claude.Adapter{}
	default:
		return nil
	}
}

// SetDeliveryGate wires the production runtime delivery gate.
func (s *TelemetryService) SetDeliveryGate(g *RuntimeDeliveryGate) { s.deliveryGate = g }

// Clear removes all cached state for a session.
func (s *TelemetryService) Clear(sessionID string) {
	s.mu.Lock()
	delete(s.adapterStates, sessionID)
	s.mu.Unlock()
	if s.statusStore != nil {
		s.statusStore.Clear(sessionID)
	}
	if s.approvals != nil {
		s.approvals.Clear(sessionID)
	}
	if s.deliveryGate != nil {
		s.deliveryGate.Deactivate(sessionID)
	}
}

// RegisterOrReplaceLaunch is the single production-owned atomic launch replacement boundary.
func (s *TelemetryService) RegisterOrReplaceLaunch(spec transcript.LaunchSpec) (int64, bool) {
	gen, replaced, _ := transcript.RegisterOrReplaceLaunch(spec, func(gen int64) {
		s.invalidateForLaunch(spec.SessionID, gen)
	})
	return gen, replaced
}

// invalidateForLaunch installs a non-current high-water at the new launch generation.
func (s *TelemetryService) invalidateForLaunch(sessionID string, launchGen int64) {
	if s.statusStore == nil {
		return
	}
	s.mu.Lock()
	if st := s.adapterStates[sessionID]; st != nil {
		st.clear()
	}
	s.mu.Unlock()
	s.statusStore.Invalidate(sessionID, launchGen, 0, "launch binding replaced")
	if s.approvals != nil {
		s.approvals.SupersedeRuntime(sessionID, launchGen, 0, "launch binding replaced")
	}
	if s.deliveryGate != nil {
		s.deliveryGate.Deactivate(sessionID)
	}
}

// isAcceptedAdapter returns true only for agent kinds that have an accepted
// T0 contract.AgentAdapter in production.
func isAcceptedAdapter(kind string) bool {
	switch kind {
	case "codex", "claude":
		return true
	default:
		return false
	}
}

// isAcceptedVersion checks that the detected provider version matches
// the accepted adapter version. Used by adapter_state.go.
func isAcceptedVersion(kind, version string) bool {
	switch kind {
	case "codex":
		return version == "0.144.1"
	case "claude":
		return version == "2.1.202"
	default:
		return false
	}
}

// readRawLinesForPath reads raw JSONL lines for the accepted adapter path.
// getProcessIdentity extracts the PID and process start time for a session.
// callAcceptedAdapter passes records to the accepted version-specific adapter.
func callAcceptedAdapter(kind string, rawLines [][]byte, sessionID string, prevCursor string) ([]agent.AgentEvent, string, string, bool) {
	adapter := acceptedAdapterFor(kind)
	if adapter == nil {
		return nil, "", prevCursor, false
	}

	ctx := context.Background()
	records := make([]contract.RawRecord, 0, len(rawLines))
	for _, line := range rawLines {
		if len(line) == 0 {
			continue
		}
		records = append(records, contract.RawRecord{
			Bytes:      line,
			Source:     agent.SourceJSONL,
			Provenance: contract.ProvenanceNativeLog,
		})
	}
	if len(records) == 0 {
		return nil, "", prevCursor, false
	}

	result, err := adapter.ReadEvents(ctx, contract.ReadInput{
		Session:   contract.SessionContext{SessionID: sessionID},
		Records:   records,
		Cursor:    contract.Cursor(prevCursor),
		MaxEvents: 500,
	})
	nextCursor := prevCursor
	degraded := result.Degraded.Degraded
	if err == nil && string(result.NextCursor) != "" {
		nextCursor = string(result.NextCursor)
	}
	if err != nil {
		return nil, "", nextCursor, true
	}
	if len(result.Events) == 0 && degraded {
		return nil, "", nextCursor, true
	}
	if len(result.Events) == 0 {
		return nil, "", nextCursor, false
	}

	discoveredVersion := extractStreamVersion(kind, records)
	return result.Events, discoveredVersion, string(result.NextCursor), degraded
}

// extractStreamVersion reads the version from the first structural record field.
func extractStreamVersion(kind string, records []contract.RawRecord) string {
	if len(records) == 0 || len(records[0].Bytes) == 0 {
		return ""
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(records[0].Bytes, &m); err != nil {
		return ""
	}
	switch kind {
	case "codex":
		payload, _ := m["payload"]
		if payload != nil {
			var p map[string]json.RawMessage
			if err := json.Unmarshal(payload, &p); err == nil {
				if cv, ok := p["cli_version"]; ok {
					var v string
					if err := json.Unmarshal(cv, &v); err == nil && v != "" {
						return v
					}
				}
			}
		}
	case "claude":
		if ver, ok := m["version"]; ok {
			var v string
			if err := json.Unmarshal(ver, &v); err == nil && v != "" {
				return v
			}
		}
	}
	return ""
}

var now = time.Now
