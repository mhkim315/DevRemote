package agent

// ParseResult holds the complete output of a ParseBatch call.
type ParseResult struct {
	Events      []AgentEvent    // normalized events from this batch
	Cursor      string          // opaque position for next ParseBatch call
	Status      AgentStatus     // best-guess agent status after this batch
	Approvals   []AgentApproval // approval requests detected in this batch
	Degraded    bool            // true if parser ran in degraded mode
	Diagnostics []string        // human-readable parse issues (non-empty only when Degraded)
}

// AgentParser converts raw log lines into normalized agent output.
type AgentParser interface {
	// ParseBatch processes raw JSONL lines and returns a complete result.
	// cursor is an opaque token from a previous call (empty = start from beginning).
	// Implementations must not panic. On unrecoverable input, set Degraded=true
	// and include diagnostic messages.
	ParseBatch(lines [][]byte, cursor string) ParseResult

	// AgentKind returns the stable agent identifier this parser handles.
	AgentKind() string
}
