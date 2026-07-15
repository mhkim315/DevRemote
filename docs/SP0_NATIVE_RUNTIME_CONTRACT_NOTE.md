# SP0 Native Managed Runtime — Pre-Implementation Contract Note

Status: pre-implementation note (protocol §5). Baseline `1fc0e5a3e2055dca0944d13d5e02118c35ce2977`.
Authoritative handoff: `docs/NEXT_EXECUTOR_SP0_NATIVE_RUNTIME_HANDOFF.md`.

## Ownership and authority

- **Child process + app-server stdio owner**: `codexManagedRuntime` (one per managed
  session, `internal/term`). It holds the process handle, stdin writer, stdout JSONL
  reader, and the single event-pump goroutine. Nothing else may read or write the
  child's stdio. No PTY, no Recorder, no shell.
- **Semantic-status authority**: Codex native protocol notifications ONLY
  (`turn/started`, `turn/completed`), applied through
  `ManagedSessionRegistry.UpdateNativeStatus`. Screen text, PTY bytes, JSONL files,
  process names, CWD, and observer telemetry have no write path into the registry
  (no such method is exported to them).
- **Process/session lifecycle authority**: the daemon-owned child handle (spawn,
  kill, reap) via a narrow `managedLauncher` seam.
- **Composition owner**: `ManagedCodexService` — owns the launcher, the registry,
  the LaunchGeneration counter, and all runtimes; constructed by `NewAppWithDeps`
  behind default-off `Config.EnableManagedCodex`; stopped by `App.Shutdown`.

## Canonical SessionID

Daemon-generated at create: `codex_app_server:<genLocalID("codex-app")>`
(adapter segment `codex_app_server` is never registered in `mux.Registry`, so the
identity space is disjoint from tmux/cmux/localpty/controlled_pty and is never
reachable through adapter discovery or `Registry.Refresh`). Clients never supply IDs.

## Immutable binding (fixed at Register, never mutated)

SessionID, Provider=`codex`, Version=`codex-cli 0.144.1` (pinned, verified),
RuntimeEpoch (== LaunchGen, monotonic per service), opaque ProcessID string
(launcher-derived; never parsed, never exposed), OS + Arch metadata, CreatedAt.

## Native states and exact transition events

Closed vocabulary: `idle | working | completed | exited`.

- Register → `idle` (only after initialize/initialized handshake + `thread/start`
  result succeed).
- `idle → working`: exact notification `turn/started` with `threadId` equal to the
  runtime's bound thread, current epoch.
- `working → completed`: exact notification `turn/completed` with matching
  `threadId`, current epoch. (`completed` is the semantic completed/idle state.)
- any → `exited`: child stdout EOF / read error / Wait return → `MarkExited`.
- Unknown or unmatched notifications (including `thread/status/changed`,
  `item/*`, crafted methods): IGNORED — they can never produce or restore a known
  status. Old/closed-epoch updates rejected.

## Linearization points

- **Registry state**: one `sync.Mutex` inside `ManagedSessionRegistry`. Register,
  Get, List, UpdateNativeStatus, MarkExited, Remove, and the closed flag all
  linearize here. Defensive copies on read. No lock held across I/O.
- **Runtime lifecycle**: per-runtime mutex ordering spawn → handshake → register →
  pump-start, and stop/rollback. Registry lock is never held while touching the
  child; runtime lock is never held while calling registry methods that block.
- **Launch generation**: service counter incremented under the service mutex at
  spawn; the epoch is copied into the runtime and the record before the pump starts.

## Failure, cleanup, rollback

- Verify (version/digest/realpath) fails → no spawn, error to caller.
- Spawn fails → error; nothing registered.
- initialize / initialized / thread/start fails or times out (bounded) → kill +
  reap child, nothing registered, no visible session.
- Register fails (capacity/duplicate) → kill + reap child, error.
- Child exit → pump closes, `MarkExited(id, epoch)`, child reaped via Wait.
- Daemon shutdown → `ManagedCodexService.Shutdown(ctx)`: registry closed (later
  updates rejected), every child killed (process group) and reaped with a bounded
  fallback deadline.
- After `MarkExited`/`Remove`/shutdown-close: any queued/late status update is
  rejected at the registry compare.

## Capacity

`maxManagedSessions = 4` (SP0 constant). `Register` on a full registry fails
closed; the child is killed + reaped by the caller; existing records unchanged.
Duplicate SessionID likewise fails closed without replacing state.

## Entry path (P1)

- CLI `pokit run --detach codex` (exactly the recognized profile token) sends a
  structured create request `{operation:"create", profileId:"codex", detach:true}` —
  no joined command string. All other `pokit run` forms are untouched.
- IPC create handler fails closed when more than one of {profileId, executable,
  command} is present.
- With `EnableManagedCodex` on and `profileId=="codex" && detach`, the daemon
  launches the pinned certified executable directly:
  argv `[<pinned>/node_modules/.bin/codex, "app-server", "--stdio"]` — evidence-exact;
  no `bash`, `sh`, `-c`, no interactive Codex, no PATH lookup. Flag off → existing
  legacy profile branch (controlled_pty), unchanged semantics.
- After registration the runtime starts ONE server-derived bounded certification
  turn (`turn/start` with a fixed constant text input owned by the daemon; no user
  input, no command execution requested). The production event pump then drives
  `working → completed`.

## Approvals (explicit SP0 behavior)

Capacity zero; `provenActionMapping` empty. If the provider nonetheless emits
`item/commandExecution/requestApproval`, SP0 never responds and never makes it
actionable; the frozen A1 contracts are untouched. Native approval is SP1.

## Binding table

| Field | Created/derived at | Stored at | Recomputed/compared at | Copied into request/receipt at | Invalidated at | Negative test |
| --- | --- | --- | --- | --- | --- | --- |
| SessionID | genLocalID at managed create | registry key + record | every registry op (exact) | IPC create response; REST DTO | Remove / shutdown | duplicate Register fails closed |
| Provider | constant at create | record | immutable | REST DTO | never | DTO shape test |
| Version | pinned config | record | `Verify()` before every spawn | REST DTO | mismatch → no spawn | tampered digest/version → no child, no session |
| RuntimeEpoch (LaunchGen) | service counter at spawn | record + runtime copy | UpdateNativeStatus / MarkExited exact compare | not client-supplied ever | MarkExited / Remove / closed | stale-epoch update rejected |
| ProcessID (opaque) | launcher at spawn | record (internal only) | n/a | NEVER (excluded from DTO) | child exit | DTO excludes process details |
| ThreadID | `thread/start` result | runtime only | every `turn/*` notification match | never exposed | child exit | mismatched-threadId event ignored |
| NativeStatus | registry at Register | record | closed vocabulary switch in pump | REST DTO | epoch close | unknown event cannot fabricate status |

## Adversarial counterexamples (one per critical invariant)

1. **Stale epoch**: update with epoch N−1 (or after MarkExited) → rejected; record
   unchanged. Test: `TestManagedRegistry_StaleEpochRejected`.
2. **Fabricated status**: crafted unknown notification / `thread/status/changed`
   → no transition. Test: unknown-event pump test.
3. **Capacity**: 5th Register fails; first 4 records byte-identical before/after.
4. **Duplicate ID**: second Register with same ID fails; original untouched.
5. **Observer overwrite**: failing tmux/cmux refresh + telemetry churn → managed
   REST row unchanged (no API exists to write it). Production-path test.
6. **Conflicting create**: profileId+command in one request → fail closed before
   any spawn; launcher never invoked.
7. **Shell wrapper**: injected launcher asserts exact exe+argv; any `-c`/shell
   token in argv fails the test.

## REST read surface (P3)

- `GET /api/sessions` (authenticated, both auth modes): managed rows appended
  directly from the owned registry — never from `mux.Registry`/telemetry snapshot.
- `GET /api/sessions/{id}/native-status`: bounded managed DTO
  `{id, provider, version, nativeStatus, launchGen, createdAt, statusChangedAt,
  exited}` — excludes prompts, command text, payloads, paths, tokens, raw protocol
  messages, PIDs/process details.

## Explicit non-goals

Interactive/attached terminal, WebSocket/mobile input, resize, Stop/Kill/Delete
REST, reconnect, persistence, restart recovery, generic replacement, multi-provider
dispatch, provider SDK/plugin system, approval actions, process-image attestation
beyond the pinned `Verify()`, tmux/cmux removal, Claude/Windows support.
