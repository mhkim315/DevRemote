# POKIT Device-Test Agent Onboarding

**Authority:** operational onboarding for emulator and Android physical-device
testing

**Audience:** a fresh testing agent with no prior conversation context

**Last reviewed:** 2026-07-24

**Companion reference:** [`DEVICE_TESTING_METHODS.md`](DEVICE_TESTING_METHODS.md)

This document is the single entry point for preparing and operating a POKIT
device-test run. It explains how to establish identity, prepare the three
terminal panes, capture evidence, execute the bounded scenarios, and stop
safely.

It does **not** authorize implementation changes, production diagnostics,
device revocation, owner recovery, application-data deletion, artifact
rebuilding, or acceptance of a milestone. Those actions require an explicit
test packet or separate approval.

---

## 1. Operating rules

The testing agent must follow these rules before running any command:

1. Repository truth, artifact identity, and device identity are discovered
   from the local system. A worker report is not evidence by itself.
2. Test only the exact candidate, daemon, and APK identified by the current
   handoff packet.
3. Do not rebuild a frozen artifact. A rebuild has a new identity and requires
   a new candidate freeze.
4. Do not use an emulator result as physical-device evidence.
5. Do not describe a human-visible property as PASS unless a human observed it
   or an accepted native UI assertion proves it.
6. Do not automatically revoke a device, recover an owner, clear application
   data, remove host trust, or purge a registry.
7. Never print or commit bearer tokens, pairing bootstrap secrets, private
   keys, raw QR payloads, Authorization headers, or unrestricted log archives.
8. Start evidence capture before the first state-changing action.
9. Stop on an identity mismatch. Do not “fix” the mismatch during the run.
10. Clean up only processes and files created by the current run.

The valid final outcomes are:

- `PASS`: every required automatic assertion and human gate passed;
- `FAIL`: a contract, identity, security, or behavioral assertion failed;
- `INCOMPLETE`: evidence or a required human/device action is missing.

`INCOMPLETE` must never be promoted to `PASS`.

---

## 2. Required handoff packet

Do not begin setup until the Coordinator provides all required values below.
Copy them into a private run-local file; do not edit a tracked repository file.

```sh
export POKIT_REPO="/absolute/path/to/DevRemote"
export EXPECTED_BRANCH="feature/canonical-timeline-foundation"
export EXPECTED_SOURCE_SHA="<40-character-source-sha>"
export EXPECTED_DAEMON_SHA256="<64-character-daemon-sha256>"
export EXPECTED_APK_SHA256="<64-character-apk-sha256>"
export DAEMON_ARTIFACT="/absolute/path/to/pokit-daemon"
export APK_ARTIFACT="/absolute/path/to/pokit-app-release.apk"
export ANDROID_SERIAL="<adb-device-serial>"
export EXPECTED_ANDROID_MODEL="SM-S926N"
export TEST_PROFILE="physical-smoke"
```

The handoff must also identify:

- acceptance contract and exact section under test;
- whether the run is emulator or physical-device evidence;
- allowed state changes;
- forbidden state changes;
- required scenario list;
- required human observations;
- current owner/member expectation;
- whether destructive reset/revoke is authorized;
- expected daemon endpoint or production tunnel;
- evidence destination;
- who may issue the final verdict.

If any identity is `UNSET`, abbreviated, contradictory, or supplied only in
prose without an exact value, return `BLOCKED_HANDOFF_INCOMPLETE`.

### 2.1 Recommended handoff record

Create `$RUN_PRIVATE/handoff.env` with mode `0600` after the evidence directory
is created. It may contain public hashes and local paths, but no bearer token,
QR secret, private key, or password.

```text
EXPECTED_BRANCH=...
EXPECTED_SOURCE_SHA=...
EXPECTED_DAEMON_SHA256=...
EXPECTED_APK_SHA256=...
DAEMON_ARTIFACT=...
APK_ARTIFACT=...
ANDROID_SERIAL=...
EXPECTED_ANDROID_MODEL=SM-S926N
TEST_PROFILE=physical-smoke
CONTRACT_REF=docs/...md#section
```

---

## 3. Supported profiles and proof boundary

### 3.1 `emulator-fast`

Purpose: repeatable functional regression.

May prove:

- app launch and navigation;
- insecure-local session discovery;
- Terminal rendering and control availability;
- input request/acknowledgment behavior;
- reconnect behavior;
- deterministic screenshots and logs.

Does not prove:

- physical-device Keystore behavior;
- production pairing or tunnel security;
- real notification delivery;
- SM-S926N rendering or touch behavior;
- frozen release artifact behavior if a debug APK is used.

### 3.2 `physical-smoke`

Purpose: non-destructive test of the frozen release artifacts on the exact
physical device.

May prove:

- installed APK identity;
- QR pairing and local approval;
- device identity and permission restoration;
- HTTPS/WSS operational transport;
- managed session discovery;
- exact-generation input and Terminal behavior;
- N1 notification-to-action behavior;
- reconnect and stale-generation handling.

It must preserve existing host trust and registry state unless the handoff
explicitly authorizes otherwise.

### 3.3 `physical-destructive`

Purpose: clean-install, revoke, replacement, and stale-credential testing.

This profile is disabled by default. It requires all of:

- exact target device ID;
- `--allow-app-data-reset`;
- `--allow-device-revoke`;
- explicit Coordinator or user approval;
- a recovery plan for the current owner;
- evidence capture already running.

Never infer destructive authorization from the words “clean test.”

---

## 4. Host prerequisites

The host is macOS. Confirm tools without installing or upgrading anything:

```sh
command -v git
command -v adb
command -v shasum
command -v unzip
command -v go
command -v lsof
command -v python3
command -v aapt2 || true
```

For emulator or source-level mobile work, also inspect:

```sh
command -v node
command -v npm
command -v emulator
java -version
```

If a required tool is absent, report it. Do not install packages, update
Gradle, run `npm install`, or change the lockfile without explicit authority.

### 4.1 Repository preflight

```sh
cd "$POKIT_REPO"
git status --short --branch
git rev-parse --show-toplevel
git rev-parse HEAD
git rev-parse '@{upstream}'
git merge-base --is-ancestor "$EXPECTED_SOURCE_SHA" HEAD
git diff --check
git ls-files --others --exclude-standard
```

For a frozen-candidate run, require:

- repository root equals `POKIT_REPO`;
- current branch equals `EXPECTED_BRANCH`;
- local HEAD equals its tracked upstream unless the contract says otherwise;
- tracked and untracked worktree is clean;
- expected source SHA is the intended candidate or accepted ancestor;
- no active implementation writer owns the worktree.

If HEAD is documentation evidence after the frozen candidate, compare the
candidate-to-HEAD diff and prove that no production/mobile file changed.

### 4.2 Device preflight

Do not restart the ADB server unless necessary; restarting removes reverse
rules and can disturb another test.

```sh
adb devices -l
adb -s "$ANDROID_SERIAL" get-state
adb -s "$ANDROID_SERIAL" shell getprop ro.product.model
adb -s "$ANDROID_SERIAL" shell getprop ro.build.fingerprint
adb -s "$ANDROID_SERIAL" shell getprop ro.build.version.release
adb -s "$ANDROID_SERIAL" shell getprop ro.build.version.sdk
```

Require:

- exactly the expected serial is selected;
- state is `device`, not `offline` or `unauthorized`;
- model equals `EXPECTED_ANDROID_MODEL`;
- the screen is unlocked before UI actions;
- no command uses an implicit default device when more than one is attached.

Every ADB command in a multi-device environment must include
`-s "$ANDROID_SERIAL"`.

---

## 5. Evidence directory and privacy

Create one run-owned directory before starting the daemon or app:

```sh
umask 077
RUN_ID="$(date -u +%Y%m%dT%H%M%SZ)-device"
EVIDENCE_ROOT="${POKIT_EVIDENCE_ROOT:-$HOME/Desktop/pokit-device-runs}"
RUN_DIR="$EVIDENCE_ROOT/$RUN_ID"
RUN_PRIVATE="$RUN_DIR/raw-private"
RUN_SANITIZED="$RUN_DIR/sanitized"
RUN_SCREENSHOTS="$RUN_DIR/screenshots"
mkdir -p "$RUN_PRIVATE" "$RUN_SANITIZED" "$RUN_SCREENSHOTS"
mkdir -p "$RUN_DIR/pids"
chmod 700 "$RUN_DIR" "$RUN_PRIVATE"
```

Expected output:

```text
<run-id>/
├── manifest.json
├── transitions.jsonl
├── artifact-identity.json
├── device-identity.json
├── assertions.json
├── human-gates.json
├── pids/
├── raw-private/
├── sanitized/
├── screenshots/
└── REPORT.md
```

`raw-private/` is never staged, committed, uploaded, or pasted into a prompt.
Sanitization must cover at least:

- `Authorization` and bearer values;
- `bootstrapToken` and pairing secrets;
- raw pairing payloads;
- private keys and seed material;
- cookies and Tapflow PATs;
- signed URL query strings;
- unrestricted environment dumps;
- user home paths where not needed for reproduction.

Keep event IDs, session IDs, generations, public fingerprints, timestamps,
artifact hashes, HTTP status, and bounded error categories when they are needed
for evidence.

After creating the directory, put the non-secret shared terminal variables in
`$RUN_PRIVATE/context.env`, set it to mode `0600`, and source that same file in
T1, T2, and T3:

```sh
chmod 600 "$RUN_PRIVATE/context.env"
set -a
. "$RUN_PRIVATE/context.env"
set +a
```

Use shell-quoted absolute paths in this file. Inspect it before sourcing it; it
is configuration, not an executable supplied by an untrusted party.

---

## 6. Three-terminal setup

Use three fresh terminal panes. All panes must use the same `RUN_DIR`,
`ANDROID_SERIAL`, and artifact paths.

### T1 — test controller

Responsibilities:

- preflight and artifact verification;
- scenario execution;
- screenshots and state snapshots;
- human-gate prompts;
- report generation;
- cleanup.

```sh
cd "$POKIT_REPO"
export ANDROID_SERIAL="<exact-serial>"
export RUN_DIR="<absolute-run-directory>"
```

### T2 — daemon and daemon log

Start only the verified daemon artifact. Do not trust a filename such as
`/tmp/devremote_fresh` without hashing it first.

```sh
umask 077
"$DAEMON_ARTIFACT" daemon > "$RUN_PRIVATE/daemon.log" 2>&1 &
DAEMON_PID=$!
printf '%s\n' "$DAEMON_PID" > "$RUN_DIR/pids/daemon.pid"
```

For an emulator-only insecure profile, use the separately authorized local
port. Never describe that result as production transport evidence.

### T3 — Android logcat

```sh
adb -s "$ANDROID_SERIAL" logcat -c
adb -s "$ANDROID_SERIAL" logcat -v threadtime \
  > "$RUN_PRIVATE/android.log" 2>&1 &
LOGCAT_PID=$!
printf '%s\n' "$LOGCAT_PID" > "$RUN_DIR/pids/logcat.pid"
```

Do not use `pkill adb`, `killall`, or broad process-name termination during
cleanup. Record and terminate only the PIDs created by the run.

---

## 7. Artifact identity gate

Run this gate before installing or launching anything.

### 7.1 Daemon

```sh
test -f "$DAEMON_ARTIFACT"
ACTUAL_DAEMON_SHA256="$(shasum -a 256 "$DAEMON_ARTIFACT" | awk '{print $1}')"
test "$ACTUAL_DAEMON_SHA256" = "$EXPECTED_DAEMON_SHA256"
go version -m "$DAEMON_ARTIFACT"
```

Require:

- SHA-256 equals the handoff value;
- `vcs.revision` equals the frozen source candidate;
- `vcs.modified=false`;
- architecture is valid for the host.

### 7.2 APK bundle

```sh
test -f "$APK_ARTIFACT"
ACTUAL_APK_SHA256="$(shasum -a 256 "$APK_ARTIFACT" | awk '{print $1}')"
test "$ACTUAL_APK_SHA256" = "$EXPECTED_APK_SHA256"
unzip -p "$APK_ARTIFACT" assets/PB_DEVICE_CANDIDATE_SHA.txt
unzip -p "$APK_ARTIFACT" assets/DAEMON_SHA256.txt
```

Require:

- full APK SHA matches;
- embedded candidate matches `EXPECTED_SOURCE_SHA`;
- embedded daemon digest matches `EXPECTED_DAEMON_SHA256`;
- package/application identity matches the contract;
- manifest transport policy matches the contract.

### 7.3 Installed APK

After installation:

```sh
APK_REMOTE="$(
  adb -s "$ANDROID_SERIAL" shell pm path com.pokit.mobile |
    sed -n '1s/^package://p' |
    tr -d '\r'
)"
test -n "$APK_REMOTE"
adb -s "$ANDROID_SERIAL" pull "$APK_REMOTE" "$RUN_PRIVATE/installed.apk"
INSTALLED_APK_SHA256="$(
  shasum -a 256 "$RUN_PRIVATE/installed.apk" | awk '{print $1}'
)"
test "$INSTALLED_APK_SHA256" = "$EXPECTED_APK_SHA256"
```

If the platform splits the installed APK, record every returned package path
and stop for a contract-aware verifier rather than comparing only the first
file.

Any artifact mismatch produces `FAIL_ARTIFACT_IDENTITY` and stops the run.

---

## 8. Evidence capture helpers

### 8.1 Screenshot

```sh
capture_screen() {
  name="$1"
  adb -s "$ANDROID_SERIAL" exec-out screencap -p \
    > "$RUN_SCREENSHOTS/$name.png"
}
```

Capture before and after each human gate and every failure.

### 8.2 Device state

```sh
adb -s "$ANDROID_SERIAL" shell dumpsys activity activities \
  > "$RUN_PRIVATE/activity.txt"
adb -s "$ANDROID_SERIAL" shell dumpsys package com.pokit.mobile \
  > "$RUN_PRIVATE/package.txt"
adb -s "$ANDROID_SERIAL" shell getprop \
  > "$RUN_PRIVATE/device-properties.txt"
```

### 8.3 Daemon state

Use the exact verified CLI/daemon artifact:

```sh
"$DAEMON_ARTIFACT" devices > "$RUN_PRIVATE/devices.txt"
"$DAEMON_ARTIFACT" audit --limit 20 > "$RUN_PRIVATE/audit.txt"
lsof -nP -iTCP -sTCP:LISTEN > "$RUN_PRIVATE/listeners.txt"
```

If a command is unsupported by the candidate, record the bounded error. Do not
substitute an unrelated globally installed CLI.

### 8.4 Transition record

Every scenario step should append:

```json
{
  "sequence": 1,
  "state": "ARTIFACT_VERIFY",
  "startedAt": "RFC3339",
  "finishedAt": "RFC3339",
  "result": "PASS",
  "assertions": ["daemon_sha_match", "apk_sha_match"],
  "evidence": ["artifact-identity.json"],
  "humanConfirmed": false
}
```

Use monotonically increasing sequence numbers. Never rewrite a prior result;
append a correction that references the earlier sequence.

---

## 9. Physical-smoke scenario

The exact contract may add or remove steps. This is the minimum standard
sequence.

### 9.1 Device ready

1. Confirm frozen APK identity.
2. Install the exact APK with the contract-approved install command.
3. Pull the installed APK and compare its hash.
4. Confirm package version and activity.
5. Confirm the verified daemon and production tunnel are ready.
6. Confirm operational endpoint returns the expected unauthenticated status
   before pairing, not a successful anonymous response.

### 9.2 Human gate: QR scan

Prompt:

```text
[HUMAN GATE QR_SCAN]
Scan the displayed QR with SM-S926N.
Confirm:
- the QR fits and has no visible corruption;
- the app leaves the camera view;
- the app shows a pending local-approval state;
- the app does not claim connection before Mac approval.

Enter: pass / fail / abort
```

Record the answer, timestamp, screenshot, and current daemon audit state.

### 9.3 Human gate: host approval

Prompt the user to approve locally in `pokit pair`. The test agent must not
approve remotely or expose a remote approval endpoint.

After approval, record:

- public device ID/fingerprint;
- owner/member role;
- effective permission names;
- pairing timestamp;
- host public fingerprint;
- HTTP/transport transition evidence.

Do not record the QR payload or bootstrap secret.

### 9.4 Authentication restoration

1. Verify authenticated session-list access.
2. Force-stop the app without clearing data.
3. Relaunch it.
4. Verify the same device identity is restored.
5. Verify permissions are revalidated by the server.
6. Verify there is no `missing bearer token` race.
7. Verify operational REST and WebSocket use the accepted secure transport.

Simple app or daemon restart must not silently create a new device identity.

### 9.5 Managed session

Create or select a contract-approved managed Codex/Claude or shell fixture.
Record:

- session ID;
- runtime ID;
- exact generation;
- provider/profile;
- lifecycle state;
- adapter/session capabilities;
- device effective permissions.

Do not use an IPC bypass unless the contract explicitly permits it. An IPC
fixture does not prove the user-facing session-creation flow.

### 9.6 Terminal and acknowledged input

Use a run-unique marker:

```text
printf 'POKIT_DEVICE_<RUN_ID>\n'
```

Require:

- session supports input;
- device is authorized to input;
- input request is bound to the exact generation;
- accepted acknowledgment is received;
- the marker appears in PTY output;
- the UI says delivered to terminal only after acknowledgment;
- no claim is made that shell command success follows merely from delivery.

Then verify:

- Ctrl+C interrupts the intended foreground operation;
- at least one control macro;
- orientation or resize geometry;
- background/foreground reconnect;
- no character-per-line or ANSI rendering regression;
- Terminal remains accessible as a fallback.

Human observation is required for visual ANSI/layout quality.

### 9.7 N1 exact-event notification

Trigger an accepted `ApprovalRequested` or `AgentBlocked` event.

Record:

- canonical event ID;
- session/runtime/generation;
- event kind;
- notification timestamp;
- current authority state before tap.

Human gate:

```text
[HUMAN GATE N1_TAP]
Tap the POKIT notification.
Confirm that it opens the exact relevant Activity event and exposes only
currently authorized actions.

Enter: pass / fail / abort
```

The app must re-query server authority on tap. Exercise the contract-required
closed outcomes:

- `actionable`;
- `already_resolved`;
- `stale_generation`;
- `session_unavailable`;
- `insufficient_permission`;
- `canonical_event_unavailable`;
- `event_degraded_or_gap`.

A stale, resolved, unavailable, degraded, or unauthorized notification must
never replay an action. Safe fallbacks are Activity context, Transcript,
Terminal, preserved evidence, or the session list as defined by the contract.

### 9.8 Optional destructive scenarios

Do not execute revoke, `pm clear`, owner recovery, replacement pairing, or host
trust removal under `physical-smoke`.

If the authorized profile is `physical-destructive`, freeze the before-state
first and require a second confirmation immediately before the destructive
command. Revoke only the exact device ID; never enumerate all active devices
and revoke them as a group.

---

## 10. Emulator automation

The emulator is the preferred place to automate navigation, deterministic
input, reconnect, and failure injection.

Before starting:

- prove the named AVD is not already running;
- use `-no-snapshot`;
- use a run-owned emulator log;
- wait for `sys.boot_completed=1`;
- establish only the documented `adb reverse` rules;
- do not call emulator results physical proof.

The existing Detox scaffold is under `mobile/.detoxrc.js` and `mobile/e2e/`.
It currently assumes that the app is already at a writable Terminal screen.
Therefore it is a scaffold, not a complete onboarding E2E suite.

When using Detox:

- record the debug/test APK identity separately;
- do not claim the instrumented APK equals the frozen release APK;
- automate setup and navigation before relying on macro assertions;
- verify actual input acknowledgment and PTY marker, not only button
  visibility;
- keep release physical-smoke evidence separate.

Tapflow may assist exploratory emulator control, but it is not a required
authority. If used:

- bind the service to localhost;
- generate a unique password and short-lived PAT per run;
- never use the example password from the methods guide;
- store PAT/cookies only under `raw-private/`;
- delete them during cleanup;
- fall back to ADB or Detox when React Native Modal controls are inaccessible;
- do not weaken an assertion merely because Tapflow cannot tap it.

---

## 11. Failure handling

Stop immediately and mark `FAIL` for:

- daemon, APK, embedded identity, or installed APK mismatch;
- dirty or unexpected build provenance;
- pairing authority created before local approval;
- completed, expired, or canceled invitation reuse;
- secret/private material in stdout, stderr, logcat, argv, or report;
- operational API falling back to unauthorized cleartext;
- identity changing after an ordinary restart;
- revoked device retaining REST, WebSocket, refresh, or input access;
- wrong-session or wrong-generation intervention;
- stale/resolved notification replaying an action;
- Timeline failure blocking native runtime, approval, input, or lifecycle
  authority;
- required raw/sanitized evidence being unavailable.

Mark `INCOMPLETE` for:

- disconnected or locked device;
- missing human confirmation;
- notification delivery unavailable due to external service;
- test interrupted before cleanup/report;
- an unsupported tool that prevents a required proof;
- evidence capture starting after the tested action.

For every failure:

1. stop further state-changing scenarios;
2. capture screenshot, activity, device, daemon, and audit state;
3. preserve raw-private evidence;
4. generate a sanitized failure summary;
5. record exact last completed transition;
6. do not modify product code to diagnose inside the evidence run.

If diagnostics are needed, close the run as `FAIL` or `INCOMPLETE`, create a
separate diagnostic packet, and assign a new artifact identity to any changed
build.

---

## 12. Cleanup

Cleanup is bounded to run-owned resources.

1. Stop only PIDs recorded in `$RUN_DIR/pids/`.
2. Remove only ADB reverse entries created by this run.
3. Do not kill unrelated emulator, Metro, daemon, tunnel, or ADB processes.
4. Preserve raw-private evidence until the verifier confirms that sanitized
   evidence is sufficient.
5. Remove ephemeral Tapflow credentials and cookies.
6. Confirm repository status is unchanged.
7. Record cleanup results in `transitions.jsonl`.

```sh
cd "$POKIT_REPO"
git status --short --branch
git diff --check
```

The testing run must not leave tracked or untracked repository files.

---

## 13. Report and verifier handoff

`REPORT.md` must contain:

### Identity

- contract reference;
- source candidate SHA;
- evidence/repository HEAD;
- daemon SHA-256 and Go build provenance;
- APK SHA-256 and embedded identities;
- installed APK SHA-256;
- branch/upstream/worktree state;
- macOS version and architecture;
- Android serial (redacted if publication policy requires it), model, version,
  SDK, and build fingerprint;
- application ID, version name, and version code;
- host public fingerprint;
- device public fingerprint;
- role and effective permissions.

### Scenario matrix

For every step:

- `PASS`, `FAIL`, `INCOMPLETE`, or separately identified known issue;
- automatic assertions;
- human observation;
- exact evidence references;
- timestamps;
- session/runtime/generation/event IDs where relevant.

### Security-negative results

- no authority before local approval;
- no secret output;
- no operational cleartext fallback;
- stale-generation rejection;
- no resolved notification replay;
- revoke/replacement behavior if authorized and executed.

### Known issues

Known issues must identify whether they are:

- unchanged from the accepted baseline;
- newly introduced;
- not exercised;
- outside the current contract.

Do not convert a failed required gate into a known issue after execution.

### Final statement

The testing agent reports evidence only:

```text
DEVICE_RUN_RESULT=PASS|FAIL|INCOMPLETE
RUN_ID=<id>
SOURCE_SHA=<sha>
DAEMON_SHA256=<sha>
APK_SHA256=<sha>
REPORT=<absolute-path>
```

The testing agent does not set an ACCEPT SHA or declare milestone acceptance.
A fresh independent verifier checks the frozen artifacts, transition record,
redaction, assertions, and human confirmations.

---

## 14. First-session checklist

A fresh agent should be able to answer every line before testing:

- [ ] I have the exact contract reference.
- [ ] I have full source, daemon, and APK hashes.
- [ ] I know whether this is emulator, non-destructive physical, or destructive
      physical testing.
- [ ] I verified branch, HEAD, upstream, ancestry, and clean worktree.
- [ ] I selected the exact ADB serial and model.
- [ ] I created a private evidence directory with mode `0700`.
- [ ] I verified daemon and APK identity before launch/install.
- [ ] I started daemon and logcat capture before the first action.
- [ ] I know which observations require the user.
- [ ] I will not revoke, recover, clear, rebuild, or purge without authority.
- [ ] I will record exact session, generation, event, and artifact identity.
- [ ] I will redact secrets while preserving reproducible evidence.
- [ ] I will stop on identity or security mismatch.
- [ ] I will report `INCOMPLETE` rather than infer missing evidence.
- [ ] I will leave repository and unrelated processes unchanged.

If any item is unchecked, do not start the device gate.
