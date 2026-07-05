package mux

import "strings"

// SessionRef decomposes a compound session ID (e.g. "tmux:devremote")
// into its underlying adapter and raw ID components.
type SessionRef struct {
	Adapter string
	RawID   string
}

// ParseSessionID splits a compound session ID. If no adapter prefix is present,
// it defaults to treating the entire string as the RawID (with an empty Adapter).
func ParseSessionID(id string) SessionRef {
	parts := strings.SplitN(id, ":", 2)
	if len(parts) == 2 {
		return SessionRef{
			Adapter: parts[0],
			RawID:   parts[1],
		}
	}
	return SessionRef{
		Adapter: "",
		RawID:   id,
	}
}
