# Pokit Executor Onboarding — M3-auth-R1 Android End-to-End Re-verification

Status: **READY — begin only from the accepted M3-auth-4A baseline**

Canonical repository: `https://github.com/mhkim315/DevRemote.git`

Branch: `feature/phase10-multi-adapter`

Required baseline (full hash):

```text
b34c33528dd50dd6daa8321b358a3fa3e5aca90b
```

Baseline subject:

```text
fix(M3-auth-4A): host-bind the central device transport, fail closed cross-origin
```

M3-auth-4A final verdict: **ACCEPT**

This document is the authoritative handoff for the next execution agent. The
next phase is `M3-auth-R1`: authenticated Android M3a end-to-end
re-verification. Do not start iOS, M3b lifecycle controls, Transcript, or new
authentication architecture.

## 0. If this commit or document is missing

Do not reconstruct the phase from memory and do not work from a stale local
branch. Run:

```sh
git remote set-url origin https://github.com/mhkim315/DevRemote.git
git fetch origin feature/phase10-multi-adapter
git show b34c33528dd50dd6daa8321b358a3fa3e5aca90b --stat
git show origin/feature/phase10-multi-adapter:docs/NEXT_SESSION_M3_AUTH_R1_HANDOFF.md
```

If the worktree is clean and already on the feature branch:

```sh
git switch feature/phase10-multi-adapter
git rebase origin/feature/phase10-multi-adapter
```

Before editing, verify:

```sh
git rev-parse HEAD
git merge-base --is-ancestor b34c33528dd50dd6daa8321b358a3fa3e5aca90b HEAD
```

The second command must exit `0`. If it does not, stop: the worktree is not
based on the accepted implementation.

If local changes exist, preserve them. Do not reset, force-checkout, or discard
another agent's work. Inspect the handoff with `git show` first and coordinate
before rebasing.

## 1. Product and security invariants

Pokit is a local-first runtime connecting the user's computer and paired phone.

```text
controlled PTY
  → one session-owned Recorder (only PTY reader)
  → local/mobile/web subscribers
```

Hard invariants:

1. Recorder is the only PTY reader.
2. Recorder subscriber data is raw PTY output only.
3. Remote REST authorization uses the paired device bearer.
4. Remote Terminal WebSocket authorization uses a one-time, session-bound
   ticket.
5. Device bearer, Supabase JWT, and dev token never enter remote Terminal URLs,
   HTML, WebView state, logs, or navigation state.
6. Device bearer is transmitted only to the exact canonical paired origin.
7. `paired_device` does not require a Supabase session.
8. `explicit_local_dev` is the only legacy-token mode.
9. Mobile mirrors host PTY geometry; mobile does not resize the shared PTY.
10. WebSocket/viewer disconnect does not stop the process.

Do not weaken these contracts to make M3a easier to wire.

## 2. What M3-auth-4A accepted

The accepted chain at `b34c335` is:

```text
paired host + Android hardware DeviceKey
  → challenge-authenticated device bearer
  → host-bound central GET transport
  → authenticated session discovery
  → selected session
  → REST-issued one-time WS ticket
  → authenticated Terminal HTML bootstrap
  → ticket-bound WebSocket
  → binary raw PTY / text control framing
  → reconnect tickets A → B → C
```

Accepted implementation includes:

- explicit `AuthMode` and fail-closed routing;
- pairing save and atomic trusted-state installation;
- paired-device navigation without Supabase;
- host-bound `DeviceAuth { tokenManager, origin }`;
- cross-origin rejection before any fetch;
- device-authenticated probe/session/history/activity GETs;
- exact 64-character lowercase-hex WS ticket validation;
- ephemeral ticket URLs and reconnect singleflight;
- binary raw input/output and text geometry control framing;
- real generated-script execution tests;
- daemon replay/wrong-binding/revoke/replacement/expiry proofs;
- Recorder raw-stream and single-reader preservation.

Do not reopen or refactor this architecture without a concrete production
defect.

## 3. Independent verification evidence

Independent review of `b34c335` found no remaining M3-auth-4A blocker.

Clean detached checkout evidence:

- `go build`: PASS
- `go vet`: PASS
- `go test -race ./...`: PASS
- mobile TypeScript: PASS
- Jest: PASS (`201` tests at acceptance)

The clean checkout did not contain generated `android/gradlew`, so the verifier
could not independently rerun the Kotlin compile there. The implementation
agent reported the complete build gate passing. Treat physical Android/WebView
validation as an explicit R1 release gate, not as a reason to redesign the
accepted transport.

## 4. M3-auth-R1 objective

Prove and, only where necessary, complete the authenticated Android M3a flow:

```text
paired Android device
  → app cold start
  → DeviceKey + TokenManager restore
  → authenticated daemon probe
  → authenticated session profile list
  → create controlled_pty session from typed profile
  → returned canonical running session
  → session appears in dashboard
  → open ticket Terminal
  → bidirectional raw input/output
  → background/foreground reconnect with fresh ticket
  → natural exit shows ended state and preserves history
```

This phase is re-verification plus the smallest missing authenticated M3a REST
wiring. It is not a broad UX redesign.

## 5. Required code-path analysis before editing

Trace these actual production paths:

- `mobile/App.tsx`
- `mobile/src/lib/client.ts`
- `mobile/src/lib/authTransport.ts`
- `mobile/src/lib/connection.tsx`
- `mobile/src/lib/authMode.ts`
- `mobile/src/components/NewSessionModal.tsx`
- `mobile/src/screens/dashboard/DashboardScreen.tsx`
- `mobile/src/screens/FeedScreen.tsx`
- `mobile/src/lib/terminalController.ts`
- daemon route registration in `companion-daemon/cmd/devremote/app.go`
- session creation/profile handlers in `companion-daemon/internal/term`

Determine exactly which M3a calls still use the legacy token transport.
Expected likely gaps include `GET /api/session-profiles` and
`POST /api/sessions`. Confirm from code; do not assume.

## 6. Required implementation boundary

### Authenticated reads

All remote paired-device reads needed by M3a must use the host-bound device
transport. At minimum verify:

- daemon probe;
- session list;
- session profiles;
- session/history/activity reads needed by the opened view.

### Authenticated create

M3a New Session must create through:

```http
POST /api/sessions
Authorization: Bearer <device bearer>
Content-Type: application/json

{"profileId":"shell|codex|claude", "name":"...", "cwd":"..."}
```

Only the accepted typed payload may be sent. The mobile must never send an
executable path, shell command, runner, or caller-chosen canonical session ID.

Do not blindly retry this non-idempotent POST after a 401. Refresh before the
operation or surface a recoverable authentication error without creating a
duplicate session.

### Explicitly out of scope

- Stop/Kill/Delete mobile UX (`M3b`)
- custom arbitrary-command security expansion
- iOS (`M3-auth-1B`, `M3-auth-2B`)
- Transcript work
- notifications/orchestrator
- Windows

## 7. Required automated acceptance proofs

Use the actual production client/controller functions, not copied test helpers.

### Cold start and routing

- paired device restores TokenManager and host-bound origin;
- paired device reaches product without Supabase;
- missing/corrupt pairing fails closed;
- wrong operational origin makes zero network requests;
- disconnect/re-pair cannot reuse the previous host bearer.

### Profile and create boundary

- paired-device profile list sends the device bearer, not a Supabase token;
- member/read-only principal cannot create (`403`);
- owner create sends exactly `{profileId,name,cwd}`;
- successful response must contain canonical ID, `controlled_pty`, and `running`;
- malformed 2xx response does not navigate or add a phantom session;
- failed/network/401/403 create leaves the form open and adds no phantom card;
- duplicate submit produces one POST;
- non-idempotent create is not automatically replayed after 401.

### Session to Terminal

- returned session appears through authenticated list refresh;
- selecting it uses the ticket path, never `terminalURL(...token...)`;
- Terminal bootstrap receives no reusable credential;
- reconnect rotates tickets and session switch discards stale work;
- raw input/output and geometry framing regressions remain green.

### Daemon/Recorder regression

- creation is capability/profile driven;
- Recorder starts before the success response contract is exposed;
- Recorder remains the sole reader;
- capture continues without a WebSocket viewer;
- tmux/localpty behavior does not regress;
- cmux remains observe/best-effort only.

## 8. Real-device Android gate

If a Samsung device is available, run the actual release flow:

1. install the fresh release APK;
2. pair over LAN QR;
3. switch to the configured HTTPS tunnel/LTE path;
4. cold-start the app without a Supabase login;
5. create Shell, Codex, and Claude profiles where installed;
6. verify each opens the ticket Terminal;
7. type from mobile and local terminal;
8. background/foreground the app;
9. verify reconnect and fresh output;
10. resize Terminal.app/VS Code and verify mobile geometry mirrors it;
11. exit naturally and verify ended/history behavior.

Record exact APK hash/build, daemon commit, device model, Android version, and
observations. If no device is available, do not claim the physical gate passed.

## 9. Validation policy

`BLOCKER`:

- bearer sent to the wrong origin;
- Supabase/dev token authorizes remote M3a operation;
- duplicate session creation;
- wrong create payload or capability contract;
- navigation before Recorder-ready running response;
- phantom session on failure;
- Recorder ownership violation;
- Terminal bearer exposure or ticket replay;
- local terminal not restored/session lifecycle broken.

`FOLLOW-UP`:

- visual polish;
- optional profiles;
- helper cleanup;
- richer loading animation;
- manual Disconnect UX refinement;
- trailing-slash normalization if existing canonicalization/redirect behavior is
  proven safe for this phase;
- old-APK compatibility documentation.

Do not demand refactoring solely for test granularity.

## 10. Commands and final report

Run targeted tests during implementation, then:

```sh
sh scripts/build-gate.sh
```

If available, also run:

```sh
sh scripts/android-native-gate.sh
```

Final report must include:

- commit hash and branch;
- baseline ancestry confirmation;
- exact REST authentication matrix;
- exact create request/response contract;
- automated test names and results;
- full gate result;
- APK/device smoke evidence or an honest deferral;
- clean worktree;
- a verifier prompt;

Commit and push, then stop for independent verification. Do not begin M3b or
iOS automatically.

## 11. Completion and next phases

M3-auth-R1 completes only after independent acceptance.

Then proceed in this order:

```text
M3-auth-1B  → iOS Secure Enclave DeviceKey provider
M3-auth-2B  → iOS QR pairing, bearer, WS-ticket, Terminal integration
M3b         → Mobile Stop/Kill/Delete lifecycle UX
```

Platform naming is fixed:

```text
A = Android
B = iOS
```

