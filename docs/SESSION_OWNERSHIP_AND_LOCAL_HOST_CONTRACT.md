# M1.5 — Session Ownership and Local Terminal Host Contract

Status: approved plan; execute after corrected M0/M1 acceptance

## Purpose

Pokit has two independent extension axes:

1. where a runtime/session comes from;
2. which local terminal UI hosts `pokit run`.

They must not be represented as one adapter list.

## Session ownership classes

### Managed runtime

Example: `controlled_pty` created by `pokit run` or the safe daemon profile API.

Pokit owns:

- process and process group;
- PTY;
- Recorder;
- lifecycle state;
- Stop/Kill cleanup;
- retained history/catalog.

Expected capabilities:

```text
observe
input
liveTerminal
reliableTranscript
managedLifecycle
```

### External attachable runtime

Example: tmux.

Pokit can discover and attach to an existing, externally owned byte stream.
It may observe and write input, but M2 must not treat it as a Pokit-managed
process merely because input/control is available.

Expected MVP capabilities:

```text
observe
input
liveTerminal
reliableTranscript
managedLifecycle = false
```

Any future operation that kills a tmux session is a separate explicit adapter
action, not controlled PTY Stop semantics.

### External observer

Example: cmux in the current product.

```text
observe
bestEffortTranscript
managedLifecycle = false
```

cmux viewport snapshots are not an authoritative runtime stream. Do not expose
managed Stop/Kill or reliable terminal/history based on snapshot heuristics.

### Internal/experimental runtime

Example: `localpty` while it remains a test/feature-flag backend.

Its implementation may be daemon-owned, but product lifecycle capabilities are
not advertised until explicitly accepted for user-facing use.

### Unknown adapter

Safe default:

```text
ownership = external
observe only
managedLifecycle = false
```

## Capability rule

Input/control capability and lifecycle ownership are different dimensions.

```text
input=true does not imply stop=true
control=true does not imply managedLifecycle=true
```

M2/M3 lifecycle actions are available only when `managedLifecycle=true`.
Mobile must branch on capability/ownership, not adapter names.

## Local terminal hosts

Terminal.app, VS Code Integrated Terminal, iTerm2, Ghostty, Warp, and a tmux
pane are potential host UIs for the `pokit run` CLI. They are not runtime
adapters and do not provide arbitrary attachment to already-running tabs.

Generic path:

```text
POSIX terminal host
→ pokit run
→ 0600 Unix socket subscriber
→ Recorder
→ controlled PTY
```

Minimum host contract:

- macOS/POSIX TTY stdin and stdout;
- raw mode support;
- terminal size query;
- common ANSI/DEC mode handling;
- Unix domain socket support;
- terminal mode restoration after EOF/error/signal.

If stdin/stdout is not a supported TTY, CLI must fail clearly or degrade to
detached mode; it must not claim interactive attach.

## Current evidence matrix

| Host | Attach | Input | Resize | Exit restore | Evidence |
| --- | --- | --- | --- | --- | --- |
| macOS Terminal.app | yes | yes | yes | yes | real-device/manual verified |
| VS Code Integrated Terminal | yes | yes | yes | yes | real-device/manual verified |
| iTerm2 | unverified | unverified | unverified | unverified | candidate |
| Ghostty | unverified | unverified | unverified | unverified | candidate |
| Warp | unverified | unverified | unverified | unverified | candidate |
| tmux pane as host | unverified as E10b host | unverified | unverified | unverified | distinct from tmux adapter tests |
| non-TTY/pipe | unsupported | unsupported | unsupported | N/A | detached/error fallback |

Do not infer host compatibility from adapter tests. tmux adapter verification
does not prove that running `pokit run` inside a tmux pane satisfies the local
host contract.

## Geometry ownership

MVP contract:

- one local attached controller is the geometry owner;
- that host may resize the shared controlled PTY;
- mobile/web mirror PTY geometry and do not resize it;
- simultaneous local geometry owners are not supported in MVP.

A future multi-host implementation must define an explicit primary controller
or resize arbitration policy before being accepted.

## Multiple viewers and writers

- multiple viewers are supported through Recorder subscriptions;
- simultaneous writers are best-effort and may interleave in MVP;
- this limitation must be documented;
- a future controller lease/input-owner model is preferred before multi-user
  collaboration.

## M1.5 acceptance

- API/session DTO exposes ownership or an equivalent `managedLifecycle`
  capability without vendor-specific mobile branching;
- controlled_pty is managed;
- tmux is external attachable;
- cmux is external observer;
- unknown adapter uses the safe external/observe-only default;
- M2 Stop/Kill cannot target external sessions through the managed lifecycle
  route;
- local host compatibility is tracked separately from adapter capability;
- Terminal.app and VS Code evidence is recorded as manual product validation;
- geometry and multiple-writer limitations are explicit.

## Non-goals

- attaching to arbitrary existing Terminal.app/VS Code/iTerm2/Ghostty/Warp tabs;
- new terminal-specific adapters;
- private macOS APIs, Accessibility scraping, or TTY injection;
- proving every terminal host before M2;
- implementing lifecycle Stop/Kill in M1.5.
