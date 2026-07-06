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
	LocalID string // renamed from RawID — this is the local ID within the adapter
	RawID   string // deprecated alias for LocalID, kept for backward compat
}

// ParseSessionID splits a compound session ID at the first ':'.
// Adapter is the prefix, LocalID is everything after the first ':'.
func ParseSessionID(id string) SessionRef {
	parts := strings.SplitN(id, ":", 2)
	if len(parts) == 2 {
		return SessionRef{Adapter: parts[0], LocalID: parts[1], RawID: parts[1]}
	}
	return SessionRef{Adapter: "", LocalID: id, RawID: id}
}

// Canonical returns the canonical form: "<adapter>:<local-id>".
func (r SessionRef) Canonical() string {
	if r.Adapter == "" {
		return r.LocalID
	}
	return r.Adapter + ":" + r.LocalID
}

// String returns the canonical form.
func (r SessionRef) String() string { return r.Canonical() }

// Validate checks that the adapter name and local ID are non-empty and
// contain no control characters. Returns nil if valid.
func (r SessionRef) Validate() error {
	if r.Adapter == "" {
		return fmt.Errorf("%w: adapter name is empty", ErrInvalidSessionID)
	}
	if r.LocalID == "" {
		return fmt.Errorf("%w: local ID is empty", ErrInvalidSessionID)
	}
	if strings.ContainsAny(r.Adapter, "\x00\x1f\x7f") {
		return fmt.Errorf("%w: adapter name contains control characters", ErrInvalidSessionID)
	}
	if strings.ContainsAny(r.LocalID, "\x00\x1f\x7f") {
		return fmt.Errorf("%w: local ID contains control characters", ErrInvalidSessionID)
	}
	return nil
}
