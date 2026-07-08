# E8 Mobile Connectivity Question

Date: 2026-07-08

## Context

E8 desktop evidence is complete. Next step is mobile WebView validation
(M1). The phone needs to reach the daemon for E8DIAG capture.

## Current state

- Daemon: 127.0.0.1:9171 (--insecure-local-only)
- Emulator: works via adb reverse tcp:9171 tcp:9171
- Phone on WiFi: cannot reach 127.0.0.1
- Phone on LTE: cannot reach daemon
- Cloudflare tunnel: configured (term.fullcount.kr → 127.0.0.1:9171) but not running

## Question

What is the approved method for phone-to-daemon connectivity during
mobile validation?

Options:
- A: adb reverse (requires USB connection)
- B: Cloudflare tunnel (needs verification that tunnel is operational)
- C: New flag like --listen-addr or --dev-bind-all-interfaces (separate from --insecure-local-only)
- D: Other approach

## Constraint

--insecure-local-only must remain 127.0.0.1 bound. This is a security
boundary that should not be weakened for testing convenience.
