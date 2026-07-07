package agent

// AgentParser converts a single raw log line (JSONL) into a normalized
// common AgentEvent. Parsers must not panic. On unparseable or malformed
// input, return (nil, nil) — the caller treats this as "skip this record".
// On unexpected but recoverable input (unknown fields, missing fields),
// return the best-effort event with lower confidence or EventUnknown.
type AgentParser interface {
	Parse(line []byte) (*AgentEvent, error)
}
