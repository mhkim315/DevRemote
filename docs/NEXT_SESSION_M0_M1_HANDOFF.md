# Next Session Handoff — M0/M1 Mobile Session Lifecycle

Status: execution handoff
Branch: `feature/phase10-multi-adapter`
Read first: `docs/MOBILE_SESSION_LIFECYCLE_AND_TRANSCRIPT_PLAN.md`

## Mission

Implement M0/M1 only:

```text
M0  lifecycle/API contract foundation
M1  safe controlled_pty creation and daemon-owned profiles
```

Do not implement Stop/Kill/Delete UI or Transcript refactoring in this phase.

## Current validated baseline

- controlled PTY runtime is stable;
- Recorder is the single PTY reader;
- local/mobile share the same PTY;
- Terminal.app, VS Code, Codex, and Claude mobile terminal paths work;
- background/foreground reconnect and local terminal restoration work;
- PTY geometry mirror is validated from `6b40b32`.

## Current product bug to replace

Mobile `+ NEW AGENT` calls `createOrUpdateSession(id, runner, color)`. The daemon
expects a canonical adapter ID and ignores `runner` as the command. Do not patch
this mismatch with client-side string concatenation. Establish the product API.

## Required work

### 1. M0 contract

Define and test:

- lifecycle DTO with `starting`, `running`, `stopping`, `exited`, `failed`;
- daemon-generated canonical session ID;
- controlled_pty-only MVP scope;
- detach/stop/kill/delete/natural-exit meanings;
- process-group ownership audit;
- compatibility policy for current `pokit run` POST payload.

Only creation needs implementation in M1. Stop/Kill/Delete may be route/type
contracts or follow-up documentation, not runtime implementation yet.

### 2. Daemon-owned profiles

Add an authenticated read contract for initial profiles:

- Shell;
- Codex;
- Claude.

The daemon owns executable policy. Mobile owns only presentation preferences.

### 3. Safe create request

Support a request shaped like:

```json
{
  "adapter": "controlled_pty",
  "profileId": "codex",
  "name": "repo-review",
  "cwd": "/Users/example/project"
}
```

Server returns canonical ID plus lifecycle state. Do not require the mobile
client to construct `controlled_pty:<id>`.

Custom commands, if accepted at all in M1, use executable plus argv and are
disabled for remote callers by default. Do not expose untrusted `bash -c`.

### 4. Recorder production path

Creation must keep the accepted path:

```text
daemon creates controlled PTY
→ session/catalog entry exists
→ one Recorder starts
→ API exposes the session
→ viewers subscribe
```

Do not add another PTY reader or make API/mobile own capture.

## Required product-boundary tests

- `GET /api/session-profiles` returns safe Shell/Codex/Claude presets;
- valid preset request launches correct executable/argv and CWD;
- response contains canonical ID and lifecycle state;
- invalid profile, name, CWD, and unauthorized custom command are rejected;
- existing CLI create payload remains compatible;
- Recorder single-reader/no-WebSocket capture regressions pass;
- tmux/cmux creation and capability behavior are unchanged;
- auth is applied to profile/create endpoints.

Do not extract helpers solely for unit-test convenience. Test public/API and
runtime boundaries.

## Explicit non-goals

- mobile New Session UI;
- graceful Stop implementation;
- Force Kill;
- Delete History;
- Transcript projector;
- agent semantic parsing;
- cmux reliability work;
- daemon-restart persistence;
- unrestricted remote Custom command.

## Acceptance report format

Return:

```text
M0/M1 status: ACCEPT / SCOPED ACCEPT / NEEDS FOLLOW-UP
Commit: <hash>

Product contracts proven:
- ...

Tests/build:
- ...

Deferred to M2/M3:
- ...

Known limitations:
- ...
```

## Validation classification

BLOCKER:

- command injection or unrestricted remote execution;
- canonical ID/API mismatch;
- incorrect executable/argv/CWD;
- Recorder ownership regression;
- session exposed before runtime/Recorder is ready;
- auth bypass;
- CLI regression.

FOLLOW-UP:

- UI polish;
- extra profiles;
- optional helper extraction;
- advanced lifecycle presentation;
- Transcript work.
