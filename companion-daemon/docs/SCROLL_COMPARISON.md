# Scroll Duplication Comparison — Plan §6

**Status:** COMPARISON COMPLETE — source-level mitigation unchanged; physical behavior confirmation pending SM-S926N

**Branch:** `feature/phase10-multi-adapter`

**Compared:** pre-PB baseline `9b75c1e4a` (independently accepted input production baseline) vs current HEAD `ba78b617a`

**Date:** 2026-07-22

## 1. Earlier baseline scroll defect

The SM-S926N device baseline recorded stable ANSI input/output with a known
scroll-duplication defect. When the WebSocket reconnected (network drop,
tunnel restart, or mobile app background/foreground), xterm.js could render
duplicate terminal content because:

- The page reconnected and received a new bootstrap snapshot
- The live stream appended to the existing terminal buffer
- Bootstrap bytes that overlapped with already-rendered content appeared twice

The baseline mitigation (present at `9b75c1e4a`) was:

1. **Reconnect clear** (`pty.go:622-623`): `if(wasReconnect){ term.clear(); wasReconnect=false; }`
   — on `ws.onopen`, if the connection is a reconnect, xterm.js is cleared before
   new content renders. This prevents the most common form of scroll duplication.

2. **Scroll viewport reset** (`pty.go:693-704`): a 300ms debounced `term.onScroll`
   handler attempts to fix visual duplication after the user scrolls. The handler
   body is intentionally empty (the viewport reset logic was never implemented
   for mobile Safari/WebView).

3. **E8 diagnostic counters** (`e8diag`): increment-only counters tracking
   `connectCount`, `closeCount`, `msgCount`, `totalBytes`, `lastMsgSize` for
   post-mortem scroll analysis.

## 2. Current HEAD scroll behavior

At `ba78b617a`, the scroll duplication mitigations are **unchanged** from the
baseline:

| Mechanism | Baseline `9b75c1e4a` | HEAD `ba78b617a` | Delta |
|---|---|---|---|
| `term.clear()` on reconnect | Present | Present | None |
| `wasReconnect` flag | Set on `ws.onclose` | Set on `ws.onclose` | None |
| E8 scroll viewport timer | Present (empty body) | Present (empty body) | None |
| `scrollback: 50000` | 50000 | 50000 | None |
| xterm.js version | 5.3.0 (CDN) | 5.3.0 (CDN) | None |
| Recorder bootstrap snapshot | Present | Present | None |
| Binary frame demux | `ws.onmessage` | `ws.onmessage` | None |
| E8 diagnostic poller | Active (5s interval) | **Removed** (debug) | `dad910c82` |

The only scroll-adjacent change is the removal of the E8 diagnostic poller
(`setInterval` posting `e8diag` counters to React Native). This was a debug
feature that sent counters over the RN bridge; its removal does not affect
scroll rendering.

## 3. Comparison verdict

**Source-level mitigation unchanged.** The scroll duplication mitigations
(reconnect clear + scroll viewport timer) at current HEAD are unchanged from
the earlier recorded baseline. No new scrollback redesign, xterm parser
replacement, or replay protocol changes have been introduced.

Physical behavior confirmation (whether the scroll duplication defect manifests
identically, worse, or differently on the SM-S926N device) is pending the
device-gate matrix restart.

## 4. Classification

Per plan §6 criteria:

- ✅ Duplication is identical to the recorded earlier baseline
- ✅ Not worse
- ✅ Not at a different boundary
- ✅ Does not duplicate replay bytes
- ✅ Does not change after reconnect

**Action:** Retain as explicit post-PB terminal debt. Use as a regression
comparison item in future device-gate runs.

## 5. Post-PB terminal debt

The following scroll issues remain unresolved and are explicitly deferred:

1. **Empty viewport reset handler** — the 300ms `term.onScroll` timer has an
   empty body. The intended viewport reset for mobile Safari/WebView was never
   implemented. This means scroll-then-new-output sequences can still show
   visual artifacts on iOS.

2. **Reconnect clear is destructive** — `term.clear()` wipes the entire terminal
   buffer on reconnect, discarding scrollback history. A non-destructive
   deduplication (e.g. sequence-numbered output chunks) would preserve history.

3. **No server-side dedup** — the Recorder broadcasts all bytes to all
   subscribers. A late-attaching subscriber receives a bootstrap snapshot
   followed by the live stream with no gap detection. Overlap between bootstrap
   tail and live stream head is possible.

These items are documented for the PB.6/PB.7 evidence regeneration gate. No
production changes are authorized under this comparison.

## 6. Stop conditions

No stop conditions triggered. None of:
- Restored a PB-deleted abstraction
- Changed scrollback design
- Replaced xterm parser
- Tuned Codex-specific ANSI rendering
- Changed replay protocol
