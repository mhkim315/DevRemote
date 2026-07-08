# E6 No-login Local Test Branch Report

Date: 2026-07-08
Build flag: EXPO_PUBLIC_POKIT_NO_LOGIN_LOCAL_TEST=1

## Implementation

- **Backend**: `InsecureLocalOnly` flag on Handlers. AuthMiddleware accepts `Authorization: Bearer dev-token` when insecure mode is active.
- **Mobile**: `EXPO_PUBLIC_POKIT_NO_LOGIN_LOCAL_TEST=1` env var gates no-login mode. Skips Supabase auth entirely. Uses synthetic dev session with `access_token: 'dev-token'`.
- **Production**: Auth unchanged. `InsecureLocalOnly=false` + flag unset = full Supabase JWT flow.

## Fresh install test

| Step | Android Emulator | Notes |
|------|---------|-------|
| pm clear (fresh data) | ✅ | App data wiped |
| Launch | ✅ | ConnectScreen appears first |
| AuthScreen skipped | ✅ | No Supabase login required |
| URL entry | ✅ | `http://10.0.2.2:9171` entered |
| Connect | ✅ | daemon accepts dev-token |
| Dashboard renders | ✅ | Session cards, sections, states |

## Build gate

```sh
sh scripts/build-gate.sh
=== ALL GATES PASSED ===
```

## Supported local URLs

When `EXPO_PUBLIC_POKIT_NO_LOGIN_LOCAL_TEST=1`:
- localhost
- 127.0.0.1
- 10.0.2.2 (Android emulator)
- LAN IP (192.168.x.x)

## Remaining in M-track

- M1 Physical Device Smoke
- M2 Real Push + Notification Tap
- M3 First-time Pairing / Onboarding
- M4 Real Interaction UX
