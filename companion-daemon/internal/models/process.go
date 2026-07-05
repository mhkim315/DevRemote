package models

import "time"

// ProcessInfo holds runtime information about the active agent process
type ProcessInfo struct {
	PID       int
	CWD       string
	Command   string
	StartedAt time.Time
	PaneID    string
}
