package mux

import (
	"fmt"
	"strings"
)

// SessionRef decomposes a compound canonical session ID (e.g. "tmux:devremote")
// into its adapter and local ID components. This is the single canonical form;
// handler/mobile must use this type instead of string split or regex.
type SessionRef struct {
	Adapter string
	LocalID string
}

// ParseSessionID splits a compound session ID at the first ':'.
// Adapter is the prefix, LocalID is everything after the first ':'.
func ParseSessionID(id string) SessionRef {
	parts := strings.SplitN(id, ":", 2)
	if len(parts) == 2 {
		return SessionRef{Adapter: parts[0], LocalID: parts[1]}
	}
	return SessionRef{Adapter: "", LocalID: id}
}

// Canonical returns the canonical form: "<adapter>:<local-id>".
func (r SessionRef) Canonical() string {
	if r.Adapter == "" {
		return r.LocalID
	}
	return r.Adapter + ":" + r.LocalID
}

func (r SessionRef) String() string { return r.Canonical() }

// Validate checks that the adapter name and local ID are non-empty and
// contain no control characters (including newline, tab). Returns nil if valid.
func (r SessionRef) Validate() error {
	if r.Adapter == "" {
		return fmt.Errorf("%w: adapter name is empty", ErrInvalidSessionID)
	}
	if r.LocalID == "" {
		return fmt.Errorf("%w: local ID is empty", ErrInvalidSessionID)
	}
	for _, ch := range r.Adapter {
		if ch < 0x20 || ch == 0x7f {
			return fmt.Errorf("%w: adapter name contains control character U+%04X", ErrInvalidSessionID, ch)
		}
	}
	for _, ch := range r.LocalID {
		if ch < 0x20 || ch == 0x7f {
			return fmt.Errorf("%w: local ID contains control character U+%04X", ErrInvalidSessionID, ch)
		}
	}
	return nil
}
