# Step 9.5-R0 Build Reproducibility Contract

**Status:** R0 Contract — Implementation Not Started

**Authority:** mandatory prerequisite to Step 9.5 Base Alpha artifact freeze

**Source plan:** [`STEP9_5_BUILD_REPRODUCIBILITY_REMEDIATION_PLAN.md`](STEP9_5_BUILD_REPRODUCIBILITY_REMEDIATION_PLAN.md)

---

## 1. Supported toolchain tuple

The build entrypoint must validate each component. Any mismatch fails closed.

| Component | Supported Version | Source of Truth |
| --- | --- | --- |
| macOS | 15.x (arm64) | `uname -m` = arm64 |
| Go | 1.26.x | `go version` |
| Node | 22.x | `mobile/.nvmrc` or `package.json engines` |
| npm | 10.x | lockfile version |
| JDK | 21 (Temurin) | Gradle toolchain spec |
| Android SDK | 36 (build-tools 36.x) | `mobile/android/build.gradle` |
| Expo CLI | ~56.0.0 | `mobile/package.json` |
| React Native | 0.85.3 | `mobile/package.json` |
| Gradle | 9.3.1 | `mobile/android/gradle/wrapper/gradle-wrapper.properties` |
| foojay-resolver | (remove) | RN Gradle plugin default |

### 1.1 foojay-resolver incompatibility

**Observed:** Gradle 9.3.1 + `org.gradle.toolchains.foojay-resolver-convention` 0.5.0
does not recognize `IBM_SEMERU`. The RN Gradle plugin applies this resolver by
default. Clean build fails with `JvmVendorSpec does not have member field
'org.gradle.jvm.toolchain.JvmVendorSpec IBM_SEMERU'`.

**Classification:** Upstream RN Gradle plugin incompatibility with Gradle 9.3.1.

**R1 resolution:** Either:
- Pin a compatible Gradle wrapper version (downgrade to 8.x if RN plugin
  compatibility matrix requires it), OR
- Override the toolchain resolver to use a vendor Gradle 9.3.1 recognizes.
  Editing `node_modules` directly is prohibited; fix must be source-controlled
  via `mobile/android/settings.gradle` or a config plugin.

### 1.2 JVM vendor selection

The build must specify an explicit JVM vendor. The diagnostic used host JDK
21.0.10 (Temurin). The contract requires:
- Explicit JDK vendor in Gradle toolchain specification
- Fail-closed on missing or unsupported JDK
- No reliance on `JAVA_HOME` alone (Gradle toolchain auto-detection may
  override it)

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

### 3.2 Implementation boundary

Only `mobile/plugins/*` (new Expo config plugin), `mobile/app.json`, and
existing `.xml` files may be modified. The generated `mobile/android/`
tree remains in `.gitignore` except for the explicitly tracked overlay
files.

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

- Must use a clean real clone or linked worktree with proven VCS metadata
- `-trimpath` required
- `vcs.revision` must equal `EXPECTED_SOURCE_SHA`
- `vcs.modified` must be `false`
- Build timestamp must be the deterministic source commit timestamp, not
  the build machine's current clock

### 4.4 Linked-worktree VCS metadata

**Observed:** A linked Git worktree (`git worktree add`) did not emit the
required Go VCS metadata (`vcs.revision`, `vcs.modified`).

**Classification:** Go build system limitation with Git worktrees. The
`.git` file in a linked worktree is a reference to the main repository,
not a real `.git` directory. Go's VCS stamping requires a real `.git`
directory or `.git` file pointing to a valid repository.

**R1 resolution:**
- Require a clean real clone for production artifact builds, OR
- Prove equivalent VCS metadata in the supported checkout form (e.g.,
  `go build -buildvcs=true` with `GIT_DIR` or equivalent), OR
- Embed the expected SHA via `-ldflags` as a cross-check against the
  Go VCS stamp

The build must never silently accept a daemon missing `vcs.revision` or
with `vcs.modified=true`.

---

## 5. Formula/frozen-daemon identity policy

### 5.1 Problem

The Homebrew formula installs via `go build` from a tarball (not a Git
clone). A formula-built daemon will have different Go build provenance
(no VCS metadata) than a frozen daemon built from a clean clone.

### 5.2 Classification

Formula installation and frozen artifact production are distinct build
paths. They may produce different bytes even from the same source.

### 5.3 Policy

- The **frozen daemon** is the authoritative artifact for Step 9.5.
  It must be built from a clean real clone with full VCS metadata.
- The **formula daemon** is a convenience installation path. It must
  embed the same source SHA through `-ldflags` even if Go VCS metadata
  is absent.
- Formula and frozen daemon identity must be independently measured and
  documented. They must never be claimed equal without byte comparison.
- The formula pin must point to the exact production source candidate
  (not documentation HEAD).

---

## 6. R1 scope

R1 implements the minimal changes required to produce a clean Android
prebuild that preserves all accepted native settings:

1. Expo config plugin (or equivalent) for `usesCleartextTraffic`
2. Gradle toolchain fix (pinned Gradle version or resolver override)
3. Generated manifest assertion test
4. Build entrypoint script with toolchain validation

R1 must not change any production/mobile behavior, dependency versions,
or accepted authority.

---

## 7. Rollback procedure

Any R1 change that breaks clean prebuild, release compilation, or
generated manifest settings must be rolled back to the last known-good
state:

```text
git checkout <last-good-sha>
rm -rf mobile/android mobile/node_modules
npm ci --prefix mobile
npx expo prebuild --platform android --no-install --prefix mobile
# Verify manifest settings manually
```

Rollback is complete when the pre-R1 build failure is reproduced exactly.

---

## 8. ACCEPT/REJECT gates

### ACCEPT

An independent reviewer confirms:
1. Supported toolchain tuple is documented and compatible
2. Clean prebuild failure is reproducible from the diagnostic SHA
3. foojay-resolver incompatibility is correctly classified
4. Generated-native ownership decision is sound
5. Pairing V1 manifest requirement is explicit and testable
6. Formula identity policy distinguishes convenience from authority
7. Clean-clone state machine is complete
8. No implementation is started before R0 ACCEPT

### REJECT

The contract is rejected if it:
- Relies on a developer-local generated directory
- Permits manual manifest restoration
- Uses mutable global packages without version pinning
- Proposes an unexplained Gradle downgrade without compatibility evidence
- Starts R1 implementation before R0 acceptance

---

## 9. Allowed files (R0)

Only these files may be created or modified during R0:

- `docs/STEP9_5_R0_CONTRACT.md` (this document)
- No production, mobile, daemon, formula, or config changes

---

## 10. Test agent status

The device-test agent (`SM-S926N`, `R3CX106PTFD`) remains `BLOCKED` until
R0–R4 independent ACCEPT. No build, install, pair, or recovery action is
authorized.

```text
BLOCKED_ANDROID_BUILD_TOOLCHAIN
BLOCKED_ARTIFACT_HANDOFF_PENDING
```
