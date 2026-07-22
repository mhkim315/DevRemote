# STEP5 Evidence — Workspace Identity, Clean Snapshots, and Cooperative Leases

Implementation commit: `ef50cd3230f77a215992a5de1b7f6f7d25b30f25`

STEP5 R2 implementation commit: `404a3e882dfe96275822abcd4597701f3865efdd`

`internal/workspace` is a pure, in-memory contract package. It executes no Git
commands, uses no filesystem locks, starts no goroutines, and cannot block
external editors, shells, or Git clients.

## Delivered contract

- `Identity` binds repository ID, shared-sequential/frozen-validation mode,
  base/current SHA, tree hash, snapshot ID, and the implemented, honestly
  disclosed `repo-only` isolation profile.
- `SnapshotManifest` requires a clean committed state and binds base/target
  SHA, tree/index hashes, untracked-manifest digest, and diff digest. Dirty
  worktree validation returns `ErrDirtySnapshot`; it is explicitly deferred.
- `Manager` is a cooperative POKIT-managed write ledger. It supports acquire,
  compare-and-release, heartbeat, expiry check, stale-owner rejection, epoch
  monotonicity, expiry-based daemon-crash recovery, and external-drift
  invalidation. It never claims exclusive filesystem ownership.
- `FindingBinding.Stale` marks a validation result stale when repository,
  snapshot, or tree identity changes.

The default-off `--enable-workspace-lease` config flag constructs this isolated
contract only. There are no live terminal/input/approval/lifecycle callbacks,
no REST/WS routes, and no mobile changes.

## R2 isolation correction

The roadmap vocabulary remains `repo+process`, `repo+network-policy`,
`containerized`, and `VM-isolated` (corrected from `vm`), but none has a
concrete enforcement implementation in this repository. `Identity.Validate`
therefore rejects every one of them; only `repo-only` is accepted. Tests prove
each rejected claim, preventing the contract from overstating isolation.

## Tests and gates

Tests cover clean/dirty snapshot behavior, isolation-vocabulary rejection,
compare-and-release, stale owners, expiry/crash recovery, drift invalidation,
finding staleness, and flag default-off construction.

Passed:

```text
go build ./...
go vet ./...
go test -race ./... -count=1 -timeout 300s
go test ./internal/timeline/contract -run=^$ -fuzz=FuzzEnvelopeValidator -fuzztime=2s
go test ./internal/timeline/contract -run=^$ -fuzz=FuzzComputedEventIDStable -fuzztime=2s
test -z "$(gofmt -l .)"
cd ../mobile && npx tsc --noEmit && npm test -- --runInBand
```
