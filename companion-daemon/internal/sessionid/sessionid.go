// Package sessionid provides the provider-neutral canonical session identity
// primitives (PA2b, docs/PA2_LIFECYCLE_TRANSPORT_CONTRACT.md).
//
// This package owns ONLY structural identity: the canonical
// "<adapter>:<local-id>" form, its parsing, serialization, structural
// validation, and the adapter-name grammar. It deliberately excludes legacy
// migration (MigrateLegacyID stays in internal/mux), adapter discovery,
// registry lookup/mutation, approval claim/delivery/consumption logic,
// provider-specific identity validation, authority-version validation, and
// lifecycle generation authority — those remain with their current owners.
//
// internal/sessionid must not import internal/mux (enforced by the PA2b
// forbidden-import gate); internal/mux retains thin deprecated wrappers that
// delegate here.
package sessionid

import (
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidSessionID is the sentinel for structurally invalid session
// identities. internal/mux aliases its ErrInvalidSessionID to this exact
// value, so errors.Is matches through either name.
var ErrInvalidSessionID = errors.New("invalid session ID")

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

// ValidateAdapterName checks that an adapter name is a non-empty lower-case ASCII
// identifier matching [a-z][a-z0-9_-]*. Returns ErrInvalidSessionID if invalid.
func ValidateAdapterName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: adapter name is empty", ErrInvalidSessionID)
	}
	if strings.Contains(name, ":") {
		return fmt.Errorf("%w: adapter name %q contains colon", ErrInvalidSessionID, name)
	}
	for i, ch := range name {
		if ch >= 'a' && ch <= 'z' {
			continue
		}
		if i > 0 && (ch >= '0' && ch <= '9' || ch == '_' || ch == '-') {
			continue
		}
		return fmt.Errorf("%w: adapter name %q contains invalid character %q at position %d", ErrInvalidSessionID, name, ch, i)
	}
	return nil
}
