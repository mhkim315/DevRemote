# Step 8 Evidence — Mobile Operational Cockpit

**EVID HEAD:** (this commit)
**IMPL SHAs:** 90cc46c3f (store) → 44f98dfcb (handler) → 10432920b (mobile) → 793510e07 (observer) → 0a11ef7e1 (validation store) → 117740d1f (integration)

## Scope
- timeline/writer: non-blocking append observer
- validation: observable store (submit/read/subscribe)
- cockpit: aggregation from timeline+validation+runtime catalog+approval store
- cockpit handler: GET /api/cockpit with sessions:read device bearer auth
- mobile: CockpitScreen with daemon base URL, auth context, camelCase schema

## Gate results
- go build/vet/race PASS, gofmt clean
- mobile TSC PASS, Jest 35 suites/542 PASS
- cockpit tests: store roundtrip, auth 401, observer delivery, schema match

## Authority proof
- Read-only projection, no POST/PUT/DELETE
- Auth required (device bearer sessions:read)
- Every item displays provider/session/generation origin
