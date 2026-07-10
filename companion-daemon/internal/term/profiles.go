package term

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// M1 — Daemon-owned launch profiles.
//
// The daemon owns executable policy. Mobile owns only presentation (color,
// order). Executable paths are resolved server-side via $PATH and are NEVER
// returned to clients — the public shape is only {id, label, available}. This
// keeps remote callers from discovering or choosing arbitrary binaries.

type sessionProfile struct {
	id      string
	label   string
	resolve func() (executable string, args []string, available bool)
}

// publicProfile is the JSON returned by GET /api/session-profiles.
type publicProfile struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Available bool   `json:"available"`
}

// builtinProfiles are the daemon-owned presets. Shell resolves the user's
// login shell (safe fallback to /bin/bash); codex/claude resolve their CLIs on
// $PATH. Availability reflects whether the executable is actually installed.
func builtinProfiles() []sessionProfile {
	return []sessionProfile{
		{id: "shell", label: "Shell", resolve: func() (string, []string, bool) {
			sh := os.Getenv("SHELL")
			if sh == "" {
				sh = "/bin/bash"
			}
			if filepath.IsAbs(sh) {
				if fi, err := os.Stat(sh); err == nil && !fi.IsDir() {
					return sh, nil, true
				}
			}
			if p, err := exec.LookPath(sh); err == nil {
				return p, nil, true
			}
			if p, err := exec.LookPath("bash"); err == nil {
				return p, nil, true
			}
			return "", nil, false
		}},
		{id: "codex", label: "Codex", resolve: func() (string, []string, bool) {
			if p, err := exec.LookPath("codex"); err == nil {
				return p, nil, true
			}
			return "", nil, false
		}},
		{id: "claude", label: "Claude", resolve: func() (string, []string, bool) {
			if p, err := exec.LookPath("claude"); err == nil {
				return p, nil, true
			}
			return "", nil, false
		}},
	}
}

// ResolveProfile maps a profile id to an executable + argv resolved by daemon
// policy. ok=false when the id is unknown; available=false when the id is
// known but the executable is not installed.
func ResolveProfile(id string) (executable string, args []string, available, ok bool) {
	for _, p := range builtinProfiles() {
		if p.id == id {
			exe, a, avail := p.resolve()
			return exe, a, avail, true
		}
	}
	return "", nil, false, false
}

// ProfileLabel returns the display label for a known profile id, or "" if the
// id is unknown. Used to derive a default display name when the client omits
// one (the New Session name field is optional; the daemon supplies a default).
func ProfileLabel(id string) string {
	for _, p := range builtinProfiles() {
		if p.id == id {
			return p.label
		}
	}
	return ""
}

// HandleSessionProfiles (GET) returns the daemon-owned launch presets. Auth is
// applied by the router middleware. Executable paths are not exposed.
func HandleSessionProfiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	profiles := builtinProfiles()
	out := make([]publicProfile, 0, len(profiles))
	for _, p := range profiles {
		_, _, avail := p.resolve()
		out = append(out, publicProfile{ID: p.id, Label: p.label, Available: avail})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// validateCWD checks a client-provided working directory. Empty means "inherit
// the daemon's working directory". A non-empty value must be an absolute path
// to an existing directory — this prevents relative-path surprises and
// launching in a non-existent location.
func validateCWD(cwd string) error {
	if cwd == "" {
		return nil
	}
	if !filepath.IsAbs(cwd) {
		return fmt.Errorf("cwd must be an absolute path")
	}
	fi, err := os.Stat(cwd)
	if err != nil {
		return fmt.Errorf("cwd does not exist")
	}
	if !fi.IsDir() {
		return fmt.Errorf("cwd is not a directory")
	}
	return nil
}

// validateSessionName rejects empty or control/path-bearing names. Display
// names are cosmetic; keep them printable and free of separators that could
// confuse canonical IDs or logs.
func validateSessionName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("name is required")
	}
	if len(name) > 64 {
		return fmt.Errorf("name too long")
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("name contains control characters")
		}
	}
	if strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("name must not contain path separators")
	}
	return nil
}
