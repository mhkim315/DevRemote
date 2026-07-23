# Step 8 Evidence — Mobile Operational Cockpit

**EVID HEAD:** (this commit)
**IMPL SHAs:** 90cc46c3f (store) → 44f98dfcb (handler) → 10432920b (mobile) → 793510e07 (observer) → 0a11ef7e1 (validation store) → 117740d1f (integration) → 4f10ea83f (T2 bounded observer concurrency)

## Scope
- timeline/writer and validation: one bounded queue (64), one dispatcher, and
  at most eight concurrent callback goroutines; enqueue is fail-open and
  non-blocking.
- observer callbacks are panic-contained; dispatcher waits at most two seconds
  before advancing. Shutdown drains queued work but returns after a five-second
  deadline if a callback is non-cooperative.
- validation: observable in-memory store (submit/read/subscribe/unsubscribe)
- cockpit: aggregation from timeline+validation+runtime catalog+approval store
- cockpit handler: GET /api/cockpit with sessions:read device bearer auth
- mobile: CockpitScreen with daemon base URL, auth context, camelCase schema

## Gate results
- go test ./... and go vet ./... PASS; targeted observer tests pass under -race
- mobile TSC PASS, Jest 35 suites/542 PASS
- observer tests: blocking callback timeout, panic containment, concurrent
  append/submit + close, idempotent close, and optional App worker cleanup

## Authority proof
- Read-only projection, no POST/PUT/DELETE
- Auth required (device bearer sessions:read)
- Every item displays provider/session/generation origin
- No production validation Submit caller exists yet. The validation store is a
  cockpit observer seam only and does not claim validator execution evidence.
