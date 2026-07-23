# Step 8 Evidence — Mobile Operational Cockpit

**EVID HEAD:** (this commit)
**IMPL SHAs:** 90cc46c3f (store) → 44f98dfcb (handler) → 10432920b (mobile) → 24d6d2d95 (mailbox model)

## Scope
- timeline/writer and validation: bounded ring buffers (128 envelopes / 64 results).
  Zero goroutines — readers poll via ReadRecent on their own schedule.
- cockpit: handler calls Refresh on every GET, polling writer.ReadRecent,
  validation.ReadRecent, runtime catalog, and approval store.
- Ring buffers are readable after Close (closed submissions are rejected,
  but existing data remains available for inspection).
- No real-time push, no observer callbacks, no dispatcher goroutines, no
  semaphore limits — simplest possible polling model.

## Gate results
- go test ./... PASS, go vet ./... PASS, all under -race
- mobile TSC PASS, Jest 35 suites/542 PASS
- Ring buffer tests: wrap-around, clamped n, concurrent append+read, empty
  buffer, Close idempotent. Validation tests: Submit+ReadAll+ReadRecent,
  capped history (4096 then drop oldest half), concurrent safety.

## Authority proof
- Read-only projection, no POST/PUT/DELETE
- Auth required (device bearer sessions:read)
- Every item displays provider/session/generation origin
- The App struct exposes the ValidationStore for future producers. No
  production Submit caller exists yet — Step 7 was contract-only.
