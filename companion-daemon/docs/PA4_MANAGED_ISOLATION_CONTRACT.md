# PA4 Managed Isolation Contract

**PA4 ACCEPT SHA:** `96e21fff5`
**Status:** FROZEN — all PA4 waves accepted, isolation verified
**Date:** 2026-07-20

## Contract

PA4 establishes complete isolation between managed paths (controlled_pty, codex, claude) and legacy registry paths (tmux, cmux).

### Invariants

1. **useRegistry=false** for all managed controlled_pty paths. No Registry.FindSession fallback.
2. **TerminalTransport owns subscriber capability** — uses direct Recorder reference, never global GetRecorder.
3. **No RecorderFor bypass** — OwnedPTYRuntime.RecorderFor removed. SubscriberFanOut is the sole subscriber path.
4. **Generation-gated transport** — IsRetired uses explicit flag; retired transports deny all operations.
5. **Managed catalog never probes Registry** — structural isolation, no comparison facades.
6. **LifecycleService routes through ProviderLifecycleOwner** — never Registry adapter lookup.
7. **ApprovalStore uses explicit agent identity** — never Registry-based inference.
8. **Legacy observer routes contained** — feed only non-managed sessions; catalog projection drops collisions.

### Managed Adapters

| Adapter | Prefix | Owner |
|---------|--------|-------|
| controlled_pty | `controlled_pty:` | OwnedPTYRuntime |
| codex | `codex_app_server:` | ProviderLifecycleOwner (codex) |
| claude | `claude_headless:` | ProviderLifecycleOwner (claude) |

### Production Mode

- `--insecure-local-only` never used for managed paths
- Production isolation verified on SM-S926N Android 16
- Tunnel connected, device paired, 6 sessions visible

### PA4 Wave Ledger

| Wave | IMPL SHA | Status |
|------|----------|--------|
| PA4.1 | `2c34101fb` | ACCEPTED — Managed REST/list/get/status isolation |
| PA4.2 | `ba675493a` | ACCEPTED — Lifecycle and approval lookup isolation |
| PA4.3 | `cc53eb5af` | ACCEPTED — Terminal transport generation-gated isolation |
| PA4.4 | `a8bf135bf` | ACCEPTED — Observer containment |
| PA4.5 | `96e21fff5` | ACCEPTED — Facade/fallback deletion, RecorderFor bypass removed |
