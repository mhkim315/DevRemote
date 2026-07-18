package mux

import "devremote/companion-daemon/internal/sessionid"

// PA2b: the provider-neutral canonical session identity primitives moved to
// internal/sessionid. This file retains ONLY thin aliases/wrappers so existing
// mux callers keep compiling with identical behavior. It contains no parsing
// logic. New managed and term production code must import internal/sessionid
// directly.

// SessionRef decomposes a compound canonical session ID (e.g. "tmux:devremote")
// into its adapter and local ID components.
//
// Deprecated: temporary PA4/PB deletion target — use
// internal/sessionid.SessionRef directly in new code.
type SessionRef = sessionid.SessionRef

// ParseSessionID splits a compound session ID at the first ':'.
// Adapter is the prefix, LocalID is everything after the first ':'.
//
// Deprecated: temporary PA4/PB deletion target — use
// internal/sessionid.ParseSessionID directly in new code.
func ParseSessionID(id string) SessionRef { return sessionid.ParseSessionID(id) }

// ValidateAdapterName checks that an adapter name is a non-empty lower-case ASCII
// identifier matching [a-z][a-z0-9_-]*. Returns ErrInvalidSessionID if invalid.
//
// Deprecated: temporary PA4/PB deletion target — use
// internal/sessionid.ValidateAdapterName directly in new code.
func ValidateAdapterName(name string) error { return sessionid.ValidateAdapterName(name) }
