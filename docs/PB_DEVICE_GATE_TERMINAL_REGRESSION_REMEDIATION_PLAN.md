# PB Device-Gate Terminal Regression Remediation Plan

**Status:** AUTHORITATIVE AND FROZEN — DEVICE GATE PAUSED

**Branch:** `feature/phase10-multi-adapter`

**Last independently accepted input production baseline:** `9b75c1e4a`

**Current diagnostic checkpoint:** `dad910c82154f56f51553a995ebbb07e196eda7f`

**Target device:** Samsung SM-S926N (Galaxy S24 Ultra), Android 16

**PB DEVICE CANDIDATE SHA:** **UNSET**

**PB ACCEPT SHA:** **UNSET**

## 1. Purpose and authority

The first SM-S926N run proved pairing, authentication, terminal discovery, and
basic shell execution far enough to expose a new terminal regression. Direct
xterm keyboard input and simple line-oriented commands can work, while native
Send/macros remain unauthorized or unstable and full-screen ANSI applications
render with pathological wrapping. These results are diagnostic only. They are
not acceptable device-gate evidence.

The physical matrix is paused and returns to the PB implementation and
independent-verification workflow. This document is the sole authority for that
bounded remediation. It amends only the device-gate portion of
[`PRE_DEVICE_QR_INPUT_REMEDIATION_PLAN.md`](PRE_DEVICE_QR_INPUT_REMEDIATION_PLAN.md).
It does not reopen PA4 isolation, PB deletion, QR protocol or cryptography,
device-role policy, Input-B outcome semantics, or provider behavior.

Historical PB evidence is immutable. The three diagnostic commits ending at
`dad910c82` are an unaccepted checkpoint, not a new PB baseline or ACCEPT SHA.

## 2. Confirmed regression boundaries

### 2.1 Lost managed-PTY geometry authority

The accepted pre-PB native-session path exposed the live PTY size. The PB.5 V1
launcher path creates `SpawnConfig` values with zero rows and columns, starts
the PTY without a size, and sets a size only when both values are positive. Its
`nativePTYHandle` implements resize but not live size retrieval. Consequently,
the Recorder/TerminalTransport geometry chain cannot report the managed PTY
size after the V1 cutover.

Full-screen applications use the PTY's own column count to lay out alternate-
screen output. A simple `ls` result therefore does not disprove a geometry
defect. The prior accepted behavior measured a 30-row by 100-column spawn and
tracked later resize changes. That behavior, not viewport inference, is the
compatibility target.

The mobile injected page also retains a three-second `/term/size` fetch. In a
paired WebView it has no bearer token, can receive `401`, and falls back to
viewport-derived geometry. No bearer credential may be moved into page script
to preserve that poll.

### 2.2 Split and duplicated terminal-control authority

The daemon page consumes the server `hello` to enable direct xterm keyboard
input. React Native independently derives `deviceCanInput` from a forwarded
`hello` and gates Send, paste, and macros. This explains the observed split:
keyboard input can work while native controls, including Ctrl+C, remain blocked.

At the diagnostic checkpoint both the daemon page handler and the early mobile
WebSocket interceptor forward `hello`, `read_only`, and `input_result` frames to
React Native. A duplicated `hello` can reset pending Input-B operation state.
The implementation comment claiming exactly-once forwarding is therefore not
an acceptance proof.

## 3. Frozen execution sequence

```text
DEVICE_GATE_PAUSED at diagnostic checkpoint dad910c82
→ TERM-G1 managed PTY geometry authority restoration
→ independent TERM-G1 ACCEPT
→ TERM-C1 single control bridge and native capability convergence
→ independent TERM-C1 ACCEPT
→ conditional scroll-duplication comparison
→ PB.6/PB.7 full automated evidence regeneration
→ exact PB_DEVICE_CANDIDATE_SHA freeze
→ daemon and APK built from that same candidate
→ restart the complete SM-S926N matrix from pairing
→ final independent PB ACCEPT
```

No production packet may start before the preceding packet is independently
accepted. Each packet uses an implementation commit followed by an
evidence-only commit. The device tester preserves screenshots, logs, role and
permission observations, and artifact identities, then stops implementation
work until a new candidate is frozen.

## 4. TERM-G1 — managed PTY geometry authority restoration

### 4.1 Required implementation boundary

- Give every production managed PTY an explicit initial size before the child
  process starts. Restore the previously demonstrated 30-row by 100-column
  behavior unless repository history proves a later independently accepted
  managed default.
- Restore live `GetSize()` capability on the V1 native PTY handle through the
  existing narrow optional geometry boundary. Do not reintroduce mux,
  Registry, Adapter, attach, tmux, cmux, or localpty abstractions.
- Preserve exact-generation transport ownership. Geometry is read from the
  exact captured PTY instance and must never re-resolve a replacement by ID.
- Preserve resize semantics and prove `GetSize()` observes the resulting exact
  rows and columns.
- Make authenticated bootstrap and the terminal WebSocket geometry control
  frame the only production mobile geometry sources.
- Remove the paired-WebView periodic unauthenticated `/term/size` fetch. Never
  expose a bearer token to WebView JavaScript to make the fetch succeed.
- The mobile viewer mirrors server-authoritative PTY geometry. It must not
  resize the shared PTY merely to fit the phone viewport.
- Reject or ignore zero, negative, non-integral, implausibly large, stale-
  generation, or wrong-session geometry without destroying the last valid
  server geometry.

### 4.2 Mandatory tests

- Spawn a real native managed PTY with the frozen default and assert
  `GetSize() == (30, 100)` before application output is interpreted.
- Resize the exact PTY, assert the new live size, and prove a stale generation
  cannot observe or resize its replacement.
- Prove Recorder through TerminalTransport reports the same rows/columns and
  that the WS geometry frame is bound to the expected session and generation.
- Bootstrap and WS geometry paths agree; reconnect obtains the new connection's
  exact geometry.
- Static and runtime proof that paired production WebView code performs zero
  `/term/size` fetches and contains no bearer token.
- Mobile tests cover valid geometry, invalid bounds, wrong session/generation,
  reconnect, and preservation of the last valid geometry.
- ANSI fixtures cover ordinary output, wide lines, cursor movement, colors,
  alternate-screen entry/exit, and no one-character rewrapping at 100 columns.

### 4.3 Stop conditions

Stop and reject if the change restores a PB-deleted abstraction, lets mobile
viewport measurements become PTY authority, moves credentials into WebView,
accepts geometry from the wrong generation, changes Input-B framing, or tunes a
provider-specific renderer instead of restoring terminal geometry.

## 5. TERM-C1 — single control bridge and native capability convergence

### 5.1 Required implementation boundary

- Establish one authoritative WebSocket control dispatcher per connection.
  Each `hello`, `read_only`, `input_pending`, `input_result`, and `geometry`
  frame is parsed and forwarded to React Native at most once.
- The same server `hello` snapshot governs page-level direct xterm input and
  React Native `deviceCanInput`; neither side may infer permission from session
  capability, stored role, QR data, or local state.
- Preserve exact binding to server-generated connection ID, session ID, and
  generation. A mismatch fails closed and records only safe structural
  diagnostics identifying the failed predicate.
- A duplicate or repeated identical `hello` must not clear or reclassify a
  pending acknowledged input operation. A genuinely new connection still
  converts unresolved delivery to `delivery_unknown` as frozen by Input-B.
- Send, paste, macros, Ctrl+C, and direct keyboard input must all obey the same
  effective permission snapshot. Owner-only input policy remains unchanged.
- Preserve versioned acknowledged production input, exact captured transport,
  closed result vocabulary, duplicate-result cache, no automatic retry, and
  the existing 40 ms text/Enter split.
- Do not add REST permission fallback, raw-binary production fallback, member
  promotion, or transport lookup by session ID.

### 5.2 Mandatory tests

- A real WebSocket-to-daemon-page-to-WebView-to-React-Native integration test
  proves one valid `hello` produces exactly one native delivery and enables all
  authorized surfaces.
- Wrong session, connection, generation, missing capability, malformed frame,
  and unknown permission each fail closed without a PTY write.
- Exactly-once forwarding tests cover `hello`, `read_only`, `input_pending`,
  `input_result`, and reconnect sequences.
- Repeated identical `hello` cannot clear pending Input-B ACK state; a new
  connection preserves the existing `delivery_unknown` rule.
- Ctrl+C sends exactly one byte `0x03` through acknowledged input and receives
  `accepted`; denial produces zero write. Paste and every macro receive the same
  positive and negative controls.
- Text plus Enter remains one UI operation of two acknowledged writes, and only
  two accepted outcomes display **Delivered to terminal**.
- Tests run against the served production HTML and injected mobile bridge, not
  only by calling the React Native `onMessage` callback directly.

### 5.3 Stop conditions

Stop and reject if more than one bridge owns forwarding, any local field grants
permission, duplicate control frames mutate delivery state, unauthorized input
reaches the PTY, production falls back to legacy binary input, or the UI claims
shell success rather than terminal delivery.

## 6. Scroll-duplication boundary

The earlier device baseline had stable ANSI input/output with a known scroll-
duplication defect. Geometry and control authority must be restored before that
defect is interpreted. After TERM-G1 and TERM-C1 ACCEPT:

- if duplication is identical to the recorded earlier baseline, retain it as
  explicit post-PB terminal debt and use it as a regression comparison item;
- if it is worse, occurs at a different boundary, duplicates replay bytes, or
  changes after reconnect, stop and propose a separate bounded terminal replay/
  buffer packet before freezing the device candidate;
- do not combine scrollback redesign, xterm parser replacement, Codex-specific
  ANSI tuning, or replay protocol changes into TERM-G1 or TERM-C1.

## 7. Evidence and SHA flow

The following identities remain distinct and unset until produced:

| Identity | Meaning |
|---|---|
| `TERM_G1_IMPL_SHA` | Geometry production/tests only |
| `TERM_G1_EVIDENCE_SHA` | Documentation-only evidence naming exact G1 implementation |
| `TERM_G1_ACCEPT_SHA` | Independent G1 verdict identity |
| `TERM_C1_IMPL_SHA` | Control bridge/capability production/tests only |
| `TERM_C1_EVIDENCE_SHA` | Documentation-only evidence naming exact C1 implementation |
| `TERM_C1_ACCEPT_SHA` | Independent C1 verdict identity |
| `PB_DEVICE_CANDIDATE_SHA` | Final production/mobile source used to build both artifacts |
| `PB_AUTOMATED_EVIDENCE_SHA` | Later documentation head for regenerated PB.6/PB.7 gates |
| `PB_ACCEPT_SHA` | Final independent verdict after the complete physical matrix |

Each packet runs focused and full daemon build/vet/race, mobile typecheck/unit
tests, formatting and diff checks, PA4/PB architecture gates, exact ancestry,
HEAD/upstream equality, and clean-worktree checks. Evidence must name artifact
and source identities and must state failures honestly; logging or screenshots
cannot replace assertions.

## 8. Device-gate resume criteria

The SM-S926N matrix restarts from its first pairing scenario only after:

- TERM-G1 and TERM-C1 have independent ACCEPT identities;
- the managed PTY reports a valid exact generation-bound geometry at spawn,
  after resize, and after reconnect;
- direct keyboard, Send, paste, Ctrl+C, and macros agree on the same owner
  permission and acknowledged delivery behavior;
- no paired WebView performs unauthenticated REST polling;
- the conditional scroll comparison is classified;
- PB.6/PB.7 automated evidence is regenerated;
- clean daemon and APK artifacts attest the same exact
  `PB_DEVICE_CANDIDATE_SHA`;
- `PB_ACCEPT_SHA` remains **UNSET**.

Prior partial device results remain diagnostic attachments only. They may not
be spliced into the restarted matrix or used to assign PB ACCEPT.

## 9. Explicitly out of scope

- QR payload/protocol/cryptography changes and future Pairing V2.
- Role changes, member input permission, automatic owner transfer, or remote
  self-promotion.
- Provider-specific Codex/Claude rendering hacks, Enter-delay tuning, shell
  bootstrap redesign, empty-terminal recovery, or terminal restart redesign.
- Scrollback/replay redesign unless the conditional comparison opens a new
  independently reviewed packet.
- Reintroduction of any PA4-isolated or PB-deleted legacy authority.
- Canonical Timeline and all later provider/product phases.
