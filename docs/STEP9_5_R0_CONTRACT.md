# Step 9.5-R0 Build Reproducibility Contract

**Status:** R0 Contract — Implementation Not Started

**Authority:** mandatory prerequisite to Step 9.5 Base Alpha artifact freeze

**Source plan:** [`STEP9_5_BUILD_REPRODUCIBILITY_REMEDIATION_PLAN.md`](STEP9_5_BUILD_REPRODUCIBILITY_REMEDIATION_PLAN.md)

---

## 1. Supported toolchain tuple

The build entrypoint must validate each component. Any mismatch fails closed.

| Component | Supported Version | Source of Truth | Validation |
| --- | --- | --- | --- |
| macOS | 26.x (arm64) | `sw_vers -productVersion`, `uname -m` | `[[ "$(uname -m)" == "arm64" ]]` |
| Go | 1.26.x | `go version` | `go version | grep -q "go1.26"` |
| Node | 20.x | `mobile/.nvmrc` | `node --version` starts with `v20.` |
| npm | 10.x | `mobile/package.json` engines field | `npm --version` starts with `10.` |
| JDK | 21 (Temurin) | Gradle toolchain spec | `java -version 2>&1 | grep -q "21."` |
| Android SDK | 36 (build-tools 36.x) | `mobile/android/build.gradle` | `sdkmanager --list` |
| Expo CLI | ~56.0.0 | `mobile/package.json` | `npx expo --version` |
| React Native | 0.85.3 | `mobile/package.json` | lockfile |
| Gradle | **8.11.1** (downgrade from 9.3.1) | `mobile/android/gradle/wrapper/gradle-wrapper.properties` | wrapper checksum |
| foojay-resolver | **removed** (not compatible with RN 0.85.3 + Gradle 9.x) | `mobile/android/settings.gradle` | assert absent |

### 1.1 Gradle version decision (R0 freezes this choice)

**Options evaluated:**

| Option | Compatibility | Risk |
| --- | --- | --- |
| A: Downgrade to Gradle 8.11.1 | RN 0.85.3 plugin matrix supports 8.x | Must verify wrapper checksum; well-trodden path |
| B: Override foojay-resolver for Gradle 9.3.1 | Unknown RN 0.85.3 + Gradle 9.x compatibility surface | RN Gradle plugin may have additional 9.x issues beyond the resolver |

**Decision: Option A — Gradle 8.11.1.** The RN 0.85.x Gradle plugin is
tested against Gradle 8.x. Keeping 9.3.1 requires validating every RN Gradle
plugin interaction point. Downgrading the wrapper is one source-controlled
file change.

### 1.2 foojay-resolver incompatibility

**Observed:** Gradle 9.3.1 + `org.gradle.toolchains.foojay-resolver-convention`
0.5.0 does not recognize `IBM_SEMERU`. The RN Gradle plugin applies this
resolver by default. Clean build fails.

**R0 resolution:** Remove foojay-resolver reference from
`mobile/android/settings.gradle`. The Gradle toolchain specification
selects JDK 21 Temurin explicitly without a resolver. This is a
source-controlled change to one tracked file.

### 1.3 JVM vendor selection

- Explicit JDK vendor: **Eclipse Temurin 21**
- Gradle toolchain: `jvmToolchain { languageVersion = 21; vendor = JvmVendorSpec.ADOPTIUM }` (Adoptium = Temurin in Gradle vocabulary)
- Fail-closed on missing or unsupported JDK
- No reliance on `JAVA_HOME` alone

### 1.4 macOS validation

The build entrypoint must validate:
```bash
[[ "$(uname -s)" == "Darwin" ]]
[[ "$(uname -m)" == "arm64" ]]
[[ "$(sw_vers -productVersion)" == 26.* ]]
```

### 1.5 Source-controlled version pins

| File | Purpose |
| --- | --- |
| `mobile/.nvmrc` | Node version (`20`) |
| `mobile/package.json` `engines` | Node `>=20.0.0 <21.0.0`, npm `>=10.0.0 <11.0.0` |
| `mobile/package-lock.json` | Exact dependency tree (lockfileVersion 3) |
| `mobile/android/gradle/wrapper/gradle-wrapper.properties` | Gradle wrapper version + checksum |

---

## 2. Clean native-generation ownership

### 2.1 Problem

`npx expo prebuild --platform android --no-install` clears and regenerates
`mobile/android/`. The regenerated `AndroidManifest.xml` removes
`android:usesCleartextTraffic="true"` from the `<application>` element. This
setting is required by the bounded Pairing V1 LAN HTTP `/pair` transport.

### 2.2 Classification

Expo prebuild does not preserve custom manifest attributes that are not
expressed through `app.json` Expo config. The `usesCleartextTraffic` setting
is manually added to the tracked manifest but is not declared in Expo's
config schema.

### 2.3 R1 resolution

Must provide ONE source-controlled mechanism to preserve
`android:usesCleartextTraffic="true"` through clean prebuild:

- **(Preferred)** A narrow Expo config plugin in `mobile/plugins/` that
  injects the attribute into the generated manifest.
- **(Alternative)** Declare the setting through `app.json` Expo config if
  a supported key exists in Expo SDK 56.
- **(Prohibited)** Manually restoring the manifest after prebuild, copying
  a prior generated Android tree, or committing the entire generated
  `mobile/android/` directory.

### 2.4 Required generated-manifest assertions

After clean prebuild, the following must be preserved:

| Setting | Required Value | Authority |
| --- | --- | --- |
| `android:usesCleartextTraffic` | `true` | Pairing V1 LAN transport |
| `android:allowBackup` | `true` | Keystore backup |
| `android:fullBackupContent` | `@xml/secure_store_backup_rules` | Keystore |
| `android:dataExtractionRules` | `@xml/secure_store_data_extraction_rules` | Keystore |
| Package name | `com.pokit.devremote` | Application identity |
| Deep link scheme | `pokit` | Activity routing |
| Camera permission | `android.permission.CAMERA` | QR pairing |
| Notifications | Firebase/messaging | N1 push |

---

## 3. Source-controlled Pairing V1 manifest generation

### 3.1 Requirement

The regenerated Android manifest must include the Pairing V1 transport
requirement through an authoritative source-controlled mechanism. The
mechanism must be reviewable in the same repository and must not depend on
post-generation manual editing.

### 3.2 Implementation boundary (R1 allowed files)

R1 may modify only:

| Path | Purpose |
| --- | --- |
| `mobile/plugins/*` (new) | Expo config plugin for manifest injection |
| `mobile/app.json` | Declarative Expo config |
| `mobile/android/gradle/wrapper/gradle-wrapper.properties` | Gradle 8.11.1 pin |
| `mobile/android/settings.gradle` | Remove foojay-resolver |
| `mobile/android/build.gradle` | JDK toolchain spec |
| `mobile/package.json` | (no changes beyond R0 engines field) |
| `mobile/package-lock.json` | (generated, no manual edits) |
| `scripts/build-artifacts.sh` (new) | Build entrypoint |
| `docs/STEP9_5_R1_*.md` (new) | R1 evidence |

The generated `mobile/android/` tree remains in `.gitignore` except for the
explicitly tracked overlay files already present.

R1 must not modify: daemon source, pairing protocol, auth, permissions,
Timeline, Transcript, Terminal, or N1.

---

## 4. Fail-closed clean-clone build state machine

### 4.1 Entry point

One documented command that starts from a clean clone of an exact commit
and either produces a complete matched bundle or fails closed.

```text
BUILD_ENTRYPOINT=scripts/build-artifacts.sh
EXPECTED_SOURCE_SHA=<production candidate>
```

### 4.2 State machine

```
[Clean Clone] → [Validate Toolchain] → [Install Dependencies]
  → [Prebuild Android] → [Validate Generated Manifest]
  → [Build Daemon] → [Validate Daemon Identity]
  → [Build APK] → [Validate APK Identity]
  → [Freeze Artifacts] → [Done]
```

Each stage fails closed. Any stage failure stops the build and must not
produce a partial artifact that could be mistaken for a complete bundle.

### 4.3 Daemon build

- `-trimpath` required
- `vcs.revision` must equal `EXPECTED_SOURCE_SHA`
- `vcs.modified` must be `false`
- Build timestamp must be the deterministic source commit timestamp

### 4.4 Linked-worktree VCS metadata — reclassified

**Observed:** A linked Git worktree (`git worktree add`) did not emit the
required Go VCS metadata (`vcs.revision`, `vcs.modified`).

**Reclassification:** This is NOT a Go build system limitation. Go
`-buildvcs=true` (default since 1.18) works correctly from linked
worktrees when the main repository is accessible and the worktree is
clean. The observed failure was caused by one of:

- Build invocation from a directory that is not a Go module root
  (`go build` must run inside the module, not above it)
- Build from a worktree whose main repository `.git` directory was
  not reachable (e.g., the main checkout was deleted)
- Stale or incomplete Go build cache

**R0 decision:** Production artifact builds must use a **clean real clone**
(`git clone` of the exact source candidate). This is the simplest,
most reproducible path. Linked worktrees are not authorized for production
builds because the VCS metadata path depends on filesystem state outside
the worktree.

The build entrypoint must validate that `git rev-parse --is-inside-work-tree`
succeeds AND that `.git` is a directory (not a file reference):

```bash
test -d .git || { echo "FATAL: not a real clone (.git is not a directory)"; exit 1; }
```

### 4.5 Daemon identity validation

```bash
DAEMON_VCS=$(go version -m pokit-daemon | grep -E '^\tbuild\tvcs.revision=')
DAEMON_MODIFIED=$(go version -m pokit-daemon | grep -E '^\tbuild\tvcs.modified=true')
test -n "$DAEMON_VCS" || { echo "FATAL: missing vcs.revision"; exit 1; }
test -z "$DAEMON_MODIFIED" || { echo "FATAL: vcs.modified=true"; exit 1; }
```

---

## 5. Formula/frozen-daemon identity policy

### 5.1 Problem

The Homebrew formula installs via `go build` from a tarball (not a Git
clone). A formula-built daemon will have different Go build provenance
(no VCS metadata) than a frozen daemon built from a clean clone.

### 5.2 Policy

- The **frozen daemon** is the authoritative artifact for Step 9.5.
  It must be built from a clean real clone with full VCS metadata.
- The **formula daemon** is a convenience installation path. It embeds
  the source SHA through `-ldflags` (`cliGitSHA`). Go VCS metadata is
  absent from tarball builds — this is expected and documented.
- Formula and frozen daemon identity must be independently measured.
  They are NOT required to be byte-identical; the difference is
  attributable to Go build provenance, not source divergence.
- The formula pin must point to the exact production source candidate.

---

## 6. R1 scope

R1 implements the minimal changes required to produce a clean Android
prebuild that preserves all accepted native settings:

1. Expo config plugin for `usesCleartextTraffic`
2. Gradle wrapper downgrade to 8.11.1 + remove foojay-resolver
3. JDK 21 Temurin toolchain specification
4. Generated manifest assertion test
5. Build entrypoint script (`scripts/build-artifacts.sh`) with toolchain
   validation, daemon VCS identity check, and artifact freeze

R1 allowed files: see Section 3.2. No other files may be modified.

R1 must not change any production/mobile behavior, dependency versions
(beyond the Gradle wrapper downgrade), or accepted authority.

---

## 7. Rollback procedure

Any R1 change that breaks clean prebuild, release compilation, or
generated manifest settings must be rolled back:

```text
git checkout <last-good-sha>
rm -rf mobile/android mobile/node_modules
npm ci --prefix mobile
npx expo prebuild --platform android --no-install
# Verify manifest settings against Section 2.4
```

Rollback is complete when the pre-R1 build failure is reproduced exactly.

---

## 8. ACCEPT/REJECT gates

### ACCEPT

An independent reviewer confirms:
1. Supported toolchain tuple is documented and compatible (Gradle 8.11.1
   selected; foojay-resolver removed; JDK 21 Temurin specified)
2. Clean prebuild failure is reproducible from the diagnostic SHA
3. Linked-worktree VCS issue is reclassified as build-invocation, not
   Go limitation; clean-clone-only policy is frozen
4. Generated-native ownership decision is sound
5. Pairing V1 manifest requirement is explicit and testable
6. Formula identity policy distinguishes convenience from authority
7. Clean-clone state machine is complete with `.git` directory check
8. No implementation is started before R0 ACCEPT
9. Source-controlled version pins are present (`.nvmrc`, `engines`,
   macOS validation)
10. Sections 1, 3.2, and 6 agree on toolchain tuple and allowed files

### REJECT

The contract is rejected if it:
- Relies on a developer-local generated directory
- Permits manual manifest restoration
- Uses mutable global packages without version pinning
- Proposes Gradle 9.3.1 without compatibility evidence
- Authorizes linked-worktree builds for production artifacts
- Starts R1 implementation before R0 acceptance

---

## 9. Allowed files (R0)

Only these files may be created or modified during R0:

| File | Action | Purpose |
| --- | --- | --- |
| `docs/STEP9_5_R0_CONTRACT.md` | Create | This contract |
| `mobile/.nvmrc` | Create | Node version pin (20) |
| `mobile/package.json` | Modify | `engines` field (Node 20.x, npm 10.x) |

No daemon, mobile behavior, Gradle, Expo, or config changes.

---

## 10. Test agent status

The device-test agent (`SM-S926N`, `R3CX106PTFD`) remains `BLOCKED` until
R0–R4 independent ACCEPT. No build, install, pair, or recovery action is
authorized.

```text
BLOCKED_ANDROID_BUILD_TOOLCHAIN
BLOCKED_ARTIFACT_HANDOFF_PENDING
```
