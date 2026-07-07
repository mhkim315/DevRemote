package term

import (
	"encoding/json"
	"net/http"
	"regexp"
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
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

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
				ID:              st.ID,
				Adapter:         st.Adapter,
				AgentKind:       st.AgentKind,
				AgentStatus:     st.AgentStatus,
				AgentConfidence: st.AgentConfidence,
				State:           st.State,
				ParserHealthy:   true,
				LastError:       redactStr(st.LastError),
			}

			// Parser health from internal state (safe copy, not pointer).
			if ss, ok := h.Telemetry.sessionStateSnapshot(st.ID); ok {
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

// sessionStateSnapshot returns a safe copy of internal session state for diagnostics.
// It does not expose the internal pointer after releasing the lock.
func (s *TelemetryService) sessionStateSnapshot(id string) (sessionStateData, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data := s.sessions[id]
	if data == nil {
		return sessionStateData{}, false
	}
	return *data, true
}

// Redaction patterns for diagnostic output safety.
var (
	// Home directory paths: /Users/<user>/... or /home/<user>/... or C:\Users\<user>\...
	redactHomeRE = regexp.MustCompile(`(?i)(/Users/|/home/|\\Users\\)[^/\\]+`)

	// API key / token patterns.
	redactSkRE    = regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{8,}\b`)
	redactGhpRE   = regexp.MustCompile(`\bghp_[A-Za-z0-9]{20,}\b`)
	redactXoxRE   = regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}\b`)
	redactBearerRE = regexp.MustCompile(`(?i)\b[Bb]earer\s+[A-Za-z0-9._\-+=/]{8,}\b`)

	// Named secret/value patterns: key=value, token=value, etc.
	redactKeyValRE = regexp.MustCompile(`(?i)\b(token|api_?key|secret|key|password|auth)=\s*[^\s,;)]+`)
)

// redactStr truncates and strips raw paths, usernames, and sensitive data from diagnostic strings.
func redactStr(s string) string {
	if s == "" {
		return ""
	}

	// Strip username from home paths: /Users/mhk/project → <HOME>/project
	s = redactHomeRE.ReplaceAllString(s, "<HOME>")

	// Strip any remaining absolute paths.
	s = strings.ReplaceAll(s, "/Users/", "<HOME>/")
	s = strings.ReplaceAll(s, "/home/", "<HOME>/")

	// Redact API keys and tokens.
	s = redactSkRE.ReplaceAllString(s, "sk-<REDACTED>")
	s = redactGhpRE.ReplaceAllString(s, "ghp_<REDACTED>")
	s = redactXoxRE.ReplaceAllString(s, "xox-<REDACTED>")
	s = redactBearerRE.ReplaceAllString(s, "Bearer <REDACTED>")

	// Redact Authorization: header values — match full "Authorization: Bearer <token>" or "Authorization: Basic <token>".
	s = regexp.MustCompile(`(?i)Authorization:\s*\S+(\s+\S+)?`).ReplaceAllString(s, "Authorization: <REDACTED>")

	// Redact key=value secrets.
	s = redactKeyValRE.ReplaceAllString(s, "${1}=<REDACTED>")

	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
}
