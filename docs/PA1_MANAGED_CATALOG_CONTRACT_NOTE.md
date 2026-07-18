# PA1 — Managed Runtime Catalog Contract Note

Status: **PRE-IMPLEMENTATION**

This note defines the read-only `ManagedRuntimeCatalog` contract before
production edits begin. It fulfills the PA1 handoff section 4 requirement.

## 1. Authority owner

The two accepted provider-owned `ManagedSessionRegistry` instances:

| Registry | Owner | Canonical adapter prefix |
| --- | --- | --- |
| `ManagedCodexService.Registry()` | `*ManagedCodexService` | `codex_app_server` |
| `ManagedClaudeService.Registry()` | `*ManagedClaudeService` | `claude_headless` |

The catalog is a read-only federated view. It owns no store, cache, or
copy-on-write registry. Mutation authority remains exclusively with each
provider's `ManagedSessionRegistry` through its existing `Register`,
`UpdateNativeStatus`, `MarkExited`, `Remove`, and `Close` methods.

## 2. Canonical stored inputs

Every record returned by the catalog is an immutable copy of a
`ManagedSessionRecord` from one of the two authority registries. The
identity fields (`SessionID`, `Provider`, `Version`, `Epoch`, `ProcessID`,
`OS`, `Arch`, `CreatedAt`, `CertifiedDigest`, `AttestorKind`, `CertResult`,
`CertReason`) are immutable after `Register`. The semantic fields
(`NativeStatus`, `StatusChangedAt`, `Exited`) are updated only through the
provider-owned registry's `UpdateNativeStatus` and `MarkExited`.

## 3. Complete read binding

### Get

```
Get(sessionID string) (ManagedSessionRecord, bool)
```

1. Parse the canonical session ID to extract the adapter prefix.
2. If the prefix is `codex_app_server`, query only the Codex registry.
3. If the prefix is `claude_headless`, query only the Claude registry.
4. For any other prefix (including malformed, empty, tmux, cmux, localpty,
   controlled_pty, or arbitrary pane-style IDs), return `(zero, false)`.
5. A session ID that exists in BOTH registries simultaneously (ambiguous
   duplicate) fails closed: return `(zero, false)`. This is enforced by
   cross-checking: when the adapter prefix selects one registry, also
   probe the other; if both hold the ID, treat it as not found.

The catalog never probes `mux.Registry`, discovery, process names, panes,
screen text, PTY bytes, or JSONL.

### List

```
List() []ManagedSessionRecord
```

1. Call `List()` on the Codex registry; call `List()` on the Claude
   registry.
2. Concatenate both slices.
3. Sort by `CreatedAt` ascending, then `SessionID` ascending (the
   deterministic ordering already used by each registry's `List`).
4. Return defensive copies — the caller cannot mutate the catalog's
   underlying records.

The catalog never stores a merged copy or a second snapshot. Each `List()`
call reads both registries live under their respective mutexes.

Duplicate detection: if the same `SessionID` appears in both registries
after the merge (which should be impossible due to distinct adapter
prefixes), the duplicate entry is dropped and logged. The canonical entry
from the adapter-prefix-matched registry is retained.

### RuntimeOf

```
RuntimeOf(sessionID string) (RuntimeRef, bool)
```

1. First, verify the session ID is valid and non-ambiguous via the **same
   path as `Get`** (adapter-prefix dispatch + cross-registry ambiguity
   check + record validation). If `Get` would fail, `RuntimeOf` fails.
2. Delegate to the provider-specific `RuntimeOf` for the resolved adapter.
3. Cross-validate the returned `RuntimeRef` against the catalog record:
   `rt.Adapter` must equal the canonical adapter prefix, and
   `rt.LaunchGen` must equal `rec.Epoch`. A mismatch is rejected.

The provider-specific `RuntimeOf` methods remain unchanged — generation
check, launch-certification validation, exited/deferred-exit handling, and
all C3D contract conditions are preserved byte-for-byte. The catalog adds
an authority-consistency check on top: the read boundary (`Get`) and the
execution-authority boundary (`RuntimeOf`) must agree on identity.

## 4. Lookup and collision behavior

| Scenario | Get result | List behavior |
| --- | --- | --- |
| ID in Codex registry only | record, true | included |
| ID in Claude registry only | record, true | included |
| ID in both registries | zero, false (fail closed) | **both copies excluded** (fail closed) |
| Unknown/malformed prefix | zero, false | excluded |
| Pane-style ID (no adapter prefix) | zero, false | excluded |
| Arbitrary attached ID | zero, false | excluded |
| Registry closed/nil | zero, false | excluded from merge |

**Ambiguous IDs are excluded entirely from List** — neither the codex
copy nor the claude copy appears. This prevents provider-order bias
(failing closed rather than silently choosing the first registry).

**Stored-record validation**: Every record returned by `Get` or `List`
is validated at read time against:
- `mux.SessionRef.Validate()` — canonical ID with non-empty adapter
  and local ID, no control characters;
- adapter prefix matches the expected registry origin;
- non-empty `Provider`;
- non-empty `Version`.

A record that fails validation is logged and excluded. This ensures
malformed stored records (e.g. wrong adapter prefix in a registry,
empty local ID, control characters) never reach a public consumer.

## 5. Deterministic list ordering

Rows are sorted by `CreatedAt` ascending, then `SessionID` ascending.
This is the same ordering each registry's `List()` uses. When merging, a
stable sort preserves relative order for records with identical timestamps
from different registries.

## 6. Public DTO projection

The catalog's `List()` returns `ManagedSessionRecord` values — the
internal record type. Public DTO projection (`ManagedNativeStatusDTO`)
remains the responsibility of the handler layer:

| DTO field | Source | Notes |
| --- | --- | --- |
| `id` | `rec.SessionID` | canonical |
| `provider` | `rec.Provider` | |
| `version` | `rec.Version` | |
| `nativeStatus` | `rec.NativeStatus` | string-casted |
| `launchGen` | `rec.Epoch` | |
| `createdAt` | `rec.CreatedAt` | RFC3339 |
| `statusChangedAt` | `rec.StatusChangedAt` | RFC3339 |
| `exited` | `rec.Exited` | |

**Privacy exclusions** (never in any DTO): `ProcessID`, `PID`, `HookDir`,
`CertifiedDigest`, `AttestorKind`, `CertResult`, `CertReason`, `OS`,
`Arch`, raw provider events, prompts, command text, paths, tokens,
approval payloads.

The `appendCatalogRows` projector (replacing `appendManagedRows` +
`appendClaudeManagedRows`) builds `SessionTelemetry` rows from catalog
records using the same field mapping as the existing functions, with
`Adapter` set to the canonical adapter prefix and `Runner`/`AgentKind` set
to `rec.Provider`.

## 7. Restart and invalidation

Restart and invalidation behavior remains owned by the provider
registries. The catalog has no refresh, invalidation, or snapshot cache.
Each `Get`/`List` call reads the provider registries live. If a provider
service is nil or its registry is closed, that provider contributes zero
records.

## 8. Adversarial counterexamples

1. **Legacy fallback**: A caller passes `tmux:0` to `Get`. The adapter
   prefix `tmux` matches no managed provider. The catalog returns
   `(zero, false)` — it never falls through to `mux.Registry`.
2. **Ambiguous identity**: A record with `SessionID = "codex_app_server:x"`
   is somehow registered in BOTH the Codex and Claude registries (e.g., a
   test bug or a future migration error). `Get` detects the duplicate by
   cross-checking the non-selected registry and returns `(zero, false)`.
3. **Duplicate ID across registries**: The same `SessionID` appears in
   both registries. `List` drops the duplicate entry whose adapter prefix
   does not match the registry it came from, retaining one canonical row.
4. **Stale epoch**: A record's epoch changes after a runtime restart.
   `RuntimeOf` delegates to the provider's method, which compares the
   registry record's epoch against the runtime's epoch and rejects a
   mismatch.
5. **Raw-field leakage**: A DTO projection accidentally includes
   `ProcessID`. The test `TestCatalog_PublicDTOPrivacy` verifies every
   JSON key in the response against the closed allowlist.

## 9. Binding table

| Operation | Created/derived at | Stored at | Recomputed/compared at | Copied into response at | Invalidated at | Negative test |
| --- | --- | --- | --- | --- | --- | --- |
| `Get` | Provider registry `Register` | Provider `ManagedSessionRegistry.records` | Live read under registry mutex; cross-check other registry | Handler DTO projection | Provider `Remove` or `Close` | Unknown/malformed/ambiguous ID → not found |
| `List` | Both provider registries `Register` | Each provider's `ManagedSessionRegistry.records` | Live read + merge + sort per call | Handler DTO projection or `SessionTelemetry` row | Provider `Remove` or `Close` | Legacy registry blocking/failure does not affect list |
| `RuntimeOf` | Provider `InstallApprovalExecution` | Provider service `runtimes` map + registry record | Provider `RuntimeOf` (generation, certification, exit state) | `RuntimeRef` in approval claim path | Runtime exit, epoch change, service close | Stale generation rejected; uninstalled service returns false |
| Public DTO | Handler `managedNativeStatusDTO` | n/a (pure function of record) | n/a | JSON response body | n/a | No PID/ProcessID/HookDir/digest/prompt/token in DTO |
| Catalog `List` merge | Both registries live | n/a (no stored merge) | Sort: CreatedAt asc, SessionID asc | Defensive copy per call | n/a | Deterministic order verified across multiple calls |

## 10. PA1 non-goals

- No new writable store, cache, or copy-on-write registry.
- No `mux.Registry` registration of managed records.
- No modification to `mux.Registry` construction or adapter registration.
- No change to tmux, cmux, localpty, or controlled-PTY implementations.
- No change to LinkStore, linker, or `/api/v2/links`.
- No change to IPC create/attach protocol.
- No change to `HandleWS`, Recorder, VT bytes, input, resize, replay, or
  subscribers.
- No change to Stop/Kill/Delete routing or `LifecycleService`.
- No change to observer telemetry polling, transcript/activity source
  selection, or mobile branches.
- No change to Codex/Claude provider protocol, approval delivery,
  consumption witnesses, Approval Store, action digest, DTO, or mobile
  approval behavior.
- No change to `ApprovalAuthority`, `RuntimeRef`, or either provider's
  `RuntimeOf` implementation.
