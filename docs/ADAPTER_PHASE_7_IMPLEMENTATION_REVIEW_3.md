# Adapter Phase 7 Implementation Review 3

Date: 2026-07-07

Executor commit: `561f715a9`

Verifier decision: **BLOCKED**

Next phase permission: **BLOCKED**

## Scope reviewed

- `companion-daemon/internal/term/phase7_golden_test.go`
- `docs/08-phase7-adapter-ops.md`
- `docs/09-phase7-mobile-smoke.md`
- `mobile/src/components/AgentCard.tsx`
- `mobile/src/screens/FeedScreen.tsx`
- Prior review: `docs/ADAPTER_PHASE_7_IMPLEMENTATION_REVIEW_2.md`

This review evaluated the Phase 7 correction after `744a01a0b`.

## Automated verification

```sh
git diff --check
cd companion-daemon && gofmt -l internal/term/phase7_golden_test.go
GOCACHE=/tmp/devremote-phase7-561f715-go-cache go test ./internal/term -run 'Test(StatusTaxonomy|CapabilityGolden|Diagnostics|MobileLegacy)' -count=10 -v
GOCACHE=/tmp/devremote-phase7-561f715-go-cache go vet ./...
GOCACHE=/tmp/devremote-phase7-561f715-go-cache go test ./...
GOCACHE=/tmp/devremote-phase7-561f715-go-cache go test -race ./...
cd mobile && node_modules/.bin/tsc --noEmit
```

Results:

- `git diff --check`: PASS
- `gofmt -l internal/term/phase7_golden_test.go`: PASS
- Targeted Phase 7 Go tests repeated run: PASS
- `go vet ./...`: PASS
- `go test ./...`: PASS
- `go test -race ./...`: PASS
- mobile TypeScript compile: PASS

Sandbox note:

Full Go tests require local `httptest` listener binding, so they were run with
local listener permission.

## Progress since previous review

Accepted improvements:

- `phase7_golden_test.go` is now gofmt-clean.
- `docs/08-phase7-adapter-ops.md` now honestly documents the known limitation
  that `/api/sessions` is a session list, not an adapter inventory.
- The diagnostics test now includes `known_limitation_zero_sessions`, matching
  the documented limitation.
- `docs/09-phase7-mobile-smoke.md` adds concrete payloads and grep evidence.

These close the mechanical formatting blocker and make the diagnostics story
more truthful.

## Blocking finding

### 1. Mobile LocalPTY live-only behavior is still contradicted by the actual Feed screen

Phase 7 acceptance requires LocalPTY live-only sessions to render without
history/screen affordance confusion.

`docs/09-phase7-mobile-smoke.md` claims:

```text
localpty [live_stream] -> [localpty] tag, WS 연결 가능, history/screen 없음
(버튼 disabled 또는 숨김)
```

But `mobile/src/screens/FeedScreen.tsx` still fetches history regardless of
capabilities:

```tsx
const fetchHistory = () => {
  getSessionHistory(session, token)
    .then(...)
    .catch(err => console.error(err));
};

fetchSession();
fetchHistory();
const interval = setInterval(() => {
  fetchSession();
  fetchHistory();
}, 3000);
```

It also always renders the `ACTIVITY` tab:

```tsx
<TouchableOpacity
  style={[styles.tab, activeTab === 'activity' && styles.activeTab]}
  onPress={() => setActiveTab('activity')}
>
  <Text ...>ACTIVITY</Text>
</TouchableOpacity>
```

For LocalPTY, `history` is unsupported. Therefore the current mobile behavior
still attempts the unsupported history path and shows an activity affordance
without checking `sessionData?.capabilities`.

This is exactly the Phase 7 class of issue: the system architecture supports
capabilities, but the product UI still exposes or probes unsupported behavior.

Expected correction:

- Make `FeedScreen` capability-aware for history/activity, or
- update the mobile smoke document to accurately describe the current behavior
  and classify it as an accepted limitation with clear user-facing semantics.

For final Phase 7 acceptance, the stronger and preferable fix is:

- derive `supportsHistory = sessionData?.capabilities?.includes('history')`;
- avoid polling `getSessionHistory` when unsupported;
- hide/disable the ACTIVITY tab or show a clear "history unsupported for this
  session" message for LocalPTY/legacy/future-live-only payloads;
- document the expected mobile behavior in `docs/09-phase7-mobile-smoke.md`.

## Non-blocking observations

- The capability golden tests are now materially stronger and exact.
- The operations document is useful and no longer misrepresents
  `/api/sessions` as a complete adapter inventory.
- No new backend was added.
- No unapproved common runtime feature was added.

## Verdict

Phase 7 is not accepted at `561f715a9`.

The remaining blocker is the actual mobile UX: LocalPTY live-only sessions must
not keep probing or presenting unsupported history/activity behavior as if it
were generally available.
