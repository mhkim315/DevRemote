package term

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

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

	// C1D: route claude profile to the managed Claude service when enabled.
	// Uses canonical claude_headless adapter identity, not controlled_pty.
	if req.ProfileID == "claude" && h.ManagedClaude != nil {
		cwd := req.CWD
		if cwd == "" {
			cwd = "/"
		}
		id, err := h.ManagedClaude.CreateDetached(cwd)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(SessionLifecycle{Adapter: claudeHeadlessAdapter, ProfileID: req.ProfileID, Name: req.Name, State: LifecycleFailed})
			return
		}
		// PA2c: no Lifecycle.Register — the Claude provider service owns its
		// lifecycle record; ManagedRuntimeCatalog is the read path.
		writeLifecycle(w, SessionLifecycle{
			ID:        id,
			Adapter:   claudeHeadlessAdapter,
			ProfileID: req.ProfileID,
			Name:      req.Name,
			State:     LifecycleRunning,
		})
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

	cfg := SpawnConfig{
		Name:       genLocalID(req.ProfileID),
		CWD:        req.CWD,
		Executable: exe,
		Args:       args,
	}
	// PA2c: controlled-PTY creation is owned by OwnedPTYRuntime (which uses the
	// temporary mux spawn seam until PA2d) — creation registers the
	// generation-bound lifecycle record and exit watcher atomically with launch.
	if h.Lifecycle == nil || h.Lifecycle.OwnedPTY() == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(SessionLifecycle{Adapter: adapter, ProfileID: req.ProfileID, Name: req.Name, State: LifecycleFailed})
		return
	}
	canonicalID, err := h.Lifecycle.OwnedPTY().Create(r.Context(), cfg, req.ProfileID, req.Name)
	if err != nil {
		// Never expose running on startup failure; the runtime was cleaned up.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(SessionLifecycle{Adapter: adapter, ProfileID: req.ProfileID, Name: req.Name, State: LifecycleFailed})
		return
	}
	// T3/S1.1-B: register managed launch binding for accepted adapters. The
	// binding captures the daemon-owned child PID + spawn StartedAt (runtime
	// identity) and receives a monotonic launch generation. Registration goes
	// through the atomic replacement boundary (reserve → invalidate → publish) so a
	// same-ID relaunch invalidates prior status/ingestion authority at the reserved
	// generation BEFORE the new binding is observable — no concurrent poll can
	// attribute stale evidence to the new launch.
	if req.ProfileID == "codex" || req.ProfileID == "claude" {
		version := ""
		if req.ProfileID == "codex" {
			version = "0.144.1"
		} else if req.ProfileID == "claude" {
			version = "2.1.202"
		}
		spec := transcript.LaunchSpec{
			SessionID: canonicalID, Provider: req.ProfileID, Adapter: adapter,
			Version: version,
		}
		if h.Telemetry != nil {
			// Production path: the atomic replacement boundary with status
			// invalidation. In production Handlers.Telemetry is always wired.
			h.Telemetry.RegisterOrReplaceLaunch(spec)
		} else {
			// C2: with no telemetry/status wiring there is no safe invalidation, so
			// we must NOT take a replacement path. RegisterFirstLaunch fails closed
			// if a binding already exists rather than silently replacing recognized
			// identity without invalidation.
			transcript.RegisterFirstLaunch(spec)
		}
	}

	// M2/PA2c: the OwnedPTYRuntime cataloged the session and started its exit
	// watcher inside Create — no separate Register step exists.
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

// legacyCodexCommand reports whether a legacy command string is EXACTLY the
// recognized codex invocation. SP0.5 removes its shell fallback: the managed
// structured profile is the only launch path for codex.
func legacyCodexCommand(command json.RawMessage) bool {
	if len(command) == 0 {
		return false
	}
	var s string
	if json.Unmarshal(command, &s) != nil {
		return false
	}
	return strings.TrimSpace(s) == "codex"
}

// validateLaunchInputExclusivity fails closed when a create request selects
// more than one launch source. A real profile id (non-empty, non-"custom"),
// a custom executable, and a legacy command string are mutually exclusive —
// ambiguous input is never silently resolved by precedence. ("custom" is a
// marker for the executable path, not a profile selection.)
func validateLaunchInputExclusivity(profileID, executable string, command json.RawMessage) error {
	selected := 0
	if profileID != "" && profileID != "custom" {
		selected++
	}
	if executable != "" {
		selected++
	}
	if len(command) > 0 {
		selected++
	}
	if selected > 1 {
		return fmt.Errorf("conflicting launch inputs: profile, executable, and command are mutually exclusive")
	}
	return nil
}

// toOptions validates the spec and resolves it to SpawnConfig. Malformed
// input (e.g. a non-string command) is rejected — it is never coerced into a
// default shell.
func (spec localCreateSpec) toOptions() (SpawnConfig, error) {
	if err := validateLaunchInputExclusivity(spec.ProfileID, spec.Executable, spec.Command); err != nil {
		return SpawnConfig{}, err
	}
	if err := validateCWD(spec.CWD); err != nil {
		return SpawnConfig{}, err
	}
	prefix := spec.ProfileID
	if prefix == "" || prefix == "custom" {
		prefix = "run"
	}
	opts := SpawnConfig{Name: genLocalID(prefix), CWD: spec.CWD}

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
// PA2c: creation goes through the OwnedPTYRuntime lifecycle owner.
func createLocalControlled(ctx context.Context, ownedPTY *OwnedPTYRuntime, spec localCreateSpec) (string, LifecycleState, error) {
	opts, err := spec.toOptions()
	if err != nil {
		return "", LifecycleFailed, err
	}
	if ownedPTY == nil {
		return "", LifecycleFailed, ErrLifecycleUnavailable
	}
	canonicalID, err := ownedPTY.Create(ctx, SpawnConfig{Name: opts.Name, Command: opts.Command, Executable: opts.Executable, Args: opts.Args, CWD: opts.CWD}, spec.ProfileID, spec.Name)
	if err != nil {
		return "", LifecycleFailed, err
	}
	return canonicalID, LifecycleRunning, nil
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
