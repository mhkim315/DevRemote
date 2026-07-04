package parsers

import "devremote/companion-daemon/internal/models"

// Parser defines the interface for parsing agent log lines into AgentEvents.
type Parser interface {
	ParseLine(line string) (*models.AgentEvent, error)
}
