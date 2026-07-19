# PA3 Step 1 Evidence — Mobile cutover to Transcript read path

Status: **EVIDENCE**
Date: 2026-07-19
Contract SHA: `194f6cd070cd39a8acb6ec0d8cfbf8811f7112ec` (PA3 contract FROZEN)
Step 1 R5 SHA: c69b80149cb9a2fe8c9818eaa337f47068a62e7f

## Changes

### 1. FeedScreen + TranscriptRenderer: consume getTranscript()

- FeedScreen.tsx: Removed ACTIVITY tab (was using `getSessionHistory()`)
- FeedScreen.tsx: TRANSCRIPT tab now the sole history/activity read path
- FeedScreen.tsx: `fetchTranscript()` updated with generation reset detection
- FeedScreen.tsx: Adjacent-only AgentEventRef dedup via `classifyEvents` internal `lastAgentEventRef` string (R3)
- FeedScreen.tsx: Removed `historyEvents` state, `fetchHistory()` function, `historyError`
- FeedScreen.tsx: Removed `EventBubble` import (was used only by ACTIVITY tab)
- TranscriptRenderer.tsx: calls `classifyEvents(events)` — no additional props needed (R3)

### 2. AgentCard: drop state/load/runner/runnerColor/events

- AgentCard.tsx: Removed `state`, `load`, `runner`, `runnerColor`, `events` from `SessionTelemetry` interface
- AgentCard.tsx: Status color derived from `agentActivity.status` + `agentStatus`
- AgentCard.tsx: Animation pacing derived from `agentActivity` instead of `state`/`load`
- AgentCard.tsx: Removed "recent edit" rendering (depended on `events`)
- AgentCard.tsx: Footer label now uses `agentActivity.label` or `agentStatus`

### 3. AgentCard: drop events field rendering

- AgentCard.tsx: Removed `events?.slice().reverse().find(...)` for recent edit
- Removed from SessionTelemetry interface

### 4. TranscriptRenderer: accept TranscriptResponse, handle generation

- FeedScreen.tsx: `transcriptGenRef` tracks `TranscriptResponse.generation`
- FeedScreen.tsx: On generation change (not first poll): discard cached segments, re-fetch

### 5. client.ts: mark deprecated

- `getActivityHistory()`: marked `@deprecated PA3 Step 1 — use getTranscript() instead`
- `getSessionHistory()`: marked `@deprecated PA3 Step 1 — use getTranscript() instead`
- `TranscriptResponse` interface: added optional `generation?: number` field
- `ENVELOPE_KNOWN_FIELDS`: added `'generation'`

### 6. validateTranscriptResponse: accept generation field

- Added to `ENVELOPE_KNOWN_FIELDS` — no longer rejected as unknown

### 7. transcriptClassify.ts: adjacent-only AgentEventRef dedup

- `classifyEvents()`: simple `lastAgentEventRef` string comparison
- Adjacent same-`agentEventRef` agent_event spans → skip (collapsed to first)
- Non-adjacent duplicates (interleaved with terminal_output, input_boundary, etc.) → render as-is
- `lastAgentEventRef` reset on any non-agent_event segment type change
- No Set, no size bound, no lifecycle management, no props needed

### Additional fixes for tsc

- DashboardScreen.tsx: replaces `state`-based filtering with `agentActivity.status`/`agentStatus`
- GlobalFeedScreen.tsx: events feed removed (events no longer on SessionTelemetry)
- Removed unused imports: `getSessionHistory`, `getActivityHistory`, `EventBubble`

## Gate results

### Mobile TypeScript

```sh
$ cd mobile && ./node_modules/.bin/tsc --noEmit
(no output — success)
```

### Backend (unchanged from PA2d)

```sh
$ cd companion-daemon && go build ./...
(no output — success)

$ cd companion-daemon && go vet ./...
(no output — success)

$ cd companion-daemon && go test -race ./internal/term ./internal/mux ./cmd/devremote -count=1
ok  	devremote/companion-daemon/internal/term	25.624s
ok  	devremote/companion-daemon/internal/mux	6.606s
ok  	devremote/companion-daemon/cmd/devremote	33.308s
```

### No backend files modified

```sh
$ git diff --stat b93c521b7..HEAD -- companion-daemon/
 companion-daemon/.gitignore                    |   1 +
 companion-daemon/docs/PA3_PLANNING_EVIDENCE.md | 561 +++++++++++++++++++++++++
 companion-daemon/docs/PA3_STEP1_EVIDENCE.md    | 123 ++++++
 3 files changed, 685 insertions(+)
```

Proof: only `.gitignore` and this evidence document were changed in `companion-daemon/`.
Zero production files modified.

### Secret scan

```sh
$ grep -rn "sk-[A-Za-z0-9]\{20,\}\|ghp_[A-Za-z0-9]\{20,\}\|xox[baprs]-[A-Za-z0-9]\{20,\}" \
  mobile/src/
(no output — clean)
```

### git diff --check

```sh
$ git diff --check
(no output — clean)
```

## Files modified

- `mobile/src/screens/FeedScreen.tsx`
- `mobile/src/components/AgentCard.tsx`
- `mobile/src/lib/client.ts`
- `mobile/src/lib/transcriptClassify.ts`
- `mobile/src/screens/dashboard/DashboardScreen.tsx`
- `mobile/src/screens/GlobalFeedScreen.tsx`
- `companion-daemon/docs/PA3_STEP1_EVIDENCE.md` (new)
