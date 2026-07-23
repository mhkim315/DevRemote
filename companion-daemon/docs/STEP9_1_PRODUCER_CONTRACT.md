# STEP 9.1 — Operational Timeline Producer Contract

## Status and scope

This is a contract correction for the first operational Timeline producers. It
does not add a producer, normalizer, route, mobile DTO, or Timeline authority.
It replaces the unsafe assumptions identified during T1 review. CT-P2 remains
blocked; the existing envelope contract, redaction boundary, event identity,
and fail-open Timeline ownership remain unchanged.

`EventDegraded` remains a valid historical envelope kind, but is not in the
STEP 9.1 producer scope. A Timeline write failure therefore never attempts to
write another Timeline event about that failure.

## 1. Non-forgeable producer authorization

An envelope field is evidence supplied by the caller, not an authorization
claim. In particular, a caller-controlled `SessionID` prefix is never proof
that it may submit as Codex or Claude.

Composition creates one opaque, producer-scoped submit handle per accepted
managed runtime generation. A handle binds all of the following at creation:

- closed provider identity and required capability;
- canonical provider session-ID adapter/prefix;
- exact managed session ID, runtime ID, and launch generation; and
- a composition-owned, unexported capability token.

The producer receives only that handle, not a constructor for one. Its submit
operation accepts an event candidate only after comparing its provider,
session, runtime, and generation with the bound values. Mismatch, an unknown
provider, an expired/replaced generation, or a forged look-alike session prefix
is rejected before any Timeline enqueue. The handle cannot be reconstructed
from an `Envelope`, serialized, or widened to another session.

### Bind and revoke transition

The handle is not available merely because a sink was injected. Composition
owns a `Bind` transition with this exact ordering:

1. the managed provider commits registration of the session, runtime ID, and
   generation;
2. composition creates the opaque handle from that committed identity;
3. composition installs that handle into that one managed runtime's neutral
   sink slot; and
4. only then can that runtime make post-commit observations visible to the
   Timeline adapter.

Replacement, exit, kill, and delete revoke the installed handle before the
old runtime can be observed as current again. Revocation is generation-bound,
linearized with the provider's runtime replacement transition, and is safe to
race with a submit: a racing submit either completes against the still-current
binding or is rejected as stale; it cannot become a submit for the replacement
runtime. Bind never reuses a handle across runtimes, including two runtimes
with the same provider and session-like prefix.

Required contract tests:

- forged `codex:` / `claude:` look-alike prefixes are rejected;
- a Codex handle rejects Claude candidates and vice versa;
- a valid provider handle rejects another managed session or generation; and
- replacement/revocation invalidates the old handle;
- a handle cannot cross runtime instances; and
- a stale-handle submit raced with replacement is never accepted for the new
  generation.

## 2. Neutral producer seam and minimal wiring

There is no existing production emission seam that can satisfy this contract
without modifying `internal/term`: managed runtime events are owned there,
while Timeline must not become a term authority dependency. STEP 9.1 therefore
permits a small neutral term-owned interface, injected by composition:

```go
// internal/term: no Timeline package import.
type OperationalEventSink interface {
    SubmitAfterCommit(OperationalEvent)
}
```

`OperationalEvent` is a closed, redacted, provider-neutral post-commit record.
It carries only the already-authoritative provider/runtime/session/generation,
closed event kind, source identity/position, bounded references, and a
redacted-or-digest payload. It carries no raw terminal bytes, prompt, approval
payload, secret, transcript, or arbitrary metadata.

The composition adapter owns conversion from `OperationalEvent` to the
Timeline envelope and owns the opaque producer handles above. It is fail-open:
the term call occurs only after the relevant primary commit; it returns no
result to lifecycle, tool, approval, stream, transport, or recorder authority;
and it never holds a term authority lock across Timeline I/O.

`SubmitAfterCommit` is specifically a bounded, non-blocking seam operation,
not a direct filesystem call. The composition-owned implementation recovers a
sink panic at the seam and performs one bounded enqueue; a full queue becomes
a health/drop count and returns immediately. Its downstream writer worker may
block independently, but no term authority goroutine waits for it. A nil sink,
panic, saturated queue, and blocked downstream sink all have the same
authority outcome: the already-committed lifecycle, approval, tool, or stream
operation continues unchanged.

The only permitted initial hooks are post-commit managed Codex and managed
Claude lifecycle, tool, approval, and stream observations. There is no generic
term event bus, automatic replay, provider selection, callback into authority,
or transcript-derived producer. A nil sink is the default and must preserve
current behavior exactly.

Required seam tests inject a panicking sink, a saturated composition queue, and
a downstream sink with a blocked write for each lifecycle, approval, tool, and
stream hook. They prove the producer authority returns its committed outcome
without panic or Timeline-dependent latency.

## 3. Shutdown and stuck I/O truthfulness

Go cannot cancel an arbitrary blocked `Write`. STEP 9.1 therefore must not
claim that `Close` guarantees a five-second worker exit or goroutine baseline.
The producer queue has explicit `open → closing → closed` state, serialized
with submit. The close transition atomically detaches its queue, counts every
pending item as dropped, and rejects every subsequent submit. It starts no new
write after the transition and performs no post-return queue drain or write.

`Close` returns a structured outcome:

```text
workerExited: bool
inFlight:     non-negative count
pendingDropped: non-negative count
```

If I/O exits, `workerExited=true`, `inFlight=0`, and the worker baseline must
return. If an injected write remains blocked at the deadline, `Close` returns
with `workerExited=false`, its remaining in-flight count, and the atomically
accounted pending drops; daemon shutdown continues fail-open. It does not
falsely report success, recursively emit a failure event, or promise that the
blocked goroutine disappeared.

The in-flight worker owns the file close exactly once. If its blocked `Write`
later returns, it records that write's terminal outcome, exits without reading
another item, and closes the file exactly once. `Stats` and health state use
one synchronization boundary with the close state, so post-close submit,
double-close, worker completion, and health reads are race-free.

Required tests cover normal worker exit and baseline restoration; a stuck write
that produces the explicit incomplete outcome; pending items atomically counted
as dropped; no post-close submit or post-return drain/write; delayed blocked
write completion with one file close; concurrent `Submit` + `Close` without
send-on-closed panic or data race; and idempotent close/health reads.

## 4. Real-provider translation and privacy sentinel tests

Envelope-only fixtures are insufficient. Every enabled Codex and Claude source
must be exercised through its real post-commit translation hook for lifecycle,
tool, approval, and stream observations. Test fixtures include unique secret
sentinels in provider text, tool input/output, approval material, and transport
diagnostics.

For each source, assertions scan every enabled operational surface:

- Timeline JSONL/file sink and bounded in-memory ring;
- cockpit/read endpoint payload, if the projection is enabled; and
- logger/audit capture used by that producer path.

None may contain the sentinels. The expected output is only the contract's
bounded redacted summary, digest, opaque reference, and identity fields.
Coverage is per provider and per event class; one fabricated envelope test
cannot substitute for either provider's translation path.

## 5. Degradation boundary

STEP 9.1 producer code does not emit `EventDegraded` for sink, queue, write,
sync, close, or observer failure. Those outcomes are retained only as
non-recursive in-memory producer health counters/last-failure metadata for
equivalence diagnostics. They are not Timeline evidence and cannot authorize,
alter lifecycle, trigger a retry, or synthesize an event.

A future separately approved health-evidence design may map a successfully
observed external degradation fact to `EventDegraded`; it must use a distinct
primary source and prove it cannot recurse through the failing Timeline sink.
