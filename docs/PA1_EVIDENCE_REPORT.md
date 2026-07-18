# PA1 — Managed Runtime Catalog Evidence Report

Status: **REVIEW REQUEST** (R3 evidence closure)

Implementation SHA: `95149ed14d4e4c3d966032c95a5b1ddcef47e9ce`
Evidence/report SHA: (this commit)
Gate execution SHA: `95149ed14d4e4c3d966032c95a5b1ddcef47e9ce`

## 1. Ancestry

| Ancestor | SHA | Role |
| --- | --- | --- |
| PA0 baseline | `d8e663c0ccdb263f2747a4953908657ff27924e9` | accepted legacy consumer inventory |
| PF freeze | `33cce5743d4004da3e5cc2d6e1c50576c9068b81` | accepted state freeze |
| PA1-R1 | `54804e5c175205614cdd0e0d262e17edae79abef` | ambiguous drop-both, RuntimeOf consistency |
| PA1-R2 | `33270578f5e17a3d0c2b2ee2c11676576886cf99` | provider-origin + version binding |

All ancestry verified via `git merge-base --is-ancestor`.

## 2. Final state

| Condition | Value |
| --- | --- |
| Local HEAD | `95149ed14d4e4c3d966032c95a5b1ddcef47e9ce` |
| Remote HEAD | `95149ed14d4e4c3d966032c95a5b1ddcef47e9ce` (after push) |
| Local == Remote | YES |
| Worktree | clean |
| git diff --check | PASS |

## 3. Gate results (executed on implementation SHA)

| Gate | Result |
| --- | --- |
| `go build ./...` | PASS |
| `go vet ./...` | PASS |
| `go test -race ./internal/term ./cmd/devremote -count=1` | PASS |
| `gofmt -d` (changed .go files) | CLEAN |
| `git diff --check` | PASS |
| Secret scan (changed files) | CLEAN |
| Invariant: vendor branch scan | CLEAN |
| Invariant: ID inference scan | CLEAN |

## 4. Complete invariant-to-code mapping (R3 final)

### I1. Dual-registry list, deterministic ordering
- **Code**: `managedRuntimeCatalog.List()` at `managed_catalog.go:146-212`
- **Test**: `TestCatalog_List_DualRegistryDeterministicOrder`
- **Classification**: production-wired

### I2. Exact Get by canonical provider record
- **Code**: `managedRuntimeCatalog.Get()` at `managed_catalog.go:95-139`
- **Test**: `TestCatalog_Get_ExactProviderRecord`
- **Classification**: production-wired

### I3. Unknown/malformed/pane-style IDs → not found
- **Code**: `Get` default case + `validateManagedRecord` at `managed_catalog.go:74-98`
- **Test**: `TestCatalog_Get_UnknownAndMalformedIDs` (10 sub-cases)
- **Classification**: production-wired

### I4. Ambiguous ID → fail closed (Get + List)
- **Code**: `Get` cross-check at `managed_catalog.go:106-111,125-130`; `List` ambiguity exclusion at `managed_catalog.go:171-190`
- **Test**: `TestCatalog_Get_AmbiguousDuplicateFailsClosed` + `TestCatalog_List_AmbiguousDropBoth`
- **Classification**: production-wired

### I5. Blocking legacy Registry not invoked by managed paths
- **Code**: `HandleManagedNativeStatus`, `HandleManagedSessions`, `HandleSessionsV2` use `h.Catalog` (no fallback to `h.Managed`/`h.ManagedClaude`/`h.Registry`)
- **Test**: `TestCatalog_LegacyRegistryIsolation` (blocking adapter + deterministic barrier)
- **Classification**: production-wired

### I6. Managed rows not overwritable by legacy
- **Code**: `appendCatalogRows` at `managed_catalog.go:257-295`
- **Test**: `TestCatalog_ManagedRowsNotOverwritable`
- **Classification**: production-wired

### I7. Stale generation rejected
- **Code**: `ManagedSessionRegistry.UpdateNativeStatus`/`MarkExited` (unchanged)
- **Test**: `TestCatalog_StaleGenerationRejected`
- **Classification**: production-wired

### I8. RuntimeOf preserves exact provider, version, adapter, generation
- **Code**: `managedRuntimeCatalog.RuntimeOf()` at `managed_catalog.go:219-263` — `Get` for ambiguity + record validation, then cross-validates `rt.Adapter`, `rt.Version` (against certified authority version), `rt.LaunchGen`
- **Test**: `TestCatalog_RuntimeOf_PreservesExactProviderBinding` + `TestCatalog_RuntimeOf_AmbiguousFailsClosed` + `TestCatalog_RuntimeOf_BindingMismatch` + `TestCatalog_RuntimeOf_VersionMismatch`
- **Classification**: production-wired

### I9. Public DTO privacy
- **Code**: `managedNativeStatusDTO` at `managed_api.go:50-61` (8 closed fields)
- **Test**: `TestCatalog_PublicDTOPrivacy`
- **Classification**: production-wired

### I10. Defensive copies, no mutation authority
- **Code**: `ManagedSessionRegistry.Get`/`List` return copies; `ManagedRuntimeCatalog` interface is read-only
- **Test**: `TestCatalog_DefensiveCopies`
- **Classification**: production-wired

### I11. Exact provider-origin binding
- **Code**: `validateManagedRecord` at `managed_catalog.go:74-98` — Codex registry → `Provider == "codex"`, Claude registry → `Provider == "claude"`, case/cross-provider/empty rejected
- **Test**: `TestCatalog_ProviderIdentityMutations` — cross-provider, empty, case-mutated (both registries) + valid positive controls. RuntimeOf non-vacuous: working resolvers prove valid records succeed, same resolvers prove mutated records fail.
- **Classification**: production-wired

### I12. Exact version binding
- **Code**: `RuntimeOf` at `managed_catalog.go:256-261` — `rt.Version == expectedAuthorityVersion` from provider `AuthorityVersion()`
- **Test**: `TestCatalog_RuntimeOf_VersionMismatch` + `TestCatalog_EmptyVersionRejected`
- **Classification**: production-wired

### I13. Composition wiring
- **Code**: app.go catalog construction with authority versions from provider services
- **Test**: `TestNewAppWithDeps_CatalogWiring`
- **Classification**: production-wired

### I14. Malformed stored records filtered
- **Code**: `validateManagedRecord` used by `Get`, `List`, `RuntimeOf` (via `Get`)
- **Test**: `TestCatalog_MalformedRecordsFiltered`
- **Classification**: production-wired

## 5. Production-wired vs. test-only

| Path | Classification |
| --- | --- |
| `managedRuntimeCatalog.Get/List/RuntimeOf` | production-wired |
| `validateManagedRecord` | production-wired |
| `appendCatalogRows` | production-wired |
| `HandleManagedNativeStatus/HandleManagedSessions/HandleSessionsV2` (catalog path) | production-wired |
| `ManagedCodexService.AuthorityVersion()` / `ManagedClaudeService.AuthorityVersion()` | production-wired |
| `h.Catalog` + `h.RuntimeOf = h.Catalog.RuntimeOf` in app.go | production-wired |
| test helpers (`catalogForAPI`, `catalogWithCodex`, etc.) | test-only |
| `TestCatalog_*` / `TestNewAppWithDeps_CatalogWiring` | test-only |

## 6. Non-goals (verified unchanged)

mux.Registry, tmux/cmux/localpty/controlled-PTY, LinkStore, IPC, HandleWS, Recorder, VT bytes, lifecycle, telemetry polling, mobile, approval protocol, Approval Store, RuntimeRef — all unmodified.

## 7. Stop condition

PA1 is complete. PA2 is prohibited until PA1 receives an independent ACCEPT.
