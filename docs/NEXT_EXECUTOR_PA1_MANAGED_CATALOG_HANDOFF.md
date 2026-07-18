# Next Executor Handoff — PA1 Managed Catalog and Public Reads

Status: **PA1 READY AFTER INDEPENDENT PA0 ACCEPT**

This handoff authorizes **PA1 only**. It preserves the accepted Codex and
Claude managed-runtime and approval contracts and deliberately excludes the
later lifecycle, terminal, telemetry, mobile and physical-removal packets.

## 1. Canonical repository and startup state

Canonical remote:

```text
https://github.com/mhkim315/DevRemote.git
```

Canonical branch:

```text
feature/phase10-multi-adapter
```

PA1 implementation baseline:

```text
d8e663c0ccdb263f2747a4953908657ff27924e9
```

The executor may use any assigned writable checkout. It must derive the root
with `git rev-parse --show-toplevel`; no absolute path from another agent is an
implementation input.

Before editing:

1. verify the exact remote and branch above;
2. fetch and fast-forward only;
3. require local HEAD == remote HEAD;
4. require an empty worktree;
5. require `33cce5743d4004da3e5cc2d6e1c50576c9068b81` and
   `d8e663c0ccdb263f2747a4953908657ff27924e9` as ancestors;
6. record the actual checkout root in the completion report.

Do not force-push, rebase accepted history, or reset a dirty checkout.

## 2. Mandatory reading order

1. `docs/POST_CLAUDE_ACCEPTED_STATE_FREEZE.md`
2. `docs/A1_2_FINAL_ACCEPTANCE.md`
3. `docs/PA0_LEGACY_CONSUMER_INVENTORY_CONTRACT.md`
4. `docs/POST_CLAUDE_MANAGED_ONLY_RESTRUCTURING_PLAN.md`, Packet A1 only
5. `docs/EXECUTION_AGENT_TASK_PACKET_PROTOCOL.md`
6. `companion-daemon/internal/term/managed_registry.go`
7. `companion-daemon/internal/term/managed_api.go`
8. `companion-daemon/internal/term/managed_approval_activation.go`
9. `companion-daemon/internal/term/managed_claude_activation.go`
10. `companion-daemon/cmd/devremote/app.go`

## 3. Recovery audit from the interrupted Gemini checkout

An interrupted executor worked in:

```text
/Users/mhk/Documents/gemini/DevRemote
```

That checkout is **reference evidence only**, not an implementation source.
It remained at baseline `d8e663c` with a dirty 47-file worktree. A temporary
reflog commit existed at `0a4686785`, then was mixed-reset and followed by much
broader uncommitted edits.

Do not cherry-pick `0a4686785`. Do not copy the dirty tree wholesale. Do not
clean or reset that checkout until its owner has separately preserved it.

Useful findings to retain:

- `Handlers.Registry` removal has a wide test/composition blast radius and is
  not the PA1 objective by itself.
- deleting LinkStore, link routes, create paths, IPC attach, telemetry polling
  or PTY/WebSocket lookup combines PA2, PA3 and Phase B and leaves no reviewable
  PA1 checkpoint;
- replacing a removed Registry with a nil local variable is invalid: the
  interrupted `HandleWS` attempt would dereference a nil Registry;
- removing controlled-PTY creation before `TerminalTransport` exists deletes
  useful owned byte I/O rather than merely removing observer ownership;
- changing constructor signatures mechanically across dozens of tests before
  freezing the catalog boundary creates compile churn without proving the PA1
  invariant;
- the interrupted tree did not introduce a `ManagedRuntimeCatalog` and failed
  backend compilation due to half-migrated tests and deleted helpers.

These are negative design inputs. The clean baseline remains authoritative.

## 4. Pre-implementation contract note

Before production edits, add
`docs/PA1_MANAGED_CATALOG_CONTRACT_NOTE.md` and commit it separately. It must
state:

- authority owner: the two accepted provider-owned
  `ManagedSessionRegistry` instances;
- canonical stored inputs: immutable `ManagedSessionRecord` values;
- complete read binding: canonical SessionID, provider, version, epoch/native
  generation, native status and exit state;
- no new writable store, cache, copy-on-write registry or legacy registration;
- lookup and collision behavior;
- deterministic list ordering;
- exact public DTO projection and privacy exclusions;
- restart/invalidation behavior remains owned by the provider registries;
- one adversarial counterexample for legacy fallback, ambiguous identity,
  duplicate ID, stale epoch and raw-field leakage;
- PA1 non-goals from section 7 below.

The note must include a binding table for `Get`, `List`, public DTO projection
and approval-runtime delegation.

## 5. PA1 target boundary

Introduce the smallest read-only managed catalog demanded by current call
sites. Reuse the existing registries; do not create a third authority store.

The expected minimum conceptual surface is:

```go
type ManagedRuntimeCatalog interface {
    Get(sessionID string) (ManagedSessionRecord, bool)
    List() []ManagedSessionRecord
    RuntimeOf(sessionID string) (RuntimeRef, bool)
}
```

The exact Go names may differ if the contract note proves a smaller surface.
Requirements do not differ:

- `Get` selects the exact provider-owned registry from canonical identity; it
  never probes `mux.Registry`, discovery, process names, panes or terminal text;
- `List` reads copies from the accepted Codex and Claude registries and returns
  deterministic ordering without storing a merged copy;
- a duplicate/ambiguous canonical ID fails closed rather than choosing by
  provider order;
- `RuntimeOf` delegates to the accepted provider-specific `RuntimeOf` methods;
  those methods and their generation/certification validation remain unchanged;
- no mutation method is exposed by the catalog;
- no managed record is registered into `mux.Registry`.

The catalog may be installed on `Handlers` as a read-only dependency. Keep the
existing provider services for provider-specific events, prompts and lifecycle;
PA1 does not generalize those mechanisms.

## 6. Authorized production migration

Migrate only managed public reads and read-only runtime resolution:

- `HandleManagedNativeStatus`;
- `HandleManagedSessions`;
- managed rows appended to `HandleSessionsV2`;
- `appendManagedRows` / `appendClaudeManagedRows`, or a narrowly replacing
  catalog projector;
- composition of the existing combined approval runtime resolver through the
  catalog, without changing `ApprovalAuthority`, `RuntimeRef` or either
  provider's `RuntimeOf` implementation.

The legacy `/api/sessions` side may remain as a temporary read-only comparison
source during PA1, but managed rows must come only from the managed catalog. A
managed lookup must never fall through to the legacy side.

## 7. Explicit PA1 non-goals

Do not modify or delete:

- `mux.Registry` construction or adapter registration in `app.go`;
- tmux, cmux, localpty or controlled-PTY implementations;
- LinkStore, linker or `/api/v2/links`;
- IPC create/attach protocol or constructor signatures;
- `HandleWS`, Recorder, VT bytes, input, resize, replay or subscribers;
- Stop/Kill/Delete routing or `LifecycleService`;
- observer telemetry polling, transcript/activity source selection or mobile
  external/best-effort branches;
- install flags, fixtures or legacy documentation cleanup;
- Codex/Claude provider protocol, approval delivery, consumption witnesses,
  Approval Store, action digest, DTO or mobile approval behavior;
- Canonical Timeline, N1, Grok/ACP, Navigator, O1 or O2.

Those belong to PA2, PA3, PA4 or PB and require their own independent packet.

## 8. Required tests

Add focused non-vacuous tests proving:

1. Codex and Claude records are returned from the same read-only catalog with
   deterministic ordering.
2. Exact `Get` returns only the canonical provider record.
3. Unknown, malformed, pane-style and arbitrary attached IDs return not found.
4. A duplicate/ambiguous ID fails closed.
5. A blocking or panicking legacy Registry cannot be invoked by managed
   list/get/native-status requests.
6. Managed rows in `/api/sessions` are projected from the catalog and cannot be
   overwritten by a legacy row with the same ID.
7. Stale generation/native updates remain rejected by the accepted provider
   registry.
8. `RuntimeOf` preserves the exact provider, version, generation and launch
   certification checks.
9. Public DTOs contain no PID, process token, hook directory, certified digest,
   raw provider event, prompt, command, path, token or approval payload.
10. Catalog reads return defensive copies and expose no mutation authority.

Do not satisfy these tests with a fixture-only catalog unused by production
composition.

## 9. Verification gate

After implementation:

```sh
cd companion-daemon
go build ./...
go vet ./...
go test -race ./internal/term ./cmd/devremote -count=1
```

Also run:

- `gofmt -d` on changed Go files;
- `git diff --check`;
- the repository invariant and secret scans relevant to changed files;
- targeted accepted Codex and Claude runtime/approval composition tests.

No new live model turns, mobile rebuild or Android run is required when PA1
does not touch those paths. Report skips honestly.

## 10. Commit and stop discipline

Use reviewable commits:

1. PA1 contract note;
2. catalog and production read migration plus focused tests;
3. evidence report only after the implementation HEAD is frozen and gated.

The completion report must distinguish production-wired from test-only paths
and map every PA1 invariant to exact code and tests. Push fast-forward only,
confirm local == remote and a clean worktree, then stop for independent PA1
review.

PA2 is prohibited until PA1 receives an independent ACCEPT.
