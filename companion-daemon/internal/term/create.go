package term

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"time"

	"devremote/companion-daemon/internal/mux"
)

// createSessionRequest is the union of the legacy CLI payload and the M1 safe
// create payload. `command` is a RawMessage so it can be a legacy shell string
// ("bash") or an M1 custom command object ({"executable":...,"args":[...]}).
type createSessionRequest struct {
	// Legacy CLI (`pokit run`) fields.
	ID          string          `json:"id"`
	WorkspaceID string          `json:"workspaceId"`
	Runner      string          `json:"runner"`
	RunnerColor string          `json:"runnerColor"`
	Command     json.RawMessage `json:"command"`
	CWD         string          `json:"cwd"`
	// M1 safe-create fields.
	Adapter   string `json:"adapter"`
	ProfileID string `json:"profileId"`
	Name      string `json:"name"`
}

// customCommand is the argv form of a custom launch. There is intentionally no
// shell string — the executable is exec'd directly with args.
type customCommand struct {
	Executable string   `json:"executable"`
	Args       []string `json:"args"`
}

// createFromProfile implements the M1 safe-create path: the daemon owns the
// executable policy and generates the canonical ID. The client only chooses a
// profile (or, locally, a custom argv), a display name, and a CWD.
func (h *Handlers) createFromProfile(w http.ResponseWriter, r *http.Request, req createSessionRequest) {
	// MVP scope: only controlled_pty sessions are created via this contract.
	adapter := req.Adapter
	if adapter == "" {
		adapter = "controlled_pty"
	}
	if adapter != "controlled_pty" {
		http.Error(w, "only controlled_pty sessions can be created via profile", http.StatusBadRequest)
		return
	}
	if err := validateSessionName(req.Name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := validateCWD(req.CWD); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var executable string
	var args []string
	if req.ProfileID == "custom" {
		// Custom argv execution is disabled for remote callers by default; only
		// a local (--insecure-local-only) daemon accepts it.
		if !h.InsecureLocalOnly {
			http.Error(w, "custom commands are not permitted for remote callers", http.StatusForbidden)
			return
		}
		cmd, err := parseCustomCommand(req.Command)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resolved, lerr := exec.LookPath(cmd.Executable)
		if lerr != nil {
			http.Error(w, "executable not found", http.StatusBadRequest)
			return
		}
		executable = resolved
		args = cmd.Args
	} else {
		exe, a, available, ok := ResolveProfile(req.ProfileID)
		if !ok {
			http.Error(w, "unknown profile", http.StatusBadRequest)
			return
		}
		if !available {
			http.Error(w, "profile executable not installed", http.StatusBadRequest)
			return
		}
		executable = exe
		args = a
	}

	// Daemon-generated canonical ID — the client never constructs it.
	localID := fmt.Sprintf("%s-%d", req.ProfileID, time.Now().UnixNano())
	opts := mux.CreateOptions{
		Name:        localID,
		WorkspaceID: req.WorkspaceID,
		CWD:         req.CWD,
		Executable:  executable,
		Args:        args,
	}
	createdID, err := h.Registry.CreateSession(r.Context(), adapter, opts)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to create session: %v", err), http.StatusInternalServerError)
		return
	}
	canonicalID := mux.SessionRef{Adapter: adapter, LocalID: createdID}.Canonical()

	// Initialize the Recorder before the session is considered running, so the
	// single-reader capture path is ready when viewers subscribe.
	h.startRecorderForSession(r.Context(), canonicalID)

	writeLifecycle(w, SessionLifecycle{
		ID:        canonicalID,
		Adapter:   adapter,
		ProfileID: req.ProfileID,
		Name:      req.Name,
		State:     LifecycleRunning,
	})
}

// parseCustomCommand decodes and validates a custom argv command.
func parseCustomCommand(raw json.RawMessage) (customCommand, error) {
	var cmd customCommand
	if len(raw) == 0 {
		return cmd, fmt.Errorf("custom command requires an executable")
	}
	if err := json.Unmarshal(raw, &cmd); err != nil {
		return cmd, fmt.Errorf("invalid custom command: %v", err)
	}
	if cmd.Executable == "" {
		return cmd, fmt.Errorf("custom command requires an executable")
	}
	return cmd, nil
}

// startRecorderForSession starts the single Recorder for a freshly created
// session so viewers can subscribe. No-op if activity storage is disabled or
// the session does not support streaming.
func (h *Handlers) startRecorderForSession(ctx context.Context, canonicalID string) {
	if h.Activity == nil {
		return
	}
	if sess, err := h.Registry.FindSession(ctx, canonicalID); err == nil {
		if opener, ok := sess.(mux.StreamOpener); ok {
			EnsureRecorder(canonicalID, opener, h.Activity)
		}
	}
}

// writeLifecycle writes a SessionLifecycle DTO as the JSON response.
func writeLifecycle(w http.ResponseWriter, lc SessionLifecycle) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(lc)
}
