# PA1 — Managed Runtime Catalog Evidence Report

Status: **REVIEW REQUEST** (R1+R2 remediated)

Final implementation HEAD: `TBD` (this commit)

Previous HEAD: `54804e5` (R1 remediation), `5d7d4ab` (initial PA1)

## 1. Gate results (frozen remediated HEAD)

| Gate | Result |
| --- | --- |
| `go build ./...` | PASS |
| `go vet ./...` | PASS |
| `go test -race ./internal/term ./cmd/devremote -count=1` | PASS |
| `gofmt -d` (changed files) | CLEAN |
| `git diff --check` | PASS |
| Secret scan | CLEAN |
| Invariant: vendor branch | CLEAN |
| Invariant: ID inference | CLEAN |

## 2. Invariant-to-code-and-test mapping (R2 updated)

### I1. Codex/Claude records from same catalog, deterministic ordering
- **Code**: `managedRuntimeCatalog.List()` at `managed_catalog.go:146-212`
- **Test**: `TestCatalog_List_DualRegistryDeterministicOrder`
- **Classification**: production-wired

### I2. Exact Get returns canonical provider record
- **Code**: `managedRuntimeCatalog.Get()` at `managed_catalog.go:95-139`
- **Test**: `TestCatalog_Get_ExactProviderRecord`
- **Classification**: production-wired

### I3. Unknown/malformed/pane-style IDs return not found
- **Code**: `Get` default case + `validateManagedRecord` at `managed_catalog.go:74-98`
- **Test**: `TestCatalog_Get_UnknownAndMalformedIDs` (10 sub-cases)
- **Classification**: production-wired

### I4. Duplicate/ambiguous ID fails closed (Get + List)
- **Code**: `Get` cross-check at `managed_catalog.go:106-111,125-130`; `List` ambiguity detection at `managed_catalog.go:171-190`
- **Test**: `TestCatalog_Get_AmbiguousDuplicateFailsClosed` + `TestCatalog_List_AmbiguousDropBoth`
- **Classification**: production-wired

### I5. Blocking legacy Registry not invoked by managed paths
- **Code**: `HandleManagedNativeStatus`, `HandleManagedSessions`, `HandleSessionsV2` all use `h.Catalog` — no fallback to `h.Managed`/`h.ManagedClaude` or `h.Registry`
- **Test**: `TestCatalog_LegacyRegistryIsolation` (blocking adapter + deterministic barrier)
- **Classification**: production-wired

### I6. Managed rows from catalog not overwritable by legacy
- **Code**: `appendCatalogRows` at `managed_catalog.go:257-295`
- **Test**: `TestCatalog_ManagedRowsNotOverwritable`
- **Classification**: production-wired

### I7. Stale generation rejected by provider registry
- **Code**: `ManagedSessionRegistry.UpdateNativeStatus`/`MarkExited`
- **Test**: `TestCatalog_StaleGenerationRejected`
- **Classification**: production-wired (underlying registry unchanged)

### I8. RuntimeOf preserves exact provider, version, adapter, generation
- **Code**: `managedRuntimeCatalog.RuntimeOf()` at `managed_catalog.go:219-263` — calls `Get` for ambiguity + record validation, then cross-validates `rt.Adapter`, `rt.Version`, `rt.LaunchGen` against catalog record and certified authority version
- **Test**: `TestCatalog_RuntimeOf_PreservesExactProviderBinding` + `TestCatalog_RuntimeOf_AmbiguousFailsClosed` + `TestCatalog_RuntimeOf_BindingMismatch` + `TestCatalog_RuntimeOf_VersionMismatch`
- **Classification**: production-wired

### I9. Public DTO privacy
- **Code**: `managedNativeStatusDTO` at `managed_api.go:50-61` — 8 closed fields
- **Test**: `TestCatalog_PublicDTOPrivacy`
- **Classification**: production-wired

### I10. Defensive copies, no mutation authority
- **Code**: `ManagedSessionRegistry.Get`/`List` return copies; `ManagedRuntimeCatalog` interface declares only `Get`/`List`/`RuntimeOf`
- **Test**: `TestCatalog_DefensiveCopies`
- **Classification**: production-wired

### I11. Exact provider-origin binding (R2 NEW)
- **Code**: `validateManagedRecord` at `managed_catalog.go:74-98` — Codex registry records must have `Provider == "codex"`; Claude registry records must have `Provider == "claude"`. Case-mutated, cross-provider, and empty values fail closed.
- **Test**: `TestCatalog_ProviderIdentityMutations` — cross-provider, empty, case-mutated mutations in both registries fail Get/List/RuntimeOf; positive controls for valid identities pass
- **Classification**: production-wired

### I12. Exact version binding in RuntimeOf (R2 NEW)
- **Code**: `RuntimeOf` cross-validation at `managed_catalog.go:256-261` — `rt.Version` must equal the certified authority version from the provider's activation config. Provider-specific policy: no generic string normalization. `ManagedCodexService.AuthorityVersion()` + `ManagedClaudeService.AuthorityVersion()` at `managed_codex.go:659` and `managed_claude.go:907`
- **Test**: `TestCatalog_RuntimeOf_VersionMismatch` — wrong version rejected; correct version accepted. `TestCatalog_EmptyVersionRejected` — empty version excluded from Get/List/RuntimeOf.
- **Classification**: production-wired

### I13. Composition wiring (R4)
- **Code**: app.go catalog construction with authority versions, `h.RuntimeOf = h.Catalog.RuntimeOf`
- **Test**: `TestNewAppWithDeps_CatalogWiring` — verifies Catalog installed, RuntimeOf delegates
- **Classification**: production-wired

## 3. Production-wired vs. test-only

| Path | Classification |
| --- | --- |
| `managedRuntimeCatalog.Get/List/RuntimeOf` | production-wired |
| `validateManagedRecord` | production-wired |
| `appendCatalogRows` | production-wired |
| `HandleManagedNativeStatus` / `HandleManagedSessions` / `HandleSessionsV2` (catalog path) | production-wired |
| `ManagedCodexService.AuthorityVersion()` / `ManagedClaudeService.AuthorityVersion()` | production-wired |
| `h.Catalog` field + `h.RuntimeOf = h.Catalog.RuntimeOf` in app.go | production-wired |
| `catalogForAPI` / `catalogWithCodex` / `catalogWithClaude` / `catalogWithBoth` | test-only |
| `TestCatalog_*` / `TestNewAppWithDeps_CatalogWiring` | test-only |

## 4. Non-goal confirmation

No modifications to: mux.Registry, tmux/cmux/localpty/controlled-PTY, LinkStore, IPC, HandleWS, Recorder, VT bytes, lifecycle, telemetry, mobile, approval protocol, Approval Store, RuntimeRef.

## 5. Stop condition

This report marks the R2 remediation boundary. PA2 is prohibited until PA1 receives an independent ACCEPT.
