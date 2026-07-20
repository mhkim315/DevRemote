package agent

// PB.2b: Process discovery, external raw-log observation, and legacy agent
// detection from process/screen evidence are removed. Agent identity for
// managed Codex/Claude sessions comes from provider-native JSONL events,
// not from process scanning or screen analysis.
//
// Retained types are kept for agent adapter implementations that remain
// compiled but are no longer wired to production paths. They are inert
// and will be physically removed in a later wave when all consumers are
// migrated.

// DetectionEvidence aggregates observable signals used to identify
// the agent running in a session. Retained for compilation compatibility;
// managed Codex/Claude paths do not use it.
type DetectionEvidence struct {
	ProcessName string   // e.g. "claude", "codex", "node"
	ProcessArgs []string // command line arguments
	CWD         string   // current working directory
	TermAdapter string   // terminal backend name
	LogPaths    []LogRef // pre-resolved logs (from LogResolver)
	ScreenText  string   // recent terminal screen content (may be empty)
}

// LogRef points to a discovered agent log file.
type LogRef struct {
	Path        string
	DisplayPath string
	Agent       string
	Session     string
	Type        AgentEventSource
}

// ResolveResult holds the output of LogResolver.Resolve.
type ResolveResult struct {
	Logs        []LogRef
	Degraded    bool
	Diagnostics []string
}

// LogResolver discovers agent log files associated with a session.
type LogResolver interface {
	Resolve(evidence DetectionEvidence) (ResolveResult, error)
}

// AgentDetector identifies the agent from aggregated evidence.
type AgentDetector interface {
	Detect(evidence DetectionEvidence) AgentIdentity
}

// ProdDetectionEvidence is the production-boundary type alias for agent
// detection signals. Retained for compilation; managed paths do not use it.
type ProdDetectionEvidence struct {
	PID         int
	Executable  string
	Args        []string
	CWD         string
	EnvTerm     string
	TermAdapter string
}
