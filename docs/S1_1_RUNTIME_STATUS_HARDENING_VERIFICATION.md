# S1.1 Runtime Status Hardening — Independent Verification

Decision: **REJECT — three focused correctness blockers**

Reviewed implementation: `715c22b59320fa80628b394bde1cd4a4a07571e0`
Reviewed report HEAD: `1547c934113eb21a8dcc0e1ff47eb037493b3125`
Accepted S1 ancestor: `70ef5df28dede7b0f3025eeaab7826f76a229fbf`
Canonical branch: `feature/phase10-multi-adapter`

The implementation is well scoped and all automated gates pass, but the three
issues below contradict the authoritative S1.1 A/B/C handoff. A1 must not begin
until they are remediated and independently re-verified.

## 1. Repository and gate evidence

- canonical remote: `https://github.com/mhkim315/DevRemote.git`;
- local and remote review HEAD: `1547c934113eb21a8dcc0e1ff47eb037493b3125`;
- accepted S1 and all recorded T0/T1/T2/D1/T3 ancestors: present;
- worktree at review start: clean;
- focused `go test -race` for agent, transcript, mux, and term: pass;
- `sh scripts/build-gate.sh`: **ALL GATES PASSED**, including backend
  build/vet/race, mobile typecheck/Jest, Android Kotlin compile, invariants, and
  secret scan.

The first sandboxed term run could not bind an `httptest` localhost socket. The
same term race suite passed when rerun outside that sandbox restriction; this is
an environment result, not a product failure.

## 2. BLOCKER 1 — `WinningSeq` can identify the wrong evidence

Confirmed code:

- `internal/agent/contract/validate.go:ResolveStatus` selects by provenance rank
  and then normalized numeric confidence.
- `internal/term/agent_status_store.go:locateWinningSeq` matches only status and
  provenance; it does not match the winning confidence.

Counterexample: two `working/native_log` events have different confidence. If
the newer/larger-Seq event has confidence `0.5` and an older event has confidence
`0.9`, `ResolveStatus` selects the older `0.9` evidence, while
`locateWinningSeq` returns the newer `0.5` event's Seq. The internal anti-replay
high-water is then bound to an event that did not win.

This violates S1.1-A's requirement that the bounded reference identify the
exact resolver winner. Existing tests cover equal confidence, but not equal
status/provenance with unequal confidence.

Required remediation:

- bind the Seq to the exact resolver output, including confidence after the same
  normalization used by the frozen contract;
- do not fork or reimplement status precedence;
- add unequal-confidence and out-of-range-confidence regression cases, including
  one-shot/incremental ordering.

## 3. BLOCKER 2 — adapter identity is stored but not verified

Confirmed code:

- `internal/transcript/launch_binding.go:LaunchBinding.Adapter` records the
  adapter;
- `LaunchCorrelation` receives provider, version, PID, and start time only;
- `internal/term/adapter_state.go:launchCorrelation` does not pass the current
  session adapter;
- no test rejects a binding whose adapter differs from the runtime adapter.

Consequently a binding claiming `Adapter: "controlled_pty"` can correlate when
evaluated for a different adapter if the other supplied fields match. This
violates the S1.1-B adapter/provider replacement rule and its required
different-adapter negative test.

Required remediation:

- require a non-empty exact adapter match at the correlation boundary;
- pass the actual production session adapter, not a caller-invented constant;
- add direct and production-path adapter-mismatch tests.

## 4. BLOCKER 3 — replacement publication and invalidation are not atomic

Confirmed production order in `internal/term/create.go:createFromProfile`:

```text
RegisterLaunch(...)       # replacement binding becomes observable
if replaced:
    Telemetry.InvalidateForLaunch(...)
```

`RegisterLaunch` and `InvalidateForLaunch` are separate synchronized operations.
A concurrent telemetry poll can observe the new launch generation after the
binding is published but before the status high-water and adapter-state reset are
installed. It can therefore attribute prior stream evidence to the new launch.
Worse, if that poll stores the new launch at a positive stream generation before
`InvalidateForLaunch(..., streamGen=0)`, the later invalidation is rejected by
the stream-generation stale-write rule.

The existing replacement test invokes registration and invalidation
sequentially; it does not exercise the publication window. This violates the
handoff requirement that incompatible prior authority be invalidated before the
new binding can accept positive evidence.

Required remediation:

- provide one production replacement boundary in which the new generation is
  reserved, prior status/ingestion authority is invalidated, and only then the
  new binding becomes observable;
- preserve the distinction between replacement invalidation and lifecycle
  `Clear`;
- add a barrier-controlled concurrency test proving a poll cannot observe the
  new binding before invalidation and cannot attach old stream evidence to it;
- retain race coverage and per-session isolation.

## 5. Verified non-blocking product limitation

The actual local CLI path in `cmd/devremote/client.go:runClient` still joins
arguments into one command string, and the local socket create path executes it
through the legacy `bash -c` branch. `createLocalControlled` does not register a
recognized launch binding. Therefore:

- `pokit run claude` / `pokit run codex` currently provide **managed lifecycle**,
  not recognized managed-agent authority;
- the recognized PID/start/LaunchGen path reviewed here is the preset HTTP
  profile path;
- arbitrary local commands correctly receive no supported-agent semantic
  authority.

This does not retroactively expand the authoritative A/B/C handoff and is not one
of the three remediation blockers. It is, however, a mandatory documentation and
O1-readiness boundary: do not call the current local CLI path
`recognized managed-agent` or `orchestration-certified`.

## 6. Stop condition

Remediate only the three blockers above, rerun the complete gate, update the
implementation report with a new full implementation SHA, and stop for another
independent S1.1 review. Do not start A1, N1, O1, O2, terminal compatibility
certification, or a local CLI redesign in this remediation.
