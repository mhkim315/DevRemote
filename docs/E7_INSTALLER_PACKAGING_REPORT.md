# E7 Installer / Packaging Report

Date: 2026-07-08

## Verification results

| Check | Result |
|-------|--------|
| sh -n install.sh | PASS |
| sh -n uninstall.sh | PASS |
| sh -n package.sh | PASS |
| sh scripts/install.sh | PASS |
| sh scripts/install.sh (reinstall) | PASS (idempotent) |
| sh scripts/uninstall.sh | PASS |
| sh scripts/uninstall.sh (repeat) | PASS (idempotent) |
| sh scripts/package.sh | PASS (10M binary + metadata) |
| sh scripts/build-gate.sh | PASS (8/8) |

## Scripts

### install.sh
- Local: `sh scripts/install.sh` → `~/.local/bin/devremote` with version metadata
- Global: `sudo sh scripts/install.sh --global` → `/usr/local/bin/devremote`
- Permission check before build for --global
- `mkdir -p` for target directory
- Version from git describe

### uninstall.sh
- Local: `sh scripts/uninstall.sh` → removes `~/.local/bin/devremote`
- Global: `sudo sh scripts/uninstall.sh --global`
- Idempotent: succeeds even if already removed
- No side effects on unrelated files

### package.sh
- `sh scripts/package.sh` → `dist/pokit-daemon-<version>-<os>-<arch>`
- Metadata: `dist/pokit-daemon-<version>-<os>-<arch>.meta`
- Version from git describe, OS/arch from go env
- Dry-run only (no publish)

## Artifact

```
dist/pokit-daemon-0cb107c9a-dirty-darwin-arm64      10M
dist/pokit-daemon-0cb107c9a-dirty-darwin-arm64.meta 128B
```

## Remaining in M-track (not E-track)

- M1 Physical Device Smoke
- M2 Real Push + Notification Tap
- M3 First-time Pairing / Onboarding
- M4 Real Interaction UX

## Known gaps

- Global install/uninstall not tested (requires sudo, not available in this environment)
- Version is git-based; tagged releases would provide cleaner versions
- Mobile packaging (Expo EAS build) requires project owner credentials — not E-track
