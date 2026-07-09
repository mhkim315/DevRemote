# E8g4 Force-Flush Overcommit Analysis

Date: 2026-07-09
Commit: 5c74e3442 → fix applied in subsequent commit

## Observed Failure

After daemon restart, cmux:surface:2 activity showed:
- 911 total transcript lines
- Only 37 unique lines
- 95.9% duplication

## Root Cause

**force-flush recommitted the entire screen on every trigger.**

Flow:
1. Daemon starts → cmux screen polled
2. Bootstrap: commits last 200 non-volatile lines from initial screen
3. Screen idle → stability counter increments
4. forceFlushPolls=6 reached → force-flush triggers
5. Force-flush iterates ALL currLines, joins non-volatile ones,
   checks isCommitted (20-line window), and commits
6. Since committed window is 20 lines but screen has 8000+ lines,
   lines outside the window pass isCommitted and get recommitted
7. Next poll (still identical screen) → stability increments again
8. forceFlushPolls reached AGAIN → force-flush triggers AGAIN
9. Same lines recommitted AGAIN
10. Loop: each force-flush cycle recommits ≈8000 lines, but
    isCommitted window prevents only 20 from being duplicates

Result: 37 unique lines × 26 recommits = 911 total lines

## Fix

`forceFlushed` boolean on screenTracker:
- Set to true when force-flush fires
- Reset to false when screen changes (prefixLen < len)
- Force-flush only fires if !forceFlushed

This ensures force-flush commits at most once per stable screen period.

## Snapshot Sequence

```
P0: bootstrap → commit last 200 lines
P1-P5: identical screen → stableCount 1-5
P6: stableCount=6 → force-flush (first time) → commit all non-volatile lines
P7-P11: identical screen → stableCount 1-5, forceFlushed=true → skip
P12: stableCount=6, forceFlushed=true → SKIP (no recommit!) ✅
P13+: same, no recommit ✅
```

## What is a "screen" for force-flush?

Defined by: content change detection.
- `forceFlushed` resets when `prefixLen < len(s.prevLines) || prefixLen < len(currLines)`
- i.e., prev and curr screens differ in any way (any line changed, added, or removed)
- This is precise and unambiguous — no hash, frame ID, or marker window needed

## What was NOT the cause

- ❌ Semantic line reclassification (lines are stable)
- ❌ Volatile UI leaking (filtered correctly)
- ❌ Global dedup blocking legitimate repeats (marker passthrough works)
- ❌ Stability window reset by cursor/timer (tolerated bottom 3 lines)

## Regression Test

`TestForceFlush_Overcommit`: 20 identical polls → asserts <50% duplication
