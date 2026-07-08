# Next Session E8 Handoff

Read first:

- `docs/E8_EXECUTION_PLAN.md`
- `docs/E8_RUNTIME_RELIABILITY_BUG_REPORT.md`
- `docs/E8_INSTRUMENTATION_REPORT.md`
- `docs/E8_MOBILE_CONNECTIVITY_QUESTION.md`

## Current priority

Do not continue terminal duplication fixing yet.

Immediate target:

```text
E8a — Connection State Correctness
```

## Mission

Fix the product state ambiguity where a broken daemon connection appears as an
empty Dashboard with only `NEW AGENT`.

Current bad flow:

```text
Stored BASE_URL
→ isConnected=true
→ Dashboard
→ listSessions fails
→ sessions=[]
→ user sees NEW AGENT only
```

Required result:

```text
connection failure ≠ no sessions
```

## Implement only E8a

Allowed:

- verify restored `BASE_URL` before marking connected;
- verify manual/QR connect before saving `BASE_URL`;
- show connection error on ConnectScreen;
- show Dashboard fetch error instead of empty sessions;
- add retry/rescan path;
- add tests or clear runtime evidence.

Avoid:

- new tunnel manager;
- new daemon bind flag;
- changing `--insecure-local-only`;
- onboarding redesign;
- terminal duplication fix acceptance;
- mobile WebView E8DIAG acceptance.

## Expected validation

Run:

```sh
sh scripts/build-gate.sh
```

Also provide evidence for:

1. unreachable saved URL does not mark app connected;
2. unreachable manual URL shows connection error;
3. successful but empty `/api/sessions` remains distinguishable from fetch failure;
4. successful `/api/sessions` with sessions renders sessions.

## Completion statement

Use this format:

```text
E8a implementation complete.

Commit: <sha>

Changed:
- ...

Validation:
- build gate: ...
- unreachable saved URL: ...
- unreachable manual URL: ...
- empty sessions vs failed fetch: ...

Not included:
- Mobile WebView E8DIAG validation
- terminal duplication final acceptance
- tunnel manager
- new daemon bind flag
```

