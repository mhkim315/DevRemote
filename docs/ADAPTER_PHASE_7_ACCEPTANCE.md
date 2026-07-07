# Phase 7 Acceptance — Operations, UX, and Diagnostics

Date: 2026-07-07

Executor commit under review: `71a285b6c`

Verifier decision: **ACCEPT**

## Scope

Phase 7 was not a new adapter phase. The accepted scope was the productization layer
after the Phase 1-6 adapter expansion proof:

- mobile UX must remain capability-driven, not backend-name-driven;
- unavailable, degraded, ended, unsupported, and empty states must be distinguishable;
- operational requirements and diagnostic limits must be documented honestly;
- existing tmux/cmux and Phase 6 LocalPTY behavior must not regress.

## Evidence Reviewed

Primary files reviewed:

- `docs/08-phase7-adapter-ops.md`
- `docs/09-phase7-mobile-smoke.md`
- `companion-daemon/internal/term/phase7_golden_test.go`
- `mobile/src/screens/FeedScreen.tsx`

Phase 7 fixes in `71a285b6c` resolve the remaining mobile blockers from the previous
review:

1. `FeedScreen.tsx` no longer relies on a stale `sessionData` closure for history
   polling. The polling interval checks `sessionDataRef.current`.
2. Sessions without explicit `history` capability are forced back to the terminal
   tab, so the ACTIVITY panel no longer remains visible for LocalPTY/live-only
   sessions.
3. Legacy/no-capabilities behavior is now coherent between code and documentation:
   explicit `history` is required before ACTIVITY/history polling is enabled.

## Acceptance Criteria

### 1. Backend names are not hardcoded into mobile behavior

Accepted.

`docs/09-phase7-mobile-smoke.md` documents that mobile accepts unknown adapter names
as strings and uses `capabilities` for behavior. The reviewed implementation hides or
shows ACTIVITY based on explicit `history`, not on `tmux`, `cmux`, `localpty`, or a
known-backend list.

### 2. Unknown and live-only adapters are safe

Accepted.

For LocalPTY and future live-only adapters with `capabilities: ["live_stream"]`:

- adapter tag can still be displayed;
- terminal WebSocket path remains usable;
- ACTIVITY tab is hidden;
- history polling does not run after the latest session capability snapshot confirms
  no `history` capability.

### 3. Legacy/no-capabilities policy is explicit

Accepted.

The final policy is conservative: no explicit `history` capability means no mobile
ACTIVITY tab and no history polling. This avoids treating omitted capabilities as
implicit support.

### 4. Diagnostics are useful without overstating coverage

Accepted.

The Phase 7 diagnostics tests and docs distinguish:

- empty but healthy session lists;
- adapter refresh failure/unavailable state when at least one other adapter exposes
  sessions;
- ended and unsupported states.

The remaining limitation is documented rather than hidden: `/api/sessions` is a
session list, not an adapter inventory. A completely unavailable adapter with zero
sessions cannot be fully represented through `/api/sessions` alone without an
adapter-level registry/health endpoint.

### 5. Operational documentation exists

Accepted.

`docs/08-phase7-adapter-ops.md` documents backend-specific installation and operating
constraints, including socket/config scope and LocalPTY feature-flag status.

## Verification Commands

Executed from `DevRemote` unless otherwise noted.

```sh
git diff --check
gofmt -l companion-daemon/internal/term/phase7_golden_test.go
```

Both produced no output.

```sh
cd companion-daemon
GOCACHE=/tmp/devremote-phase7-71a285b-go-cache go test ./internal/term -run 'Test(StatusTaxonomy|CapabilityGolden|Diagnostics|MobileLegacy)' -count=10 -v
GOCACHE=/tmp/devremote-phase7-71a285b-go-cache go vet ./...
GOCACHE=/tmp/devremote-phase7-71a285b-go-cache go test ./...
GOCACHE=/tmp/devremote-phase7-71a285b-go-cache go test -race ./...
```

All passed.

```sh
cd mobile
node_modules/.bin/tsc --noEmit
```

Passed.

Note: full Go test and race test require local listener permissions because multiple
tests use `httptest`. The sandboxed run failed with `bind: operation not permitted`;
the same commands passed when executed with the required local listener permission.

## Non-blocking Follow-ups

These are not blockers for Phase 7:

- add an adapter-level health/diagnostics endpoint if product UX needs to show
  zero-session unavailable adapters;
- add dedicated React Native component tests for `FeedScreen` capability transitions
  once the mobile test harness exists;
- continue to treat normal WebSocket EOF/close logging as an operational polish item,
  not an adapter-architecture blocker.

## Conclusion

Phase 7 is accepted.

The adapter expansion proof is now productized enough to move beyond architecture
validation: tmux/cmux remain intact, LocalPTY remains a real third backend behind its
feature flag, and the mobile/diagnostic/ops surface no longer depends on a fixed
backend-name list.
