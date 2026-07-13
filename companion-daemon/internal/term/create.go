package term

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"devremote/companion-daemon/internal/mux"
	"devremote/companion-daemon/internal/transcript"
)

// createSessionRequest is the HTTP POST /api/sessions body. HTTP creation is
// preset-only: `command` (legacy shell string) and custom argv are NOT executed
// over HTTP — those are privileged local operations (see the 0600 socket create
// op). `command` is kept only so a legacy CLI-shaped HTTP request can be
// detected and rejected rather than silently misinterpreted.
type createSessionRequest struct {
	ID          string          `json:"id"`
	WorkspaceID string          `json:"workspaceId"`
	Runner      string          `json:"runner"`
	RunnerColor string          `json:"runnerColor"`
	Command     json.RawMessage `json:"command"`
	CWD         string          `json:"cwd"`
	Adapter     string          `json:"adapter"`
	ProfileID   string          `json:"profileId"`
	Name        string          `json:"name"`
}

// createFromProfile is the HTTP safe-create path. It is PRESET-ONLY: the
// tunnel-reachable HTTP listener must never execute arbitrary commands. Custom
// argv and legacy command strings are rejected here and are only available on
// the privileged local socket. InsecureLocalOnly/RemoteAddr are NOT used as
// locality proof — HTTP simply cannot select an arbitrary executable.
func (h *Handlers) createFromProfile(w http.ResponseWriter, r *http.Request, req createSessionRequest) {
	adapter := req.Adapter
	if adapter == "" {
		adapter = "controlled_pty"
	}
	if adapter != "controlled_pty" {
		http.Error(w, "only controlled_pty sessions can be created via profile", http.StatusBadRequest)
		return
	}
	if req.ProfileID == "custom" {
		http.Error(w, "custom commands are only available via the local pokit CLI", http.StatusForbidden)
		return
	}
	// M3a: empty name is acceptable — the daemon derives a default from the
	// profile label (e.g. "Shell"), so the mobile New Session surface can treat
	// the display name as optional while the daemon contract always returns one.
	if req.Name == "" {
		req.Name = ProfileLabel(req.ProfileID)
	}
	if err := validateSessionName(req.Name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := validateCWD(req.CWD); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	exe, args, available, ok := ResolveProfile(req.ProfileID)
	if !ok {
		http.Error(w, "unknown profile", http.StatusBadRequest)
		return
	}
	if !available {
		http.Error(w, "profile executable not installed", http.StatusBadRequest)
		return
	}

	opts := mux.CreateOptions{
		Name:        genLocalID(req.ProfileID),
		WorkspaceID: req.WorkspaceID,
		CWD:         req.CWD,
		Executable:  exe,
		Args:        args,
	}
	canonicalID, rec, err := createControlledSession(r.Context(), h.Registry, h.Activity, opts)
	if err != nil {
		// Never expose running on startup failure; the runtime was cleaned up.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(SessionLifecycle{Adapter: adapter, ProfileID: req.ProfileID, Name: req.Name, State: LifecycleFailed})
		return
	}
		// T3: register managed launch binding for accepted adapters.
		if req.ProfileID == "codex" || req.ProfileID == "claude" {
			transcript.RegisterLaunch(canonicalID, req.ProfileID, adapter, 1)
		}

	// M2: catalog the managed session + start its exit watcher on the exact
	// Recorder we just started.
	if h.Lifecycle != nil {
		h.Lifecycle.Register(canonicalID, adapter, req.ProfileID, req.Name, rec)
	}
	writeLifecycle(w, SessionLifecycle{
		ID:        canonicalID,
		Adapter:   adapter,
		ProfileID: req.ProfileID,
		Name:      req.Name,
		State:     LifecycleRunning,
	})
}

// localCreateSpec is a privileged create request accepted only over the 0600
// Unix socket (used by `pokit run`). It supports presets, custom argv, and the
// legacy shell command string — all safe here because the socket is local-only
// and not forwarded through the tunnel.
type localCreateSpec struct {
	ProfileID  string
	Name       string
	CWD        string
	Executable string
	Args       []string
	Command    json.RawMessage // strict: a JSON string, or absent
}

// toOptions validates the spec and resolves it to CreateOptions. Malformed
// input (e.g. a non-string command) is rejected — it is never coerced into a
// default shell.
func (spec localCreateSpec) toOptions() (mux.CreateOptions, error) {
	if err := validateCWD(spec.CWD); err != nil {
		return mux.CreateOptions{}, err
	}
	prefix := spec.ProfileID
	if prefix == "" || prefix == "custom" {
		prefix = "run"
	}
	opts := mux.CreateOptions{Name: genLocalID(prefix), CWD: spec.CWD}

	switch {
	case spec.ProfileID != "" && spec.ProfileID != "custom":
		exe, args, available, ok := ResolveProfile(spec.ProfileID)
		if !ok {
			return opts, fmt.Errorf("unknown profile")
		}
		if !available {
			return opts, fmt.Errorf("profile executable not installed")
		}
		opts.Executable = exe
		opts.Args = args
	case spec.Executable != "":
		resolved, err := exec.LookPath(spec.Executable)
		if err != nil {
			return opts, fmt.Errorf("executable not found")
		}
		opts.Executable = resolved
		opts.Args = spec.Args
	default:
		// Legacy shell command string. Decode strictly: reject missing,
		// non-string, or empty values rather than falling back to a shell.
		if len(spec.Command) == 0 {
			return opts, fmt.Errorf("command is required")
		}
		var cmdStr string
		if err := json.Unmarshal(spec.Command, &cmdStr); err != nil {
			return opts, fmt.Errorf("command must be a string")
		}
		if strings.TrimSpace(cmdStr) == "" {
			return opts, fmt.Errorf("command must not be empty")
		}
		opts.Command = cmdStr
	}
	return opts, nil
}

// createLocalControlled runs a privileged local create and returns the
// canonical ID and lifecycle state. Called from the 0600 socket handler.
func createLocalControlled(ctx context.Context, reg *mux.Registry, activity *ActivityBuffer, lifecycle *LifecycleService, spec localCreateSpec) (string, LifecycleState, error) {
	opts, err := spec.toOptions()
	if err != nil {
		return "", LifecycleFailed, err
	}
	canonicalID, rec, err := createControlledSession(ctx, reg, activity, opts)
	if err != nil {
		return "", LifecycleFailed, err
	}
	if lifecycle != nil {
		lifecycle.Register(canonicalID, "controlled_pty", spec.ProfileID, spec.Name, rec)
	}
	return canonicalID, LifecycleRunning, nil
}

// createControlledSession creates a controlled_pty session and proves its
// Recorder is ready before the session may be exposed as running. On readiness
// failure it terminates the just-created runtime so no unrecorded live process
// is left behind. Returns the exact Recorder so the lifecycle watcher observes
// the real one (avoids a fast-exit race where GetRecorder is already nil).
func createControlledSession(ctx context.Context, reg *mux.Registry, activity *ActivityBuffer, opts mux.CreateOptions) (string, *Recorder, error) {
	createdID, err := reg.CreateSession(ctx, "controlled_pty", opts)
	if err != nil {
		return "", nil, err
	}
	canonicalID := mux.SessionRef{Adapter: "controlled_pty", LocalID: createdID}.Canonical()
	rec, rerr := startRecorder(ctx, reg, activity, canonicalID)
	if rerr != nil {
		_ = reg.TerminateSession(ctx, "controlled_pty", createdID)
		DeleteRecorder(canonicalID)
		return "", nil, fmt.Errorf("recorder not ready: %w", rerr)
	}
	return canonicalID, rec, nil
}

// startRecorder starts the single Recorder for a freshly created session and
// proves it is ready (session found, stream openable, recorder alive). It
// unsubscribes the starter subscriber that EnsureRecorder returns so a
// create-without-viewer leaves no retained phantom subscription, and returns
// the Recorder for the lifecycle watcher.
func startRecorder(ctx context.Context, reg *mux.Registry, activity *ActivityBuffer, canonicalID string) (*Recorder, error) {
	if activity == nil {
		return nil, fmt.Errorf("activity storage not configured")
	}
	sess, err := reg.FindSession(ctx, canonicalID)
	if err != nil {
		return nil, fmt.Errorf("session not found after create: %w", err)
	}
	opener, ok := sess.(mux.StreamOpener)
	if !ok {
		return nil, fmt.Errorf("session does not support live streaming")
	}
	rec, subCh := EnsureRecorder(canonicalID, opener, activity)
	if rec == nil {
		return nil, fmt.Errorf("recorder failed to start (stream unavailable)")
	}
	// No viewer yet — do not retain the starter subscription.
	rec.Unsubscribe(subCh)
	return rec, nil
}

// genLocalID generates a unique daemon-owned local session id. Clients never
// construct canonical IDs.
func genLocalID(prefix string) string {
	p := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, prefix)
	if p == "" {
		p = "run"
	}
	return fmt.Sprintf("%s-%d", p, time.Now().UnixNano())
}

// writeLifecycle writes a SessionLifecycle DTO as the JSON response.
func writeLifecycle(w http.ResponseWriter, lc SessionLifecycle) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(lc)
}
