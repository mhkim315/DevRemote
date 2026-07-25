# DS-DEV7 — SM-S926N Physical Device Test Report

**Date:** 2026-07-25
**HEAD:** 3ded6f6f (frozen)
**Device:** SM-S926N / Android 16 / BP4A.251205.006.S926NKSSGDZG1
**macOS:** 26.5.1 arm64

## Artifact Identity

| Artifact | SHA256 |
|----------|--------|
| Source | 3ded6f6ff183bafae7b42303590d8bf984230c8c |
| Daemon | b6c0a36e0ea1d92909a98aa72d1b21590bcb0713015b7c7d354a2760bfc0cb65 |
| APK | a875538d0b45743c6b44b2b964398efb422de52b1f9a421c36e224d654b77f45 |
| Daemon vcs.revision | 3ded6f6f |
| Daemon vcs.modified | false |
| Go | 1.26.5 |
| APK versionCode | 1 |

## Gate Results

| Gate | Result |
|------|--------|
| go build | ✅ |
| go vet | ✅ |
| go test -race | ✅ 20/20 |
| vcs.modified | ✅ false |
| DAEMON MATCH | ✅ |
| APK MATCH | ✅ |

## Test Results

### 0. USB + APK Install
| Step | Action | Result |
|------|--------|--------|
| 0a | USB connect | ✅ R3CX106PTFD |
| 0b | APK install | ✅ Success |
| 0c | versionCode | ✅ 1 |

### 1. QR Pairing
| Step | Action | Expected | Result |
|------|--------|----------|--------|
| 1a | `pokit pair` | QR code displayed | ✅ PNG auto-open |
| 1b | App scan QR | Camera opens | ✅ |
| 1c | BootstrapToken | Verification passes | ✅ |
| 1d | Keystore proof | Signatures match | ✅ |
| 1e | Owner screen | No camera stuck | ✅ |
| 1f | `pokit devices` | Paired device shown | ✅ |

### 2. Shell Session (controlled_pty)
| Step | Action | Expected | Result |
|------|--------|----------|--------|
| 2a | New Session → Shell | Session created | ✅ |
| 2b | First view | Terminal tab | ✅ |
| 2c | `echo hello` | Output normal | ✅ |
| 2d | Transcript tab | Content shown | ✅ |
| 2e | CLI attach same session | Multi-viewer | ✅ |

### 3. Codex Session (TUI host)
| Step | Action | Expected | Result |
|------|--------|----------|--------|
| 3a | New Session → Codex | Session created | ✅ |
| 3b | First view | TUI displayed | ✅ |
| 3c | Prompt input | Response generated | ⚠️ No response (auth?) |
| 3d | Transcript tab | Events shown | ⚠️ Static content |
| 3e | Push notification | Notification received | ⏭️ Not tested |
| 3f | Approval tap | Approve/deny possible | ⏭️ Not tested |

### 4. Claude Session
| Step | Action | Expected | Result |
|------|--------|----------|--------|
| 4a | `pokit run claude` | Session created | ✅ |
| 4b | App session tap | Claude TUI | ✅ |
| 4c | Transcript tab | Evidence band | ⚠️ Empty/frozen |
| 4d | Suppressed state | Gray banner | ✅ |
| 4e | Push notification | Approval request | ⏭️ Not tested |
| 4f | Observer-only | Cannot mutate | ⏭️ Not tested |

### 5. Input Ownership
| Step | Action | Expected | Result |
|------|--------|----------|--------|
| 5a | App open → input | Input enabled | ✅ |
| 5b | CLI same session | Read-only | ✅ "view only" |
| 5c | App ReleaseInput | Ownership release | ⏭️ Not tested |
| 5d | CLI ClaimInput | Ownership transfer | ⏭️ Not tested |
| 5e | 30s auto-expire | Expiry works | ⏭️ Not tested |

### 6. Interrupt
| Step | Action | Expected | Result |
|------|--------|----------|--------|
| 6a-c | sleep + Interrupt | Process interrupted | ⏭️ Skipped |

### 7. Restart + Reconnect
| Step | Action | Expected | Result |
|------|--------|----------|--------|
| 7a | `kill daemon` | Daemon stopped | ✅ |
| 7b | Restart daemon | Daemon back up | ✅ |
| 7c | App reconnect | Sessions restored | ✅ (reconnect, sessions cleared) |
| 7d | Push registration | Restored | ⏭️ Not tested |

### 8. Revoke + Re-pair
| Step | Action | Expected | Result |
|------|--------|----------|--------|
| 8a | Revoke device | Registry removed | ✅ Access denied |
| 8b | Old session access | Stale/denied | ✅ |
| 8c | Push post-revoke | already_resolved | ⏭️ Not tested |
| 8d | New QR re-pair | New device identity | ✅ f3176dd7 ≠ 338e7e2a |
| 8e | Old bearer invalid | Rejected | ⏭️ Not tested |

### 9. Cloudflared + PATH
| Step | Action | Expected | Result |
|------|--------|----------|--------|
| 9a | `ps aux` | cloudflared exists | ✅ 1 instance |
| 9b | Restart 3x | Still 1 instance | ⚠️ 2 with nohup (1 with LaunchAgent) |
| 9c | LaunchAgent PATH | homebrew included | ⏭️ Not checked |

## Summary

```
PASS:    24/43
SKIP:    ~10 (push, interrupt, bearer)
PARTIAL: ~9  (Codex response, Claude transcript, input expiry, nohup dedup)
```

## Bugs Found (10)

| # | Bug | Severity | Status |
|---|-----|----------|--------|
| B7 | IPC socket disappears on terminal close | High | Workaround: nohup |
| B8 | Dual input paths (xterm.js + POKIT TextInput) | Medium | Known #8 |
| UX1 | Input field ~1cm gap above keyboard | Low | UI |
| UX2 | Status bar wastes vertical space | Low | UI |
| UX3 | "Delivered/Sent to socket" user-unfriendly | Low | UI |
| UX4 | Transcript forced line-wrap (need swipe) | Medium | UI |
| UX5 | Transcript frozen (Codex static, Claude empty) | Medium | Known #5/#10 |
| UX6 | QR y/N prompt not visible | Low | UI (future: 6-digit) |
| B9 | Send button: bash OK, agents broken | Medium | submitLine framing |
| B10 | Claude app create: EISDIR settings error | High | Hook settings path |
| QW12b | nohup cloudflared dedup partial | Low | LaunchAgent only |

## Evidence

```
~/Desktop/dev7-results.txt
~/Desktop/pokit-device-runs/ (screenshots + logs)
```
