package term

import (
	"encoding/json"

	"devremote/companion-daemon/internal/models"
)

// LogCursor manages incremental reading of large JSONL files
type LogCursor struct {
	Path       string
	Offset     int64
	Inode      uint64
	Discarding bool
}

// AgentLogParser parses lines from the log into uniform AgentEvent structures
type AgentLogParser interface {
	Parse(record json.RawMessage) ([]models.AgentEvent, error)
}
