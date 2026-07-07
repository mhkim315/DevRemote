package agent

// AgentParser converts raw log lines into normalized AgentEvents.
// Implementations must be safe for concurrent use across different sessions.
type AgentParser interface {
	// ParseBatch processes raw JSONL lines and returns normalized events.
	// cursor is an opaque position token from a previous ParseBatch call
	// (empty string = start from beginning). The returned newCursor must be
	// safe to pass to the next ParseBatch call for the same log file.
	// Implementations must not panic. On unrecoverable input, return the
	// best-effort events and set degraded=true.
	ParseBatch(lines [][]byte, cursor string) (events []AgentEvent, newCursor string, err error)

	// AgentKind returns the stable agent identifier this parser handles.
	AgentKind() string
}
