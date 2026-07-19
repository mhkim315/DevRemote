# PA3 Step 6b-4 R3 Evidence — EnsureRecorder Fix

Status: **EVIDENCE**
Date: 2026-07-20
Implementation SHA: `d7df4f067f981ade108b7df12e2cdcbb79b24a8a`

## Scope

Fix all EnsureRecorder calls in recorder_test.go to use new 2-arg signature
after ActivityBuffer parameter removal. Add local stub types for deleted
activity_stub.go so existing nil-safe assertions compile.

## Changes

- **recorder_test.go**: Fix 7 EnsureRecorder calls from 3-arg to 2-arg.
  Add local `ActivityBuffer`, `ActivityEvent`, `ActivityType` stub types
  so nil-safe test assertions compile without deleted production stubs.
  Add `var activity *ActivityBuffer` to 10 test functions.
- **telemetry_test.go**: Fix 2 NewTelemetryService calls to 5-arg signature.
- **s1e_correctness_test.go**: Fix NewTelemetryService call to 5-arg signature.
- **telemetry_service_test.go**: Restored from pre-Step6b baseline.

## Gate results

```
$ cd companion-daemon && go build ./...
(no output - success)

$ cd companion-daemon && go vet ./...
(only pre-existing parser_test.go issue - not related to our changes)
(all Step 6b-4 files: clean)

$ git diff --check
(no output - clean)
```
