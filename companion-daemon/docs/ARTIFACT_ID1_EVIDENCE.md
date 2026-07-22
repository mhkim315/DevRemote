# ARTIFACT-ID1 — Matched Candidate Provenance Evidence

**Status:** COMPLETE

**PB DEVICE CANDIDATE SHA:** `ab18846622327334300de8436aff600db5c08e17`

**Date:** 2026-07-22

## 1. Clone and Checkout

```
$ git clone --no-hardlinks . /tmp/pokit-pb-device-candidate
Cloning into '/tmp/pokit-pb-device-candidate'...
done.

$ cd /tmp/pokit-pb-device-candidate
$ git checkout --detach ab18846622327334300de8436aff600db5c08e17
HEAD is now at ab1884662 fix(pb): remove tracked SHA assets, use build-time gitignore'd generation

$ git status --porcelain --untracked-files=all
(empty — 0 untracked/modified files)
```

## 2. Daemon Build

```
$ cd companion-daemon
$ go build -o /tmp/pokit-pb-device-artifacts/pokit-daemon ./cmd/devremote
BUILD: OK
```

### Build Info

```
$ go version -m /tmp/pokit-pb-device-artifacts/pokit-daemon
/tmp/pokit-pb-device-artifacts/pokit-daemon: go1.26.4
	path	devremote/companion-daemon/cmd/devremote
	mod	devremote/companion-daemon	(devel)
	build	vcs.revision=ab18846622327334300de8436aff600db5c08e17
	build	vcs.time=2026-07-22T04:42:00Z
	build	vcs.modified=false
```

- `vcs.revision` matches candidate SHA: ✅
- `vcs.modified=false`: ✅

### Daemon SHA-256

```
5c1470a0d58072564298572aa8184a9c9565a8f57452eba56e2ebda56b10e580  pokit-daemon
```

## 3. APK Build

```
$ cd mobile
$ npm install
$ EXPO_PUBLIC_POKIT_NO_LOGIN_LOCAL_TEST= npx expo prebuild --platform android --no-install
$ cd android
$ EXPO_PUBLIC_POKIT_NO_LOGIN_LOCAL_TEST= ./gradlew assembleRelease
BUILD SUCCESSFUL in 3m 4s
1038 actionable tasks: 1038 executed
```

- `EXPO_PUBLIC_POKIT_NO_LOGIN_LOCAL_TEST` unset: ✅ (production build, no login bypass)

### APK Contents

| File | Size |
|------|------|
| `assets/index.android.bundle` | 3,255,192 bytes |
| `classes.dex` | 9,792,704 bytes |
| `classes2.dex` | 9,933,544 bytes |
| `classes3.dex` | 8,942,924 bytes |
| `classes4.dex` | 10,354,212 bytes |
| `classes5.dex` | 394,932 bytes |
| `AndroidManifest.xml` | 27,964 bytes |
| Debug signature | Default debug keystore |

⚠️ The APK is signed with the **default debug keystore** — this is a build-verify artifact, not a distribution build. The device tester must verify this APK installs and runs on SM-S926N before proceeding to the physical matrix.

### APK SHA-256

```
b4a09b79e35f4b54e9b210481f20d70ec722557c39c7a64e0b41d22099f70797  pokit-app-release.apk
```

## 4. Asset Files

| File | Content |
|------|---------|
| `PB_DEVICE_CANDIDATE_SHA.txt` | `ab18846622327334300de8436aff600db5c08e17` |
| `DAEMON_SHA256.txt` | `5c1470a0d58072564298572aa8184a9c9565a8f57452eba56e2ebda56b10e580` |
| `APK_SHA256.txt` | `b4a09b79e35f4b54e9b210481f20d70ec722557c39c7a64e0b41d22099f70797` |

## 5. Artifact Preservation

All artifacts preserved at:

```
/tmp/pokit-pb-device-artifacts/
├── PB_DEVICE_CANDIDATE_SHA.txt
├── DAEMON_SHA256.txt
├── APK_SHA256.txt
├── pokit-daemon          (12,349,410 bytes)
└── pokit-app-release.apk (160,875,337 bytes)
```

## 6. Provenance Chain

```
git clone (exact candidate) → go build → daemon binary
  → vcs.revision matches candidate SHA ✅
  → vcs.modified=false ✅
  → SHA-256 computed ✅

git clone (exact candidate) → npm install → expo prebuild + gradle assembleRelease → APK
  → EXPO_PUBLIC_POKIT_NO_LOGIN_LOCAL_TEST unset ✅
  → index.android.bundle present ✅
  → SHA-256 computed ✅

Daemon + APK built from SAME candidate SHA ab1884662 ✅
```

## 7. Verification Commands

To re-verify independently:

```sh
# Clone
git clone --no-hardlinks <repo> /tmp/pokit-reverify
cd /tmp/pokit-reverify
git checkout --detach ab18846622327334300de8436aff600db5c08e17

# Daemon
cd companion-daemon
go build -o /tmp/pokit-daemon ./cmd/devremote
go version -m /tmp/pokit-daemon | grep vcs.revision
# Expected: vcs.revision=ab18846622327334300de8436aff600db5c08e17
shasum -a 256 /tmp/pokit-daemon
# Expected: 5c1470a0d58072564298572aa8184a9c9565a8f57452eba56e2ebda56b10e580

# APK
cd ../mobile
npm install
EXPO_PUBLIC_POKIT_NO_LOGIN_LOCAL_TEST= npx expo prebuild --platform android --no-install
cd android
EXPO_PUBLIC_POKIT_NO_LOGIN_LOCAL_TEST= ./gradlew assembleRelease
cp app/build/outputs/apk/release/app-release.apk /tmp/pokit-apk.apk
shasum -a 256 /tmp/pokit-apk.apk
# Expected: b4a09b79e35f4b54e9b210481f20d70ec722557c39c7a64e0b41d22099f70797
```
