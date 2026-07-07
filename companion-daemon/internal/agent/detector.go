package agent

import "time"

// LogRef points to a discovered agent log file.
// Path is internal-only; DisplayPath is safe for diagnostics and mobile.
type LogRef struct {
	Path        string           // absolute path (internal, never exposed to API/mobile)
	DisplayPath string           // redacted display path (safe for diagnostics/API)
	Type        AgentEventSource // jsonl, log_file, or screen
	Agent       string           // hinted agent kind (may be empty if unknown)
	Size        int64            // file size in bytes, -1 if unknown
	Modified    time.Time        // last modification time
}

// ResolveResult holds the output of LogResolver.Resolve.
// Failure to resolve (permission denied, missing path, stale log)
// is indicated via Degraded+Diagnostics, not via error.
// Only truly unrecoverable conditions (e.g. nil evidence) return error.
type ResolveResult struct {
	Logs        []LogRef // discovered log references
	Diagnostics []string // human-readable issues (non-empty only when Degraded)
	Degraded    bool     // true if resolution ran in degraded mode
}

// LogResolver discovers agent log files associated with a session.
type LogResolver interface {
	// Resolve returns discovered log references. Permission denied,
	// missing paths, and stale logs set Degraded=true with diagnostics.
	// Only returns error on truly unrecoverable conditions.
	Resolve(evidence DetectionEvidence) (ResolveResult, error)
}

// ManualEvidence represents a user-provided agent link.
// Has the highest priority among all evidence sources.
type ManualEvidence struct {
	AgentKind string // manually linked agent kind (claude, codex, etc.)
	LogPath   string // manually linked log path (may be empty)
}

// DetectionEvidence aggregates observable signals used to identify
// the agent running in a session.
type DetectionEvidence struct {
	ProcessName string          // e.g. "claude", "codex", "node"
	ProcessArgs []string        // command line arguments
	CWD         string          // current working directory
	TermAdapter string          // terminal backend name (tmux, cmux, localpty)
	LogPaths    []LogRef        // pre-resolved logs (from LogResolver)
	ScreenText  string          // recent terminal screen content (may be empty)
	ManualLink  *ManualEvidence // user-provided agent link (highest priority)
}

// AgentDetector identifies the agent from aggregated evidence.
// Returns low-confidence unknown rather than false positives.
// Confidence < 0.5 MUST result in AgentKind="unknown".
type AgentDetector interface {
	// Detect returns the best-guess agent identity with confidence.
	// Confidence < 0.5 → AgentKind="unknown".
	// Must not panic on empty or partial evidence.
	Detect(evidence DetectionEvidence) AgentIdentity
}
