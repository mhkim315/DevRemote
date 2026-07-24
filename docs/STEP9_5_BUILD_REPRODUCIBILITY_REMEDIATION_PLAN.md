# Step 9.5 Build Reproducibility Remediation Plan

**Status:** READY FOR INDEPENDENT CONTRACT REVIEW — IMPLEMENTATION NOT STARTED

**Authority:** mandatory prerequisite to the Step 9.5 Base Alpha artifact
freeze and SM-S926N gate

**Blocked consumer:** the device-test agent following
[`DEVICE_TEST_AGENT_ONBOARDING.md`](DEVICE_TEST_AGENT_ONBOARDING.md)

**Current test status:** `BLOCKED_ANDROID_BUILD_TOOLCHAIN` and
`BLOCKED_ARTIFACT_HANDOFF_PENDING`

---

## 1. Purpose

Step 9.5 requires one exact production source candidate and matched daemon/APK
artifacts before the physical-device gate begins. A clean artifact-production
attempt on 2026-07-24 proved that the current Android release process is not
reconstructible from the frozen source candidate without undocumented local
state.

This packet repairs only the artifact-generation boundary. It does not change
the accepted Step 9.4 pairing, authentication, device trust, approval, input,
runtime, Timeline, Transcript, Terminal, or N1 behavior.

The intended order is:

```text
Step 9.4 ACCEPT
→ 9.5-R build-reproducibility remediation
→ independent 9.5-R ACCEPT
→ exact production source candidate freeze
→ matched daemon/APK artifact bundle
→ automated Base Alpha gates
→ non-destructive SM-S926N physical-smoke
→ separately authorized revoke/replacement recovery
→ independent Step 9.5 ACCEPT
→ dogfood
```

The waiting device-test agent must not build, install, pair, clear, revoke, or
recover anything until the matched handoff packet exists.

---

## 2. Observed blocker evidence

### 2.1 Identities used by the diagnostic attempt

| Identity | Value | Classification |
| --- | --- | --- |
| Diagnostic production source | `4e36f97b190157876b54b6403b9a8b9374393f58` | Ancestor containing the accepted production/mobile tree used for the attempt; not the future post-remediation candidate |
| Documentation HEAD at discovery | `c3abf3d2c8ea6978e11e666193fa4a65a5c680be` | Documentation authority only; not an artifact source candidate |
| Diagnostic daemon SHA-256 | `ed5712b3f528cff288abf75c60e46cba8960b83affd603b1af50c0c62fb10422` | Partial diagnostic output only; not authorized for testing |
| Diagnostic APK SHA-256 | `UNSET` | Clean release build failed |

The partial directory
`/tmp/pokit-alpha-device-artifacts/` contains
`INCOMPLETE_DO_NOT_TEST.txt`. It is not an artifact handoff and may disappear
at any time. No authoritative plan or evidence may depend on that temporary
path.

### 2.2 Daemon result

A real clean clone with a real `.git` directory produced a daemon with:

- `vcs.revision=4e36f97b190157876b54b6403b9a8b9374393f58`;
- `vcs.modified=false`;
- a deterministic source commit timestamp supplied as the CLI build time; and
- SHA-256
  `ed5712b3f528cff288abf75c60e46cba8960b83affd603b1af50c0c62fb10422`.

A linked Git worktree did not emit the required VCS metadata under the observed
build environment. The remediation must either support worktrees correctly or
require a real clean clone. It must never silently accept a daemon missing
`vcs.revision` or `vcs.modified=false`.

This daemon is not independently accepted and has no matched APK. It must not
be installed or exercised by the device-test agent.

### 2.3 Android native-project failure

The repository intentionally ignores most of `mobile/android/` while retaining
a small tracked native overlay. In a clean clone:

1. `npm ci` completed from the lockfile;
2. `npx expo prebuild --platform android --no-install` detected a malformed
   partial Android project;
3. prebuild cleared and regenerated the Android directory; and
4. the regenerated manifest removed the accepted
   `android:usesCleartextTraffic="true"` setting required by the bounded
   Pairing V1 transport.

Manually restoring the generated manifest, copying an old generated Android
directory, or building from an unrecorded developer tree would produce an
unreproducible artifact and is prohibited.

### 2.4 Gradle failure

The clean prebuild generated:

- Gradle wrapper `9.3.1`;
- React Native Gradle plugin using
  `org.gradle.toolchains.foojay-resolver-convention` `0.5.0`; and
- the observed host JDK `21.0.10`.

`assembleRelease` failed with:

```text
Class org.gradle.jvm.toolchain.JvmVendorSpec does not have member field
'org.gradle.jvm.toolchain.JvmVendorSpec IBM_SEMERU'
```

Editing `node_modules`, downgrading Gradle ad hoc, copying a prior wrapper, or
using a previously generated local Android tree is not an accepted fix. The
supported toolchain must be explicitly selected, versioned, and independently
verified.

---

## 3. Frozen authority and non-goals

### 3.1 Unchanged authority

The remediation must preserve:

- provider-native managed Codex and Claude runtime authority;
- exact runtime/session/generation identity;
- `OwnedPTYRuntime`, `TerminalTransport`, and sole-reader `Recorder`;
- Pairing V1 QR payload, local approval, single-use and expiry semantics;
- bounded LAN HTTP `/pair` exception and HTTPS/WSS operational transport;
- bearer authentication and owner/member permissions;
- Android Keystore device identity;
- approval, deny, acknowledged input, interrupt, stop, and kill;
- existing Transcript authority and Terminal fallback;
- fail-open, non-authoritative Operational Timeline;
- N1 authority re-query and stale/resolved action rejection.

### 3.2 Explicit non-goals

This packet must not:

- change pairing protocol fields, cryptography, trust, roles, or permissions;
- introduce Pairing V2;
- change production REST/WebSocket routes;
- change Terminal rendering, PTY bytes, geometry, or input behavior;
- alter N1 or Timeline semantics;
- update dependencies merely to remove audit warnings;
- introduce account registration, cloud identity, or orchestration;
- commit generated build outputs, APKs, daemon binaries, caches, credentials,
  signing keys, or private logs;
- use an old APK or daemon to complete a missing half of the bundle;
- claim byte-for-byte reproducibility unless it is actually tested and
  specified;
- weaken artifact checks because Android packaging is nondeterministic.

---

## 4. Required build model

The accepted result must provide one documented command that starts from a
clean clone of an exact commit and either produces a complete matched bundle or
fails closed.

The build model must distinguish:

1. **Production source candidate:** exact commit whose production and mobile
   source is built.
2. **Packaging/evidence HEAD:** later documentation or Formula commit that may
   point to the production source candidate.
3. **Daemon artifact identity:** SHA-256 plus Go build provenance.
4. **APK artifact identity:** SHA-256 plus embedded source and daemon identity.
5. **Installed identity:** the daemon and APK bytes actually used by the
   physical-device test.

Documentation HEAD must never be substituted for production source identity.
No artifact may require its own final commit identity to be embedded in content
that determines that same commit identity.

### 4.1 Toolchain contract

The build entrypoint must validate, record, and fail closed on unsupported:

- macOS version and architecture;
- Go version;
- Node version;
- npm version;
- JDK vendor and version;
- Android SDK and build-tools versions;
- Expo package/CLI version from the lockfile;
- React Native and native Gradle plugin versions;
- Gradle wrapper version;
- package-lock digest.

The executor must derive compatible versions from maintained upstream
constraints and the locked dependency graph. It must not choose a version only
because it happens to build on one workstation.

### 4.2 Native generation contract

The clean native-project generation path must:

- start without developer-local `mobile/android` state;
- use the repository lockfile and local installed package binaries;
- reproduce every accepted native Android setting;
- preserve the Pairing V1 transport requirement through an authoritative Expo
  config/plugin or another reviewed source-controlled generation mechanism;
- preserve Keystore, notifications, camera, package identity, deep links, and
  backup rules;
- make the generated manifest and relevant Gradle inputs inspectable;
- fail if generated settings differ from the accepted contract.

The preferred direction is a narrow source-controlled Expo config plugin or
equivalent declarative generation input. Committing the entire generated
Android tree requires a separate architectural decision and is not authorized
by this plan.

### 4.3 Artifact identity contract

The build must:

- use a clean real clone, or prove equivalent VCS metadata in the supported
  checkout form;
- build daemon with `-trimpath` and required VCS metadata;
- require daemon `vcs.revision` to equal the source candidate;
- require daemon `vcs.modified=false`;
- calculate daemon SHA-256 before the APK build;
- inject the exact full source SHA and daemon SHA-256 into APK assets;
- build a release APK without local-test authentication bypass;
- extract both embedded identities from the completed APK;
- require byte equality between injected, extracted, and manifest values;
- calculate the full APK SHA-256;
- pull the installed APK during the device gate and compare it with the frozen
  APK, handling split APKs explicitly rather than checking only one file.

The build must document whether a Formula installation produces the exact
frozen daemon bytes or a separately built source artifact. Step 9.5 cannot
claim the clean Homebrew path and frozen daemon path are identical unless that
identity is measured.

---

## 5. Bounded execution waves

### 9.5-R0 — Contract and toolchain inventory

**Purpose:** freeze the actual supported build boundary before editing.

**Allowed:** documentation; read-only inspection of lockfiles, Expo generation,
Gradle/RN compatibility, Formula behavior, previous build evidence.

**Forbidden:** production/mobile behavior changes, dependency upgrades,
generated artifact claims, physical-device actions.

**Deliverables:**

- accepted toolchain tuple and upstream compatibility evidence;
- exact clean-clone build state machine;
- generated-native ownership decision;
- Pairing V1 manifest-generation requirement;
- Formula-versus-frozen-daemon identity decision;
- rollback procedure and allowed file list.

**ACCEPT:** an independent reviewer can reproduce both observed failures and
agrees the selected toolchain/generation approach preserves accepted authority.

**REJECT:** the contract relies on a developer-local generated directory,
manual manifest restoration, mutable global packages, or an unexplained
Gradle downgrade.

### 9.5-R1 — Clean native generation

**Purpose:** make the Android project derivable from committed source.

**Likely allowed paths:**

- `mobile/app.json`;
- a narrowly scoped source-controlled Expo config/plugin;
- build-specific tests for generated manifest/config;
- the accepted build entrypoint;
- the R packet contract/evidence.

The exact write scope is frozen by R0.

**Deliverables:**

- clean generation command;
- declarative Pairing V1 transport configuration;
- generated manifest/config assertions;
- proof that clean prebuild requires no manual restoration;
- proof that no generated tree or secret is committed.

**Tests:**

- clean clone/prebuild;
- generated package/application identity;
- camera/notification/deep-link permissions;
- `usesCleartextTraffic` contract;
- no debug auth bypass in release inputs;
- second clean generation with equivalent contract-relevant output.

**ACCEPT:** the authoritative native settings survive clean regeneration and
the tracked worktree remains clean after removal of run-owned outputs.

**REJECT:** the executor copies the current ignored Android tree, patches
generated output after prebuild, or alters pairing/auth semantics.

### 9.5-R2 — Supported release toolchain

**Purpose:** make release compilation succeed under one recorded compatible
toolchain.

**Deliverables:**

- pinned/validated Java, Gradle, RN plugin, Expo, Node/npm, Android SDK tuple;
- fail-closed version checks;
- release build command;
- documented cache/network behavior;
- deterministic manifest containing all build inputs and tool versions.

**Tests:**

- clean dependency install from the lockfile;
- clean prebuild;
- `assembleRelease`;
- TypeScript and Jest;
- daemon build, vet, and race tests;
- release negative checks;
- no source or generated-output drift after cleanup.

**ACCEPT:** a fresh environment produces a release APK without editing
`node_modules`, Gradle caches, or generated files by hand.

**REJECT:** success depends on undocumented cache state, an unpinned global
tool, an incompatible wrapper workaround, or a local signing secret not
declared by the contract.

### 9.5-R3 — Matched artifact freeze

**Purpose:** generate and independently verify the exact daemon/APK pair.

**Deliverables:**

```text
pokit-alpha-device-artifacts/
├── pokit-daemon
├── pokit-app-release.apk
├── SOURCE_SHA.txt
├── DAEMON_SHA256.txt
├── APK_SHA256.txt
├── APK_EXTRACTED_SOURCE_SHA.txt
├── APK_EXTRACTED_DAEMON_SHA256.txt
├── DAEMON_BUILD_INFO.txt
├── TOOLCHAIN.json
└── MANIFEST.json
```

**Required checks:**

- full source SHA and ancestry;
- clean clone and worktree;
- daemon `vcs.revision` and `vcs.modified=false`;
- daemon SHA-256;
- APK SHA-256;
- embedded/extracted identity equality;
- packaged JS present;
- release cleartext policy matches bounded Pairing V1;
- local-test bypass absent;
- artifact files immutable for the device run;
- no secret or private signing material in the bundle.

**ACCEPT:** a fresh verifier independently computes every identity and the
manifest contains no model-originated value that could have been derived
deterministically.

**REJECT:** either artifact is rebuilt after freeze, identities are
abbreviated, daemon and APK source differ, or a partial artifact is reused.

### 9.5-R4 — Automated Base Alpha closeout

**Purpose:** prove the candidate is eligible to enter the physical gate.

**Tests:**

- daemon build/vet/race;
- mobile TypeScript/Jest;
- clean native prebuild/release build;
- Timeline fail-open and degradation regression;
- Transcript/Activity/N1 tests;
- approval/input/generation/reconnect tests;
- pairing/auth/revoke tests;
- `git diff --check`;
- artifact and secret scans.

**ACCEPT:** all required gates pass against the exact R3 source/artifacts and
the test handoff is complete.

**REJECT:** any gate uses a different build, missing identity, local auth
bypass, or post-freeze rebuild.

### 9.5-R5 — Physical device and final acceptance

R5 is executed by the waiting test agent only after R0–R4 independent ACCEPT.

Run in two bounded profiles:

1. `physical-smoke`: install, QR/local approval, identity restoration,
   managed session, Activity/Transcript/Terminal, exact-generation input,
   N1, reconnect, and safe fallback.
2. `physical-destructive` or `physical-recovery`: only with explicit user
   authorization; exact-device revoke, stale credential rejection,
   replacement pairing, epoch/identity proof, and owner recovery only if
   separately required.

The non-destructive run must not silently expand into destructive recovery.
The destructive run must not revoke all active devices or infer owner-transfer
authority.

Step 9.5 remains incomplete until the exact installed artifacts pass the
required SM-S926N matrix and a fresh independent verifier accepts the evidence.

---

## 6. Workflow and commit protocol

Use the accepted workflow:

```text
frozen R contract
→ bounded implementation
→ implementation pre-gate
→ fresh V1 contract/code verification
→ V1 ACCEPT and implementation freeze
→ deterministic evidence manifest
→ path-restricted Evidence mode when needed
→ evidence pre-gate
→ fresh V2 milestone/authority audit
```

Implementation and evidence commits remain separate. Contract documents are
frozen inputs to V1; evidence documents are produced only after V1 ACCEPT.
Trivial non-authoritative corrections need not create a permanent specialist
role.

The pre-gate must compare worker claims with Git, file contents, generated
configuration, logs, and artifact bytes. An inconsistent worker completion
claim prevents verifier dispatch and creates a normalized failure record.

No artifact may embed its own content-determining commit identity. Evidence
records the implementation/source candidate; the evidence commit identity is
derived externally from Git or the verdict ledger.

---

## 7. Tester resume handoff

The test agent resumes only when all fields are exact and independently
verified:

```text
EXPECTED_SOURCE_SHA=<new post-remediation production candidate>
EXPECTED_BRANCH=feature/canonical-timeline-foundation
EXPECTED_DAEMON_SHA256=<64 hex>
EXPECTED_APK_SHA256=<64 hex>
DAEMON_ARTIFACT=<absolute immutable bundle path>
APK_ARTIFACT=<absolute immutable bundle path>
ANDROID_SERIAL=R3CX106PTFD
EXPECTED_ANDROID_MODEL=SM-S926N
TEST_PROFILE=physical-smoke
CONTRACT_REF=docs/... exact accepted section
ARTIFACT_MANIFEST=<absolute path>
R3_VERDICT=<independent ACCEPT reference>
R4_VERDICT=<independent ACCEPT reference>
```

The handoff must also state:

- source SHA versus documentation/evidence HEAD;
- Formula source pin and installed daemon relationship;
- exact toolchain tuple;
- current expected owner/member state;
- allowed and forbidden device mutations;
- required human gates;
- whether and when the separate destructive profile is authorized.

Until then, the authoritative response remains:

```text
BLOCKED_ANDROID_BUILD_TOOLCHAIN
BLOCKED_ARTIFACT_HANDOFF_PENDING
```

---

## 8. Stop conditions

Stop the remediation or device gate if:

- the final source candidate is still the pre-remediation diagnostic SHA;
- a build uses the current documentation HEAD as a substitute source identity;
- clean prebuild loses an accepted native setting;
- build success depends on a dirty or ignored developer tree;
- `node_modules`, Gradle cache, or generated output is patched without a
  reviewed source-controlled fix;
- daemon VCS metadata is absent, dirty, or mismatched;
- daemon/APK embedded identities differ;
- a Formula-installed daemon and frozen daemon are claimed equal without byte
  comparison;
- an APK is rebuilt after the tester receives its handoff;
- pairing/auth/permission/Terminal/Timeline/N1 authority is weakened;
- a physical or destructive action occurs before exact artifact handoff;
- incomplete temporary artifacts are used as evidence.

No Step 9.5 ACCEPT SHA may be set until R5 passes.
