# SP0.5 Managed I/O and Lifecycle — Pre-Implementation Contract Note

Status: pre-implementation note (handoff §3). Baseline
`3e535974288cbf2afc2894c89ff0d640cabdab47`; accepted SP0 implementation
`2b35f524dc339c24d7c3d5ee4284c967300836c3` preserved (owned registry, native
authority, pinned runtime, process lease unchanged).
Authoritative handoff: `docs/NEXT_EXECUTOR_SP0_5_MANAGED_IO_LIFECYCLE_HANDOFF.md`.

## Owners

- **Prompt input**: `ManagedCodexService.SubmitPrompt(sessionID, epoch, text)` —
  the ONLY prompt entry for local IPC and mobile REST. Validates bounds and
  binding, enforces one-active-turn, writes only through the owned transport.
- **Provider transport**: `codexManagedRuntime` (unchanged sole stdio owner).
- **Event projection**: the runtime's pump projects native notifications via
  the Codex-specific structural allowlist `projectCodexEvent` into the
  provider-neutral event store. Raw JSON-RPC objects never leave the runtime.
- **Event/reconnect state**: per-session `managedEventStore` owned by the
  service — bounded ring, monotonic Seq, snapshot+cursor reads.
- **Lifecycle**: `ManagedCodexService` Stop/Kill/Delete, directly bound to the
  owned registry and process handles; never via mux.Adapter discovery.

## Immutable binding

SessionID (`codex_app_server:<local>`) + Provider `codex` + Version
`codex-cli 0.144.1` + launch Epoch — fixed at register (SP0), compared exactly
on every prompt, event append, lifecycle op, and cursor read. Clients never
supply identity fields; the server derives device/host/permission identity.

## Event sequence

One monotonic per-session Seq (starts 1, never reused, survives consumer
reconnects, dies with the session). Ring capacity 256 events; overflow drops
oldest and records a `gap` marker so a cursor older than the ring floor
observes `gap`, never a silently complete history. Event fields (closed
allowlist): `contractVersion` ("pokit.managed.v1"), `sessionId`, `epoch`,
`seq`, `kind`, `text` (≤4096 bytes, only for `assistant`), `observedAt`.
Kinds (closed): `assistant | working | completed | exited | gap`.
Projection allowlist: `turn/started`→working, `turn/completed`→completed,
`item/completed` with `item.type=="agentMessage"`→assistant(`item.text`
byte-bounded); everything else ignored. Native semantic-status authority stays
exactly as SP0 (registry transitions unchanged, written only by the pump).

## Input states and one-active-turn

Per-runtime state (one mutex with the turn flag): `idle → in-turn → idle`,
terminal `closed`. SubmitPrompt: valid only in `idle`; concurrent prompt while
`in-turn` → conflict error with ZERO provider write. `in-turn` is set
atomically with a successful `turn/start` write claim (claim under lock, write
after release, rollback claim on write failure); cleared by the pump on
`turn/completed` or child exit. Prompt bounds: non-empty, single line, valid
UTF-8, ≤4096 bytes, no control bytes; anything else fails closed before any
provider write. Wrong SessionID/epoch/closed session → zero provider write.
Detached sessions keep the SP0 certification-turn behavior unchanged;
interactive create starts NO automatic turn.

## Lifecycle linearization

All lifecycle ops linearize on the service mutex + registry mutex (same order
everywhere: service → registry; no lock across process I/O):

- **Stop**: epoch-bound; graceful TERM to the owned process group, bounded
  wait (2s), then KILL; reap; MarkExited; input state `closed`. Idempotent
  (second stop → already-terminal success).
- **Kill**: epoch-bound; immediate group KILL + reap; MarkExited; `closed`.
- **Delete**: allowed only in terminal (Exited) state; removes the registry
  record and the session's event store; later native events/prompts inert.
  Idempotent (deleting the deleted → not-found, no side effect).
- **Natural exit**: pump EOF → MarkExited + `exited` event + `closed` input.
- **Daemon shutdown**: SP0 lease drain unchanged (closing → cancel leases →
  kill published → bounded wait until reaped and leases empty).
- A stopped/killed/deleted session accepts no prompt and no status update
  (registry epoch/exit gates already enforce this; delete removes the row).

## Reconnect

Reconnect = client transport reconnect to the same live daemon/runtime only.
Read contract: `snapshot` (current bounded status DTO + ring floor/ceiling) +
events strictly after the presented cursor. Duplicate/replayed cursor is
idempotent (same events again); cursor ahead of ceiling, wrong session, wrong
epoch, unknown fields → fail closed. Stale prior-epoch state can never be
served: events and snapshots carry the record's single epoch. Daemon restart
recovery: explicitly deferred (fresh daemon has no managed sessions).

## Capacity / overflow / close

Sessions: SP0 registry capacity 4 (unchanged). Event ring: 256/session, gap on
overflow. Prompt: 1 active turn/session, conflict otherwise. Event store
closes with the session (delete) or daemon shutdown; closed store rejects
appends and serves nothing.

## Public DTO allowlist

- Managed status DTO: unchanged SP0 8-field set.
- Event DTO: the closed field set above. NEVER: raw JSON-RPC, prompts echoed
  back (user prompt text is not re-served), thread/turn/item ids, provider
  payloads, paths, tokens, process details.
- Capability honesty: native managed sessions report NO resize/PTY capability;
  no fake resize implementation.

## Surfaces

- **Local (A)**: IPC op `managed-attach {sessionId, cursor}` on the 0600
  socket → server streams event JSONL; client sends `{"prompt": "..."}` JSON
  lines; EOF (Ctrl-D) detaches the viewer only. `pokit run codex`
  (non-detached, recognized exact token) now sends the structured profile
  request — the legacy command-string form for this exact invocation is
  removed; managed-disabled stays an explicit unavailable error.
- **Mobile (B)**: `GET /api/managed-sessions/{id}/events?cursor=N`
  (sessions:read) returning bounded snapshot+events+nextCursor;
  `POST /api/managed-sessions/{id}/prompt` (terminal:input). Paired-device
  auth, host binding, bearer expiry reused; no raw /term/ws PTY framing.
- **Lifecycle (C)**: `POST /api/managed-sessions/{id}/stop`, `.../kill`
  (sessions:stop / sessions:kill), `DELETE /api/managed-sessions/{id}`
  (history:delete) — direct ManagedCodexService binding.

## Explicit non-goals

Approvals (capacity zero; `waiting_approval` display-only), tool-input
forwarding, arbitrary commands, ANSI/TUI emulation, PTY wrapper for
app-server, second Codex process, resize for native sessions, daemon restart
recovery, persistent broker, provider/plugin SDK, capability engine,
Task/Dispatch, Claude/Windows, tmux/cmux removal, notifications.
