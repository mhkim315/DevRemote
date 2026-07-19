# PA3 Step 6a R3 Evidence — Exact Session capture + failure test

Status: **EVIDENCE**
Date: 2026-07-20
Implementation SHA: `9289ac8ed2d7bd5658a53035fd2ba73058a7ca51`

## Defects resolved

### 1. oldCap.Session is exact (not nil)

- `CatalogEntry.session` captures exact adapter `mux.Session` at create
- `register()` accepts and stores `sess` parameter
- Replacement `oldCap.Session = existing.session` — exact identity for `CompareAndTerminate`
- All 4 call sites updated (createWithCapture, createLegacy, RegisterForTestWithHandle, lifecycle_pa2c_test)

### 2. Deterministic failure test

- `TestStep6a_FailedCreateLeavesReplacementIntact`: stops old Recorder before replacement, proves pre-install barrier creates fresh Recorder + Session, old identity never reused

### 3. Legacy cleanup path

- `register` signature updated — all callers pass `sess` (non-nil for create paths, nil for test helper)

## Gate results

```sh
$ cd companion-daemon && go build ./...
(no output — success)

$ cd companion-daemon && go vet ./...
(no output — success)

$ cd companion-daemon && go test -race ./internal/term ./cmd/devremote -count=1
ok  	devremote/companion-daemon/internal/term	14.324s
ok  	devremote/companion-daemon/cmd/devremote	33.169s
```
