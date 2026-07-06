# Adapter Expansion Phase 1 Acceptance

Accepted baseline: `1f1b5de` (`Phase 1 수정 (12차)`)

Verdict: **ACCEPTED**

Phase 1 is accepted. The identity/discovery contract and the Phase 1 regression coverage are now sufficient to proceed to the next planned phase.

## What was verified

The final revision closed the remaining blocker from revalidation 11:

- actual API/handler POST create canonicalization now includes a cmux-style local ID;
- response ID is asserted as exactly `cmux:surface:42`;
- the duplicate `err == nil` check in `TestFindSession_AdapterUnavailable` was removed.

Previously accumulated Phase 1 contract coverage is also present:

- canonical parser/formatter supports adapter-prefixed IDs and local IDs containing colons;
- Unicode local IDs are covered;
- adapter name validation and duplicate adapter rejection are covered;
- missing/invalid adapter/session errors preserve typed sentinel errors;
- `Refresh` timeout and cancellation taxonomy is tested;
- list ordering is deterministic;
- stale cache is retained for list/snapshot display when refresh fails;
- live `FindSession` does not return stale sessions when forced refresh fails;
- ended sessions disappear from live lookup after successful refresh;
- tmux-style create returns local ID and handler canonicalizes;
- cmux adapter create returns local `surface:<N>`;
- malformed cmux create output returns an error and empty ID;
- cmux-style handler/API create response is canonicalized exactly once.

## Verification commands

### Targeted mux regression loop

```sh
GOCACHE=/tmp/devremote-phase1-reverify12-go-cache go test ./internal/mux \
  -run "TestRefresh|TestFindSession|TestCanonicalID|TestCreateSession|TestCmux|TestStaleCacheOnFailure" \
  -count=100
```

Result: **PASS**

### Targeted mux/term API boundary test

Initial sandboxed run failed because the sandbox blocked local listener creation for `httptest`. The same command was rerun outside the sandbox.

```sh
GOCACHE=/tmp/devremote-phase1-reverify12-go-cache go test ./internal/mux ./internal/term -count=1
```

Result outside sandbox: **PASS**

### Full Go verification

Initial sandboxed run failed because the sandbox blocked local TCP listener and Unix socket binding. The same command was rerun outside the sandbox.

```sh
GOCACHE=/tmp/devremote-phase1-reverify12-go-cache go vet ./...
GOCACHE=/tmp/devremote-phase1-reverify12-go-cache go test ./...
GOCACHE=/tmp/devremote-phase1-reverify12-go-cache go test -race ./...
```

Result outside sandbox: **PASS**

### Mobile typecheck

```sh
npx tsc --noEmit
```

Result: **PASS**

## Notes for Phase 2

Phase 1 acceptance only covers identity/discovery contract hardening. It does not imply that later phases should skip their own fixture-adapter and third-backend proof steps.

Proceed to the next phase only with the same rule used here: prove the contract at the production boundary, not only through helper-level tests.
