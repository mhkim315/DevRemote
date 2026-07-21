# TERM-G1 Evidence — Managed-PTY Geometry Authority Restoration

**IMPL SHA:** `2d13020ae`
**EVID SHA:** (this commit — DOCS-ONLY R1)
**ACCEPT SHA:** `2d13020ae`
**Plan:** `docs/PB_DEVICE_GATE_TERMINAL_REGRESSION_REMEDIATION_PLAN.md` §4

## Implementation Summary

### Production Changes

| File | Change |
|------|--------|
| `internal/term/pty_launcher_v1.go` | `nativePTYHandle.GetSize()` via `pty.Getsize`; `pty.StartWithSize` with 30×100 default |
| `mobile/src/lib/terminalController.ts` | Validate `/term/size` response: integer, bounds 1-1000/1-2000; live WS geometry with identity binding |
| `mobile/src/screens/FeedScreen.tsx` | Remove unauthenticated `/term/size` poll; use `__pokitPTYSize` bootstrap + WS geometry frame |

### Key Behaviors

1. **Default geometry**: Every managed PTY spawns at 30×100 before child process starts (`pty.StartWithSize`)
2. **Live size retrieval**: `nativePTYHandle.GetSize()` → `handleStream.GetSize()` → `Recorder.GetSize()` → `TerminalTransport.Geom()`
3. **Identity-bound WS geometry**: Daemon emits `session` + `generation` in geometry frames; mobile validates against `__pokitGeomGen`/`__pokitGeomSession` from hello
4. **Mobile poll removed**: Paired WebView performs zero `/term/size` fetches; geometry from authenticated bootstrap + WS frames only
5. **Bounds validation**: Reject non-integer, zero, negative, >1000 rows, >2000 cols in both bootstrap and live WS paths

### Tests (17 Go + 15 mobile = 32 total)

**Go geometry tests (11):**
- `TestTERM_G1_NativeLauncherDefaultsZeroConfig`
- `TestTERM_G1_HandleGetSizeAfterResize`
- `TestTERM_G1_NativeLauncherHonorsExplicitConfig`
- `TestTERM_G1_NativeLauncherRejectsZeroNegative` (3 subtests)
- `TestTERM_G1_RecorderGetSizeChain`
- `TestTERM_G1_TerminalTransportGeomChain`
- `TestTERM_G1_ResizeReflectedThroughChain`
- `TestTERM_G1_StaleGenerationCannotObserve`
- `TestTERM_G1_StaleResizeIsNoOp`
- `TestTERM_G1_HandleImplementsPTYHandle`
- `TestTERM_G1_PTYReadDoesNotResolveWrongGeneration`

**ANSI fixture tests (6):**
- `TestTERM_G1_ANSIFixtureDefault100Cols`
- `TestTERM_G1_ANSIFixtureResizeReflectedInTTY`
- `TestTERM_G1_ANSIFixtureWideLineNoKernelWrap`
- `TestTERM_G1_ANSIFixtureAlternateScreenSequences`
- `TestTERM_G1_ANSIFixtureCursorMovement`
- `TestTERM_G1_ANSIFixtureColorSGR`

**Mobile geometry validation tests (15):**
- Valid/invalid bounds, non-integer, zero, negative, boundary max/min, NaN, non-numeric strings, absent response

## Gate Results (at ACCEPT)

```
go build ./...        PASS
go vet ./...          PASS
go test -race ./...   ALL PASS
gofmt -l .            CLEAN
npx tsc --noEmit      PASS
npx jest              PASS
```

## Remediation Rounds

| Round | SHA | Changes |
|-------|-----|---------|
| Initial | `aba206add` | GetSize, StartWithSize, mobile poll removal, 11 tests |
| R1 | `835be97a2` | Geometry order fix (StartWithSize before process) |
| R1 | `ef1ac8f94` | Mobile validation, ANSI fixtures |
| R2 | `2d13020ae` | Geometry identity binding (session+generation in WS frames) |
| gofmt | `5a92a7e10` | Formatting across all Go files |
