package parsers

// EventParser defines the interface for parsing agent log lines into AgentEvents.
type EventParser interface {
	ParseLine(line string) (eventType, summary, detail string, err error)
}
