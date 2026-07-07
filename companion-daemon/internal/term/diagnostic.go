package term

import (
	"encoding/json"
	"net/http"
	"runtime"
	"strings"
	"time"
)

var startTime = time.Now()

// DiagnosticSnapshot is the redacted, exportable diagnostic bundle.
type DiagnosticSnapshot struct {
	Daemon   DaemonDiag    `json:"daemon"`
	Adapters []AdapterDiag `json:"adapters"`
	Sessions []SessionDiag `json:"sessions"`
}

type DaemonDiag struct {
	Uptime       string `json:"uptime"`
	GoVersion    string `json:"goVersion"`
	AdapterCount int    `json:"adapterCount"`
}

type AdapterDiag struct {
	Name      string `json:"name"`
	Healthy   bool   `json:"healthy"`
	LastError string `json:"lastError,omitempty"`
}

type SessionDiag struct {
	ID               string  `json:"id"`
	Adapter          string  `json:"adapter"`
	AgentKind        string  `json:"agentKind,omitempty"`
	AgentStatus      string  `json:"agentStatus,omitempty"`
	AgentConfidence  float64 `json:"agentConfidence,omitempty"`
	State            string  `json:"state"`
	ParserHealthy    bool    `json:"parserHealthy"`
	LastError        string  `json:"lastError,omitempty"`
	DegradedReason   string  `json:"degradedReason,omitempty"`
	SamplingFails    int     `json:"samplingFails"`
	PendingApprovals int     `json:"pendingApprovals"`
}

// HandleDiagnostic handles GET /debug/diag and returns a redacted diagnostic snapshot.
func (h *Handlers) HandleDiagnostic(w http.ResponseWriter, r *http.Request) {
	snap := DiagnosticSnapshot{
		Daemon: DaemonDiag{
			Uptime:    time.Since(startTime).Round(time.Second).String(),
			GoVersion: runtime.Version(),
		},
	}

	// Adapter health from Registry.
	for _, a := range h.Registry.Adapters() {
		ad := AdapterDiag{Name: a.Name(), Healthy: true}
		as, _ := h.Registry.Snapshot(a.Name())
		if as.LastError != nil {
			ad.Healthy = false
			ad.LastError = redactStr(as.LastError.Error())
		}
		snap.Adapters = append(snap.Adapters, ad)
	}
	snap.Daemon.AdapterCount = len(snap.Adapters)

	// Session diagnostics from Telemetry + ApprovalStore.
	if h.Telemetry != nil {
		for _, st := range h.Telemetry.Snapshot(h.Registry) {
			sd := SessionDiag{
				ID:               st.ID,
				Adapter:          st.Adapter,
				AgentKind:        st.AgentKind,
				AgentStatus:      st.AgentStatus,
				AgentConfidence:  st.AgentConfidence,
				State:            st.State,
				ParserHealthy:    true,
				LastError:        redactStr(st.LastError),
			}

			// Parser health from internal state.
			if ss := h.Telemetry.sessionState(st.ID); ss != nil {
				sd.SamplingFails = ss.SamplingFailures
				sd.ParserHealthy = ss.SamplingFailures <= 2
				if ss.SamplingFailures > 0 {
					sd.DegradedReason = "sampling_failure"
				}
			}

			// Pending approvals.
			if h.Approvals != nil {
				for _, a := range h.Approvals.List(st.ID) {
					if a.Status == "pending" {
						sd.PendingApprovals++
					}
				}
			}

			snap.Sessions = append(snap.Sessions, sd)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(snap)
}

// sessionState returns the internal session state for diagnostics.
func (s *TelemetryService) sessionState(id string) *sessionStateData {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions[id]
}

// redactStr truncates and strips raw paths from diagnostic strings.
func redactStr(s string) string {
	if s == "" {
		return ""
	}
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	s = strings.ReplaceAll(s, "/Users/", "<HOME>/")
	s = strings.ReplaceAll(s, "/home/", "<HOME>/")
	return s
}
