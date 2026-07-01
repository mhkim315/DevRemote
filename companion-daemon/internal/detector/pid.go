package detector

import (
	"os"
	"path/filepath"

	"devremote/companion-daemon/internal/watcher"
)

// FindClaudePIDs returns PIDs of processes writing to .jsonl files in the given directory.
func FindClaudePIDs(jsonlDir string) map[string]int {
	result := make(map[string]int)

	entries, err := os.ReadDir(jsonlDir)
	if err != nil {
		return result
	}

	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".jsonl" {
			continue
		}

		sessionID := e.Name()[:len(e.Name())-6] // strip .jsonl
		pid := findPIDForFile(filepath.Join(jsonlDir, e.Name()))
		if pid > 0 {
			result[sessionID] = pid
		}
	}

	return result
}

// PIDToSession maps discovered PIDs to session IDs and updates RawEvent.
func PIDToSession(pids map[string]int, ev *watcher.RawEvent) {
	if pid, ok := pids[ev.SessionID]; ok && pid > 0 {
		ev.PID = pid
	}
}
