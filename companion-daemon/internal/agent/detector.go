package agent

import "time"

// LogRef points to a discovered agent log file.
type LogRef struct {
	Path     string           // absolute path to the log file
	Type     AgentEventSource // jsonl, log_file, or screen
	Agent    string           // hinted agent kind (may be empty if unknown)
	Size     int64            // file size in bytes, -1 if unknown
	Modified time.Time        // last modification time
}

// LogResolver discovers agent log files associated with a session.
// Implementations inspect process info, cwd, known paths, and terminal
// backend to produce log references. Failure to resolve logs must not
// prevent terminal session from functioning.
type LogResolver interface {
	// Resolve returns discovered log references for a session.
	// Returns empty slice (not error) when no logs are found.
	// Only returns error on unrecoverable conditions (permission denied, etc).
	Resolve(evidence DetectionEvidence) ([]LogRef, error)
}

// DetectionEvidence aggregates observable signals used to identify
// the agent running in a session.
type DetectionEvidence struct {
	ProcessName string   // e.g. "claude", "codex", "node"
	ProcessArgs []string // command line arguments
	CWD         string   // current working directory
	TermAdapter string   // terminal backend name (tmux, cmux, localpty)
	LogPaths    []LogRef // pre-resolved logs (from LogResolver)
	ScreenText  string   // recent terminal screen content (may be empty)
}

// AgentDetector identifies the agent from aggregated evidence.
// Returns low-confidence results rather than false positives.
type AgentDetector interface {
	// Detect returns the best-guess agent identity with confidence.
	// Confidence < 0.5 should result in AgentKind="unknown".
	// Must not panic on empty or partial evidence.
	Detect(evidence DetectionEvidence) AgentIdentity
}
