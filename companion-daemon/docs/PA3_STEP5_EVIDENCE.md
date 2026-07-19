# PA3 Step 5 Evidence — Mobile DTO update

Status: **EVIDENCE**
Date: 2026-07-19
Implementation SHA: `9e72a421534527a342ffe4ad74264ece9bfdcda0`

## Changes

### TypeScript SessionTelemetry interface

- Added `profileId?: string`, `name?: string` (additive, optional)
- Removed `state`, `load`, `runner`, `runnerColor`, `events` fields (already commented as removed in Step 1)

### client.ts

- Removed `getActivityHistory()` and `getSessionHistory()` functions
- Legacy `?activity=/ ?history=` endpoints return 410 Gone since Step 3

### TranscriptRenderer

- Already handles `TranscriptResponse` including `generation` field (Step 1)

## Gate results

```sh
$ cd mobile && ./node_modules/.bin/tsc --noEmit
(no output — success)

$ cd companion-daemon && go test -race ./internal/term ./cmd/devremote -count=1
ok  	devremote/companion-daemon/internal/term	16.561s
ok  	devremote/companion-daemon/cmd/devremote	33.304s
```

Zero backend diff.
