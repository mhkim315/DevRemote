# M2 Acceptance — eae1e96c9

Verdict: ACCEPT

Accepted commit: `eae1e96c9`
Initial rejected commit: `b00f2ca0a`

## Accepted contracts

- managed lifecycle routes are capability-gated;
- legacy query DELETE cannot bypass managed terminal-state/history rules;
- running managed Delete returns conflict and preserves runtime/history;
- Stop performs SIGTERM, bounded wait, and SIGKILL escalation;
- terminal success requires confirmed Recorder completion;
- unconfirmed termination returns failure without removing runtime/Recorder;
- fast natural exit converges through the exact Recorder passed at create;
- Stop/Kill/natural exit share one terminal cleanup path;
- Delete is serialized with lifecycle operations;
- concurrent Delete has one success and stable not-found results;
- Stop and natural exit remove controlled adapter session state;
- process-group guarantee is limited to the daemon-owned process group.

## Verification

```text
go test -race ./internal/term ./internal/mux ./cmd/devremote -count=1
PASS

sh scripts/build-gate.sh
ALL GATES PASSED
```

## Accepted limitations

- exit code is not captured;
- catalog is not persistent across daemon restart;
- descendants that create a separate session/process group are outside MVP;
- termination failure requires a later Force Kill/recovery UX;
- per-session lock retention may need a long-running cleanup policy.

## Next

Proceed to M2.5 minimum device-trust security before remote M3 lifecycle UX.
