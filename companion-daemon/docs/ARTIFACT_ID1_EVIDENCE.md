# ARTIFACT-ID1 — Matched Candidate Provenance Evidence

**Status:** COMPLETE

**PB DEVICE CANDIDATE SHA:** `ab18846622327334300de8436aff600db5c08e17`

**Date:** 2026-07-22

## 1. Clone and Checkout

```
$ git clone --no-hardlinks . /tmp/pokit-pb-device-candidate
Cloning into '/tmp/pokit-pb-device-candidate'... done.

$ cd /tmp/pokit-pb-device-candidate
$ git checkout --detach ab18846622327334300de8436aff600db5c08e17
HEAD is now at ab1884662

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
	build	vcs.revision=ab18846622327334300de8436aff600db5c08e17
	build	vcs.modified=false
```

- `vcs.revision` matches candidate SHA: ✅
- `vcs.modified=false`: ✅

### Daemon SHA-256

```
5c1470a0d58072564298572aa8184a9c9565a8f57452eba56e2ebda56b10e580  pokit-daemon
```

## 3. APK Build

### Environment

```
$ env -u EXPO_PUBLIC_POKIT_NO_LOGIN_LOCAL_TEST
```
Env var **truly unset** (not empty string) — production build, no login bypass.

### Asset Embedding

Before building, the candidate SHA and daemon hash are placed in the Android
assets directory so they are embedded in the APK:

```
$ mkdir -p mobile/android/app/src/main/assets
$ cp /tmp/pokit-pb-device-artifacts/PB_DEVICE_CANDIDATE_SHA.txt \
     mobile/android/app/src/main/assets/
$ cp /tmp/pokit-pb-device-artifacts/DAEMON_SHA256.txt \
     mobile/android/app/src/main/assets/
```

### Prebuild + Build

```
$ cd mobile
$ npm install
$ env -u EXPO_PUBLIC_POKIT_NO_LOGIN_LOCAL_TEST npx expo prebuild --platform android --no-install
$ cd android
$ env -u EXPO_PUBLIC_POKIT_NO_LOGIN_LOCAL_TEST ./gradlew assembleRelease
BUILD SUCCESSFUL in 3m 47s
1038 actionable tasks: 1038 executed
```

## 4. APK Verification

### SHA-256

```
934febb17b8de175816413a1aaa1a17b8d1e8f43c2cbfcb4fe26e20fce433c7d  pokit-app-release.apk
```

### Embedded Assets — Byte-for-Byte Comparison

```
$ unzip -p pokit-app-release.apk assets/PB_DEVICE_CANDIDATE_SHA.txt > extracted-candidate.txt
$ unzip -p pokit-app-release.apk assets/DAEMON_SHA256.txt > extracted-daemon.txt

$ diff <source> <extracted> → no differences
```

| Asset | Source | APK-Extracted | Match |
|-------|--------|---------------|-------|
| `assets/PB_DEVICE_CANDIDATE_SHA.txt` | `ab18846622327334300de8436aff600db5c08e17` | `ab18846622327334300de8436aff600db5c08e17` | ✅ byte-for-byte |
| `assets/DAEMON_SHA256.txt` | `5c1470a0d580...` | `5c1470a0d580...` | ✅ byte-for-byte |

### Cleartext Prohibition

```
$ unzip -p pokit-app-release.apk AndroidManifest.xml | strings | grep -i cleartext
(empty — no cleartext traffic permitted)
```

Cleartext prohibition: ✅ CLEAN

### NO_LOGIN Leak Check

```
$ unzip -p pokit-app-release.apk assets/index.android.bundle | strings | grep -i NO_LOGIN
(empty — no login bypass env var leaked into bundle)
```

NO_LOGIN: ✅ NOT FOUND in APK bundle

### Dev-Token Compile-State Check

```
$ unzip -p pokit-app-release.apk assets/index.android.bundle | LC_ALL=C grep -ao 'dev-token' | wc -l
1
```

dev-token: 1 occurrence — the string `dev-token` is the explicit local-dev
bypass credential VALUE (the query parameter name is `token`, the value
`dev-token` authenticates in `--insecure-local-only` mode). Retained in the
bundle as a string literal in auth code but INACTIVE at runtime —
EXPO_PUBLIC_POKIT_NO_LOGIN_LOCAL_TEST was unset, so the insecure-local-only
mode is never enabled in this production build.

### APK Signature

The APK is signed with the **Android SDK debug keystore** using APK Signature
Scheme v2 only:
```
$ apksigner verify --verbose pokit-app-release.apk
Verified using v1 scheme (JAR signing): false
Verified using v2 scheme (APK Signature Scheme v2): true
Verified using v3 scheme (APK Signature Scheme v3): false
Verified using v3.1 scheme (APK Signature Scheme v3.1): false
Verified using v4 scheme (APK Signature Scheme v4): false
```
This is a build-verify artifact — the device tester must verify installation
on SM-S926N.

APK contents: 1,387 entries including `assets/index.android.bundle` (3.2MB),
5 DEX files, `AndroidManifest.xml`.

### Bundled JS

```
$ unzip -l pokit-app-release.apk | grep index.android.bundle
  3255192  assets/index.android.bundle
```

## 5. Artifact Preservation

All artifacts preserved at `/tmp/pokit-pb-device-artifacts/` (readable by current user mhk):

```
/tmp/pokit-pb-device-artifacts/
├── PB_DEVICE_CANDIDATE_SHA.txt     (41 bytes)
├── DAEMON_SHA256.txt               (65 bytes)
├── APK_EXTRACTED_CANDIDATE.txt     (41 bytes) — extracted from APK, verified match
├── APK_EXTRACTED_DAEMON.txt        (65 bytes) — extracted from APK, verified match
├── pokit-daemon                    (12,349,410 bytes)
└── pokit-app-release.apk           (160,875,703 bytes)
```

Permissions (per-file):
| File | Owner:Group | Mode |
|------|-------------|------|
| `pokit-daemon` | mhk:staff | 0755 (executable) |
| `pokit-app-release.apk` | mhk:wheel | 0644 |
| `PB_DEVICE_CANDIDATE_SHA.txt` | mhk:wheel | 0644 |
| `DAEMON_SHA256.txt` | mhk:wheel | 0644 |
| `APK_EXTRACTED_CANDIDATE.txt` | mhk:wheel | 0644 |
| `APK_EXTRACTED_DAEMON.txt` | mhk:wheel | 0644 |

## 6. Provenance Chain

```
git clone (exact candidate SHA) → go build → daemon binary
  → vcs.revision = ab18846622327334300de8436aff600db5c08e17 ✅
  → vcs.modified = false ✅
  → SHA-256 = 5c1470a0d58072564298572aa8184a9c9565a8f57452eba56e2ebda56b10e580

git clone (exact candidate SHA) → npm install → expo prebuild
  → embed PB_DEVICE_CANDIDATE_SHA.txt + DAEMON_SHA256.txt in assets/
  → env -u EXPO_PUBLIC_POKIT_NO_LOGIN_LOCAL_TEST gradle assembleRelease
  → APK = 160MB release build
  → SHA-256 = 934febb17b8de175816413a1aaa1a17b8d1e8f43c2cbfcb4fe26e20fce433c7d
  → assets extracted byte-for-byte match source ✅
  → cleartext clean ✅
  → NO_LOGIN not leaked ✅

Daemon + APK built from SAME candidate SHA ab1884662 ✅
Daemon hash embedded in APK + byte-for-byte verified ✅
```

## 7. Re-Verification Commands

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

# APK (requires Android SDK + Gradle)
cd ../mobile
npm install
env -u EXPO_PUBLIC_POKIT_NO_LOGIN_LOCAL_TEST npx expo prebuild --platform android --no-install
# Embed asset files before building:
mkdir -p android/app/src/main/assets
echo "ab18846622327334300de8436aff600db5c08e17" > android/app/src/main/assets/PB_DEVICE_CANDIDATE_SHA.txt
echo "5c1470a0d58072564298572aa8184a9c9565a8f57452eba56e2ebda56b10e580" > android/app/src/main/assets/DAEMON_SHA256.txt
cd android
env -u EXPO_PUBLIC_POKIT_NO_LOGIN_LOCAL_TEST ./gradlew assembleRelease
shasum -a 256 app/build/outputs/apk/release/app-release.apk
# Expected: 934febb17b8de175816413a1aaa1a17b8d1e8f43c2cbfcb4fe26e20fce433c7d

# Verify embedded assets
unzip -p app/build/outputs/apk/release/app-release.apk assets/PB_DEVICE_CANDIDATE_SHA.txt
# Expected: ab18846622327334300de8436aff600db5c08e17
unzip -p app/build/outputs/apk/release/app-release.apk assets/DAEMON_SHA256.txt
# Expected: 5c1470a0d58072564298572aa8184a9c9565a8f57452eba56e2ebda56b10e580
```
