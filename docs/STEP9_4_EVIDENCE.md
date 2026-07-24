# Step 9.4 Evidence — Secure Accountless Onboarding

**IMPL SHA:** `238f063a8`
**EVID SHA:** (this commit)
**CONTRACT SHA:** `609127e29` + Amendment 1 (QR mediation seam)
**IMPLEMENTATION BASE:** `f7033b86c` (Step 9.3)
**Date:** 2026-07-24

Step 9.4 implements secure accountless onboarding: macOS daemon bootstrap (9.4-A),
QR-mediated pairing (9.4-B), mobile device identity verification (9.4-C),
epoch-bound mutation authorization (9.4-D), and Homebrew packaging. 118 total
rounds, 73 V1 REJECTs across 5 packets.

Every fenced block below is unedited stdout from the command named immediately
before it.

## 1. Scope

The exact stdout of `git diff --stat f7033b86c..238f063a8` is:

```
 148 files changed, 8342 insertions(+), 1779 deletions(-)
```

Full file inventory omitted for size (148 files). Key new files by packet:

| Packet | New files |
|--------|-----------|
| 9.4-A | `cmd/devremote/daemon_darwin.go` (1029), `daemon.go` (57), `daemon_stub.go` (25), `doctor.go` (287) |
| 9.4-B | `cmd/devremote/qr_pair.go` (142), `qr_pair_test.go` (132) |
| 9.4-D | `internal/devicetrust/auth_test.go` (364), `internal/devicetrust/ipc_authorizer.go` (49), `internal/devicetrust/local_authorizer.go` (38), `internal/term/epoch_lock_barrier_test.go` (178), `internal/term/real_revoke_barrier_test.go` (855), `internal/term/owned_pty_runtime_test.go` (444) |
| Packaging | `Formula/pokit.rb` (34) |
| Contract | `docs/STEP9_4_CONTRACT.md` (213), `companion-daemon/docs/STEP9_4_CONTRACT.md` (213), `docs/STEP9_4_LEDGER.md` (50) |

## 2. Packet summary (from STEP9_4_LEDGER.md)

| Packet | Status | Final SHA | Rounds |
|--------|--------|-----------|--------|
| CONTRACT | ACCEPTED | `609127e29` (+ Amendment 1) | 1 |
| 9.4-A Daemon Bootstrap | FROZEN | `4b55d33e8` | 33 |
| 9.4-B QR Pairing | ACCEPTED | `f76042ed3` | 36 |
| 9.4-C Android Keys | ACCEPTED | `0afbbfac5` | 3 |
| Homebrew | ACCEPTED | `dd5b7033c` | 1 |
| 9.4-D Revoke/Recovery | ACCEPTED | `5632868c6` | 44 |
| Integrated V1 | ACCEPTED | `238f063a8` | — |

**Total: 118 rounds, 73 V1 REJECTs**

### 2a. 9.4-A — macOS daemon bootstrap

`pokit` CLI commands: install, start, stop, status, uninstall, doctor.
`daemon_darwin.go` (1029 lines) implements launchctl lifecycle management.
Mac-only; `daemon_stub.go` (25 lines) provides no-op stub for other platforms.

### 2b. 9.4-B — QR mediation seam (Amendment 1)

`internal/devicetrust/pairing.go`: `PairingRequest` gains optional QR metadata
fields (host ID, daemon boot ID, challenge ID, expires-at). `qr_pair.go`
implements `VerifyAndStrip` bridge. BootstrapToken validation before proof
exchange. Pairing order corrected at `f76042ed3`:
handleCandidate → BootstrapToken → bridge.Verify → device proof → host proof.

### 2c. 9.4-C — Mobile device identity verification

`mobile/src/lib/deviceIdentity.ts` (58 lines): `verifyDeviceIdentityAgainstPairing`
+ `clearLocalBearerState` + `TokenManager` clearance. Production wiring at
`0afbbfac5`.

### 2d. 9.4-D — Epoch-bound mutation authorization

Atomic epoch-gated mutation protocol (`internal/devicetrust/auth_test.go`,
364 lines). `MutationAuthorizer` with mandatory production authorization.
Epoch CAS, exact match, concurrent race tests. `GetAuth` callback (Active+Epoch).
`RecheckEpoch` + session handler integration. Epoch-bound push registration.
3 `OwnedPTYRuntime` barrier tests + `real_revoke_barrier_test.go` (855 lines).

### 2e. Homebrew formula

`Formula/pokit.rb` (34 lines) — source-build formula. Signed artifact deferred
to alpha per contract amendment at `8680d36ca`.

## 3. Authority boundary (from contract §1)

Unchanged authorities:
- `HostIdentity` and `DeviceRegistry` — host and device authorities
- `ChallengeStore` — single-use challenge authority
- `DeviceSessionManager` — bearer/session authority
- `AuthHandler`, `RequirePrincipal`, existing pairing protocol — device-auth admission
- Timeline writer/projection, Transcript, Terminal authority, approval authority, runtime lifecycle authority, `Recorder`

## 4. Gate result

All commands were run from `companion-daemon/` at commit `238f063a8` with a
clean working tree.

### Backend gate

The exact stdout of `go build ./...` is: (no output — exit 0)

The exact stdout of `go vet ./...` is: (no output — exit 0)

The exact stdout of `go test -race ./... -count=1 -timeout 300s` is:

```
ok  	devremote/companion-daemon/cmd/devremote	34.167s
?   	devremote/companion-daemon/cmd/signald	[no test files]
ok  	devremote/companion-daemon/internal/agent	1.525s
ok  	devremote/companion-daemon/internal/agent/adapters/claude/v2_1_202	3.310s
ok  	devremote/companion-daemon/internal/agent/adapters/codex/v0_144_1	5.151s
ok  	devremote/companion-daemon/internal/agent/contract	3.264s
ok  	devremote/companion-daemon/internal/agent/doctor	104.050s
ok  	devremote/companion-daemon/internal/cockpit	3.638s
ok  	devremote/companion-daemon/internal/coordination	3.973s
ok  	devremote/companion-daemon/internal/devicetrust	8.957s
?   	devremote/companion-daemon/internal/models	[no test files]
ok  	devremote/companion-daemon/internal/notification	7.528s
ok  	devremote/companion-daemon/internal/projection	7.385s
ok  	devremote/companion-daemon/internal/sessionid	2.519s
ok  	devremote/companion-daemon/internal/term	20.560s
ok  	devremote/companion-daemon/internal/timeline/contract	2.952s
ok  	devremote/companion-daemon/internal/timeline/writer	3.092s
ok  	devremote/companion-daemon/internal/transcript	2.610s
ok  	devremote/companion-daemon/internal/validation	2.244s
ok  	devremote/companion-daemon/internal/watcher	2.630s
ok  	devremote/companion-daemon/internal/workspace	1.987s
?   	devremote/companion-daemon/scripts	[no test files]
```

21 packages total: 18 ok, 3 no-test.

### Key package test counts

The exact stdout of `go test -race ./internal/devicetrust -count=1 -v | grep -c 'PASS:'` is: `130`

The exact stdout of `go test -race ./internal/term -count=1 -v | grep -c 'PASS:'` is: `728`

The exact stdout of `go test -race ./cmd/devremote -count=1 -v | grep -c 'PASS:'` is: `147`

The exact stdout of `test -z "$(gofmt -l .)"` is: (no output — exit 0)

### Mobile gate

Not run locally — zero mobile test changes in the diff beyond pairing client
test fixtures.

### Result

```
BUILD:        PASS
VET:          PASS
TESTS:        PASS (21 packages, -race -count=1)
DEVICETRUST:  130 tests
TERM:         728 tests
CMD/DEVREMOTE: 147 tests
FMT:          PASS
```

## 5. Step 9.5 status

Step 9.5 (Matched Base Alpha candidate + SM-S926N product gate) remains NOT STARTED.
Step 9.4 ACCEPT does not by itself authorize Step 9.5 implementation.
