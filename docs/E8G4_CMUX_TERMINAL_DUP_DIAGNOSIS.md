# E8g4 cmux Terminal Duplication — Diagnosis Report

Date: 2026-07-09
Commit: 21770d6d2 (force-flush fix ACCEPTED)

## Observation

Real device (LTE, cmux session) on codex surface:2:

1. **Terminal**: infinite duplication, cannot scroll to bottom (P0)
2. **Transcript**: duplication still present after force-flush fix

## Root Cause Analysis

### Terminal Duplication

**Mechanism**: `ESC[2J` in cmux snapshots adds scrollback entries.

Flow:
```
cmux pollScreen (every 500ms)
  → write "\033[2J\033[H" + full_screen_content + "\033[9999m"
  → Recorder drainSnapshot → broadcast chunks to subscribers
  → HandleWS → WebSocket binary messages
  → WebView xterm.js receives ESC[2J ESC[H + content
  → xterm.js: ESC[2J triggers "save current screen to scrollback, then clear"
  → new content renders on clean screen
  → scrollback now has old screen copy
```

At 2 snapshots/second, with ~30-line screens:
- 10 seconds = 20 snapshots = 600+ scrollback lines
- 60 seconds = 120 snapshots = 3600+ scrollback lines
- User experiences "infinite scrolling" through nearly-identical screen copies

Each snapshot replaces the visible screen, but the OLD screen goes into
xterm.js scrollback. Over time, scrollback fills with duplicate copies.

**Evidence**:
- cmux adapter writes `\033[2J\033[H` prefix on every poll change
- `\033[2J` is ESC[2J = "clear entire screen" — standard VT100
- xterm.js handles ESC[2J correctly: saves current screen to scrollback, clears
- Each snapshot creates 1 scrollback entry of screen-sized content

### Transcript Duplication

**Mechanism**: Terminal scrollback growth feeds into Transcript.

Because the Terminal accumulates scrollback (via ESC[2J), the cmux screen
itself grows over time. The screenTracker processes the growing screen and
commits new lines. Some of these are the duplicated scrollback entries
from the Terminal.

This is a secondary effect — the primary issue is the Terminal duplication.

### Not the cause

- ✗ Recorder broadcast duplication (each chunk broadcast once)
- ✗ Subscriber fanout (one subscriber per WebSocket)
- ✗ drainSnapshot chunking (chunks delivered sequentially, no overlap)
- ✗ force-flush overcommit (proven fixed in 21770d6d2)

## Structural Issue

cmux is a **screen snapshot adapter**, not a PTY byte stream adapter.

| Property | PTY byte stream | cmux snapshot |
|----------|----------------|---------------|
| Data | Incremental bytes | Full screen every 500ms |
| Clear screen | Rare (only app-initiated) | Every snapshot starts with ESC[2J |
| Scrollback | Natural terminal behavior | Accumulates screen copies |
| Transcript | Natural delta | Must extract from snapshots |
| Live rendering | xterm byte stream | Should be screen-replace |

Treating cmux as xterm live stream causes snapshot frames to accumulate
as scrollback. This is the root cause of both Terminal and Transcript
duplication.

## Recommendation

**NEEDS DESIGN CHANGE** — cmux Terminal should not use xterm live stream.

Short-term options:
A. Disable cmux Terminal live view (show static snapshot or "unsupported")
B. Replace ESC[2J with ESC[H + ESC[J (clear from cursor, no scrollback save)
C. Call term.reset() on mobile before each snapshot write
D. Use a separate "snapshot renderer" component instead of xterm for cmux

Long-term design:
```
tmux/localpty → xterm byte stream renderer
cmux         → snapshot renderer (replace, not append)
             → transcript delta renderer
```

## Verification Commands

```sh
# Check snapshot content for ESC[2J prefix
curl -s "http://localhost:9171/api/sessions?activity=cmux:surface:2" | python3 -c "
import json, sys
events = json.load(sys.stdin)
for e in events:
    t = e.get('text','')
    print(f'has_ESC2J: {repr(chr(0x1b)+\"[2J\") in t}')
"

# Run regression tests
go test ./internal/mux -run TestForceFlush_Overcommit -count=1 -v
go test ./internal/mux ./internal/term -count=1
```
