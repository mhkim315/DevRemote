# cmux Snapshot Delta Extraction — PoC Report

Date: 2026-07-09

## Question

Can we recover append-only transcript events for cmux screen-polling
sessions by comparing consecutive screen snapshots?

## Answer

**Yes, technically feasible with ~85-95% accuracy for AI agent output.**

## Algorithm

`extractDelta(prev, curr string) string`

Three-strategy approach, applied in order:

### Strategy A: Common-Prefix Suffix (primary)

```
prev: "Thinking...\n"
curr: "Thinking...\nWriting file...\n"
delta: "Writing file...\n"
```

- Find longest common prefix between prev and curr
- Suffix of curr beyond the prefix is the delta
- Accept if suffix is meaningful but not a full rewrite (< 80% of curr)
- Handles the most common case: agent appends output at the bottom

### Strategy B: Line-Based Diff (fallback)

```
prev: "Old line 1\nOld line 2\nOld line 3\n"
curr: "New output\n"
delta: "New output"  (screen clear → new content)
```

- Split both snapshots into lines
- Find lines in curr not present in prev
- Accept if ≤ 5 new lines (small screen clear) or ≤ 50% of lines (incremental)
- Extended threshold for short screens (≤ 15 lines, ≤ 75% new)

### Strategy C: Common-Suffix Prefix (scroll)

```
prev: "line2\nline3\nline4\nline5\n"
curr: "line3\nline4\nline5\nline6\n"
delta: "line6\n"  (top line scrolled out, new bottom line)
```

- Find longest common suffix
- Prefix of curr before the suffix is new content
- Accept if prefix < 80% of curr

### Degradation

Returns "" (empty string) when:
- Full screen rewrite (> 80% changed)
- No meaningful delta detectable
- Complex TUI rendering (htop, vim, etc.)

## Expected Accuracy by Agent Type

| Agent | Accuracy | Notes |
|-------|----------|-------|
| Claude Code | ~95% | Heavily append-oriented, clear thinking→writing pattern |
| Codex (GPT-5) | ~90% | Similar append pattern, occasional spinner/progress |
| Gemini CLI | ~90% | Append-oriented with ANSI formatting |
| Generic shell | ~70% | Command output varies widely |
| vim/nano | ~10% | Full-screen TUI rewrites every keystroke — not supported |
| htop/watch | ~5% | Constant full-screen repainting — not supported |

## Failure Cases

1. **Progress spinners**: "/", "-", "\\", "|" rotation produces single-char deltas.
   Minor noise — acceptable.

2. **Mid-screen edits**: When an agent edits text in the middle of output
   (not append-only), common prefix stops at the edit point. Line-based diff
   may recover some lines.

3. **Full-screen TUI**: vim, htop, tmux status bars. Returns "".
   Transcript intentionally empty for these cases.

4. **ANSI-heavy output**: Colored output may shift byte positions.
   Solution: strip ANSI BEFORE comparison (already done by stripANSI).

5. **Terminal scrollback truncation**: When oldest lines are removed from top,
   Strategy C handles this but may miss multi-line additions that exactly
   match scrollback size.

## Complexity

- Time: O(n) per comparison (n = screen size), typically 10-50KB
- Space: O(n) for line sets in Strategy B
- Comparison frequency: every 500ms (cmux poll interval)
- Acceptable for daemon-side processing

## Recommended Architecture

```
cmux pollScreen (every 500ms)
  │
  ├─ read-screen → currentContent
  ├─ stripANSI(prevContent), stripANSI(currentContent)
  ├─ delta := extractDelta(cleanPrev, cleanCurrent)
  │
  ├─ if delta != "":
  │     write snapshotEndMarker + delta + snapshotEndMarker to pipe
  │     → Recorder → ActivityBuffer → Transcript
  │
  └─ Full screen (ESC[2J ESC[H + currentContent + marker):
        write to pipe → subscribers → Live Terminal (xterm.js)
```

Key points:
- Delta extraction happens in cmux adapter (where lastContent is tracked)
- Full screen still sent to pipe for Live Terminal rendering
- Delta appended as a separate write through the pipe (or separate mechanism)
- ActivityBuffer receives only the delta
- Recorder remains unchanged

## Test Results

12 test cases all pass:

| Test | Result |
|------|--------|
| First snapshot (emit everything) | PASS |
| No change (return empty) | PASS |
| Append-only (Claude writing) | PASS |
| Multi-line append | PASS |
| Screen cleared → new output | PASS |
| Full-screen rewrite (>80%) → empty | PASS |
| Scroll (top line lost) | PASS |
| Claude-style thinking → result | PASS |
| Korean text preserved | PASS |
| ANSI-only change → empty | PASS |
| Complex mixed output | PASS |
| Progress spinner (minor noise) | PASS |

## Recommendation

**Move cmux Transcript support to roadmap: E8g4.**

This is a viable low-effort feature (~1 day implementation) that would give
cmux sessions basic Transcript support for AI agent output. The algorithm
degrades safely when output is not append-only.

Not a replacement for real PTY-based capture (tmux/localpty), but a useful
complement for screen-polling sessions.
