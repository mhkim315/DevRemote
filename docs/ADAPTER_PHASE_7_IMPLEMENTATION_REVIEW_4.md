# Adapter Phase 7 Implementation Review 4

Date: 2026-07-07

Executor commit: `be5ed8af8`

Verifier decision: **BLOCKED**

Next phase permission: **BLOCKED**

## Scope reviewed

- `mobile/src/screens/FeedScreen.tsx`
- `docs/09-phase7-mobile-smoke.md`
- `companion-daemon/internal/term/phase7_golden_test.go`
- Prior review: `docs/ADAPTER_PHASE_7_IMPLEMENTATION_REVIEW_3.md`

This review evaluated the Phase 7 correction after `561f715a9`.

## Automated verification

```sh
git diff --check
cd companion-daemon && gofmt -l internal/term/phase7_golden_test.go
GOCACHE=/tmp/devremote-phase7-be5ed8a-go-cache go test ./internal/term -run 'Test(StatusTaxonomy|CapabilityGolden|Diagnostics|MobileLegacy)' -count=10 -v
GOCACHE=/tmp/devremote-phase7-be5ed8a-go-cache go vet ./...
GOCACHE=/tmp/devremote-phase7-be5ed8a-go-cache go test ./...
GOCACHE=/tmp/devremote-phase7-be5ed8a-go-cache go test -race ./...
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

- `FeedScreen` now has a `supportsHistory` concept.
- The `ACTIVITY` tab is conditionally rendered when `supportsHistory` is true.
- `docs/09-phase7-mobile-smoke.md` was updated to describe capability-driven
  activity/history behavior.

This is the right direction, but the implementation and documentation still do
not match the intended runtime behavior.

## Blocking findings

### 1. History polling still uses stale `sessionData` from the initial render

`FeedScreen` computes a render-time value:

```tsx
const supportsHistory = !sessionData || sessionData.capabilities?.includes('history') !== false;
```

But the polling effect has dependency list `[session]` only:

```tsx
useEffect(() => {
  ...
  const supportsHistory = sessionData?.capabilities?.includes('history');
  if (supportsHistory !== false) {
    fetchHistory();
  }
  const interval = setInterval(() => {
    fetchSession();
    const hist = sessionData?.capabilities?.includes('history');
    if (hist !== false) {
      fetchHistory();
    }
  }, 3000);
  return () => clearInterval(interval);
}, [session]);
```

Because `sessionData` is not in the dependency list, the interval closure keeps
the value from the render that created the effect. On initial render,
`sessionData` is `null`, so `hist !== false` remains true inside the interval.
After the LocalPTY session is fetched and `sessionData.capabilities` becomes
`["live_stream"]`, the existing interval can still continue history polling.

This does not satisfy the Phase 7 requirement that LocalPTY live-only sessions
stop probing unsupported history.

Expected correction:

- Split polling into separate effects, or include the relevant capability state
  in dependencies and clear/recreate the interval when it changes.
- Prefer deriving a stable boolean such as:

```tsx
const supportsHistory = sessionData?.capabilities?.includes('history') === true;
```

  if the accepted policy is that missing/unknown capabilities do not imply
  history support.
- Ensure `getSessionHistory` is not called after a LocalPTY/future live-only
  payload is loaded.

### 2. Activity view can remain visible after the tab is hidden

`activeTab` defaults to `'activity'`:

```tsx
const [activeTab, setActiveTab] = useState<'terminal' | 'activity'>('activity');
```

The `ACTIVITY` tab button is hidden when `supportsHistory` is false, but the
activity panel is still controlled only by `activeTab`:

```tsx
<View style={[styles.activityContainer, {display: activeTab === 'activity' ? 'flex' : 'none'}]}>
```

If a LocalPTY/future live-only payload loads while `activeTab` is still
`'activity'`, the Activity tab button disappears, but the Activity panel can
remain visible. That is still an unsupported history/activity affordance.

Expected correction:

- When `supportsHistory` becomes false, force `activeTab` to `'terminal'`, or
- render the activity panel only when `supportsHistory && activeTab ===
  'activity'`, with a clear unsupported message if needed.

### 3. Legacy behavior is internally inconsistent between table and implementation

`docs/09-phase7-mobile-smoke.md` says:

```text
legacy [] (omitted) -> ACTIVITY 탭 숨김
```

But the same document says:

```text
capabilities가 undefined(legacy): true → 하위 호환
```

The implementation also treats missing capabilities as history-supported:

```tsx
!sessionData || sessionData.capabilities?.includes('history') !== false
```

For a legacy response with no `capabilities` field, this evaluates to true.
Therefore the table and implementation contradict each other.

Expected correction:

- Decide the compatibility policy:
  - legacy no-capabilities means "unknown, keep old behavior" and Activity
    remains visible, or
  - legacy no-capabilities means "unsupported by default" and Activity is
    hidden.
- Update both `FeedScreen` and `docs/09-phase7-mobile-smoke.md` to match the
  chosen policy.

## Non-blocking observations

- No backend-name allowlist was added.
- No new backend was added.
- No common runtime feature expansion was introduced.
- TypeScript compile passes.

## Verdict

Phase 7 is not accepted at `be5ed8af8`.

The remaining work is in the mobile capability-driven UX. LocalPTY/future
live-only sessions must not continue polling unsupported history, must not
leave the Activity panel visible after history support is known to be absent,
and the legacy no-capabilities policy must be internally consistent.
