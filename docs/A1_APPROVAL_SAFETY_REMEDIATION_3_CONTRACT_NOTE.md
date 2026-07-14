# A1 Approval Safety — Remediation 3 pre-implementation contract note

Baseline `0521c382` (R2). Fixes only re-verification-2 findings R3-A..E. Binding
table maps every authoritative field to production code + a non-vacuous test, and
lists the concrete failing interleavings/counterexamples to close.

## Reproduced counterexamples to close

1. same DeviceID, different HostID/BearerSessionID/BootID, no permission → returns
   `already_accepted` (idempotency fast path precedes authority checks; ledger holds
   only `requesterDevice`).
2. claim input `authorised`, deliver `substituted`, receipt echoes claim binding →
   commit succeeds (payload not in the canonical binding; handler rebuilds it from a
   display snapshot).
3. `RuntimeDeliveryGate` test-only; `AcceptDelivery` releases the lock before any
   write → check-then-write race; old generation can accept bytes.
4. `delivery_failed` made universally terminal ("no retry") contradicts the frozen
   plan's bounded manual retry.
5. `sanitizeLogID` leaves `/Users/alice/private/repo` and `api_key=secret-value`
   verbatim.

## Binding / authority table

| authority field | source | bound in | verified at | label |
|---|---|---|---|---|
| ApprovalID, SessionID | ingest / path | `ApprovalExecutionBinding` | claim + commit (`b.equal`) | production-wired |
| Runtime (adapter/version/launch/stream) | `RuntimeOf` (server) | binding | claim (recompute vs stored) + commit + pre-delivery revalidate + `superseded` | production-wired |
| ActionDigest | store recompute from stored option+input | binding | claim + commit | production-wired |
| **PayloadDigest** (domain-sep) | store canonical payload | binding | claim; receipt `DeliveredPayloadDigest` compared at commit | production-wired (R3-B) |
| IdempotencyKey | client, canonical-validated | binding + ledger | claim | production-wired |
| **RequesterAuthContext** (device/host/bearer/boot/perm-digest) | `PrincipalFromContext` (server), sorted-perm sha256 | ledger + record | BEFORE idempotent replay; permission re-checked | production-wired (R3-A) |
| stored permission | ingest `requiredPerm` | record | every claim incl. replay (never caller-supplied) | production-wired |
| claim token | `crypto/rand` | record | commit | production-wired |
| ReceiptID | delivery boundary | receipt | commit requires non-empty on accept | production-wired |
| supersession | `SupersedeRuntime`/`InvalidateSession`/`Clear` (telemetry) | record `superseded` + gen high-water | claim + commit | production-wired |
| **delivery acceptance** | generation-owned gate endpoint | gate sink | atomic accept under gate lock; receipt only after | production-wired mechanism; **sink unavailable** (no provider channel) |
| **bounded manual retry** | record `retries` (max 2) | record | re-claim only on non-accepting failure, same key/binding/auth, not superseded/expired | production-wired (R3-D) |
| logs | `redactStr` + control-strip | — | log-capture tests | production-wired (R3-E) |

## Transition model (retry-aware)

```text
pending --claim--> executing --accept receipt--> approved/rejected/resolved
pending --expiry/supersede--> expired/invalidated
executing --non-accepting receipt--> delivery_failed (retryable if retries<2 & !superseded & !expired)
delivery_failed --same key/binding/auth re-claim--> executing (retries++)
delivery_failed (retries>=2 | superseded | expired) --> terminal (no retry)
```

## Idempotent-replay ordering (R3-A fix)

```text
validate key → find record → expire → actionable → option → recompute digest+payload
  → validate input (utf8, closed placement) → REQUESTER present + STORED permission
  → runtime match + !superseded + !expired
  → THEN idempotency: key present?
       binding+auth match & accepted  → already_accepted
       binding+auth match & failed&retryable → retry (executing, retries++)
       binding+auth match & executing → already_owned
       mismatch (any requester field / binding) → conflict
  → else pending → grant
```

## Delivery linearization (R3-C)

Accept is the sole acceptance point: under the per-session gate lock it verifies the
current endpoint == expected runtime + active + has a sink, then enqueues the exact
bytes and returns a ReceiptID — atomically, no lock across external I/O, no mutable
re-lookup. Activation/replacement/deactivation take the same lock, so a replacement
cannot interleave between check and enqueue. Production registers NO sink (no channel)
→ Accept fails → `unavailable`. The store `superseded` guard is retained as
defense-in-depth so a stale receipt still cannot commit.

## Blocked

No controlled provider mapping / production delivery channel exists → `provenActionMapping`
empty, sink unavailable, all production approvals non-actionable. A1 positive-path
item 2 = **BLOCKED**. No fixture substitutes for the production positive path.
