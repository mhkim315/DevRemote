package parsers

import (
	"devremote/companion-daemon/internal/term"
)

// EventParser defines the interface for parsing agent log lines into AgentEvents.
type EventParser interface {
	ParseLine(line string) (*term.AgentEvent, error)
}
