# M0/M1 Acceptance — a99070015

Verdict: ACCEPT

Accepted commit: `a99070015`
Initial rejected commit: `e4e2704d0`

## Accepted product contracts

### Safe HTTP creation

- HTTP controlled_pty creation is limited to daemon-approved presets;
- HTTP custom profile is rejected regardless of `InsecureLocalOnly`;
- controlled_pty legacy command-shaped HTTP requests are rejected;
- tunnel-to-localhost cannot select arbitrary command execution merely by
  changing JSON shape;
- executable paths remain daemon-owned and are not exposed by the profile API.

### Privileged local launch

- arbitrary `pokit run <command>` creation uses the existing `0600` Unix socket;
- the CLI no longer relies on tunnel-reachable HTTP for arbitrary command launch;
- local legacy command and custom argv forms are decoded/validated locally;
- valid CLI behavior remains available without weakening the HTTP boundary.

### Recorder readiness

- create returns `running` only after a single Recorder is ready;
- lookup/OpenStream/Recorder startup failure returns non-success/failed;
- failed startup terminates the newly created runtime;
- create-without-viewer retains no phantom subscriber;
- HTTP/mobile does not open or read the PTY.

### Strict request behavior

- malformed, object, number, null, missing, and empty local command values are
  rejected;
- invalid command data never falls back to a default shell session;
- canonical IDs remain daemon-generated;
- exact CWD validation/propagation remains intact.

## Verification

```text
GOCACHE=/tmp/devremote-a990700-go-cache \
go test -race ./internal/term ./internal/mux ./cmd/devremote -count=1

PASS
```

```text
sh scripts/build-gate.sh

Backend build/vet/race: PASS
Mobile typecheck: PASS
Invariants: PASS
Security scan: PASS
ALL GATES PASSED
```

## Scope boundary

Accepted:

- M0 lifecycle type/create contract foundation;
- M1 profiles and safe creation;
- privileged local CLI launch boundary;
- Recorder-ready create behavior.

Not accepted by this phase:

- M1.5 managed/external ownership capability;
- M2 Stop/Kill/Delete lifecycle;
- M3 mobile New Session UX;
- production remote authentication release gate;
- Transcript refactor.

## Next

Proceed to M1.5 using `docs/NEXT_SESSION_M1_5_HANDOFF.md`.
