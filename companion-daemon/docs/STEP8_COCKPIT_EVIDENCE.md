# Step 8 Evidence — Mobile Operational Cockpit

**IMPL SHAs:**
- Part 1 (store): `90cc46c3f4d8c79a0b3364688a097eb26cfe0dde`
- Part 2 (handler+route): `44f98dfcb9f598bdc6e00a1b23a9ab803abe2778`
- Part 3 (mobile): `10432920b55e72beb65f04449dd18ca8fb63117b`

## Scope

- `internal/cockpit/cockpit.go` — read-only aggregation store (Append/ReadAll, concurrent-safe)
- `internal/cockpit/handler.go` — GET `/api/cockpit` JSON endpoint (read-only)
- `cmd/devremote/app.go` — cockpit store + handler wiring (default-off flag)
- `mobile/src/navigation/RootNavigator.tsx` — route registration
- `mobile/src/screens/CockpitScreen.tsx` — items render with origin display, loading/error states
- Only these files changed; no existing routes/authorities modified.

## Gate results

- go build/vet/race PASS
- gofmt clean
- mobile TSC PASS, Jest 35 suites/542 tests PASS
- cockpit tests: store append/read round-trip, concurrent read safety, handler 200+JSON, empty store

## Authority proof

- Read-only projection: handler has no POST/PUT/DELETE
- Every rendered item displays provider/session/generation origin
- Cockpit store consumes existing runtime state without modifying any authority path
