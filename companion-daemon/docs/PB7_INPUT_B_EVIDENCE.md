# Input-B Evidence — Versioned Control-Request Protocol

**Input-B IMPL SHA:** `5f498f9f7`
**PA4 ACCEPT SHA:** `74560edd`
**PB Ancestry Baseline SHA:** `abe4df1d6`

## Ancestry

```
$ git merge-base --is-ancestor 74560edd8 HEAD && echo "PA4 ACCEPT: ANCESTOR OK"
$ git merge-base --is-ancestor abe4df1d6 HEAD && echo "PB BASELINE: ANCESTOR OK"
```

## Diff Scope

```
companion-daemon/internal/term/input_b_protocol.go     | 319 +++++ (new)
companion-daemon/internal/term/input_b_ack_test.go     | 173 +-
companion-daemon/internal/term/input_a_denial_test.go  |   4 +-
companion-daemon/internal/term/pty.go                  |  23 +-
companion-daemon/internal/term/terminal_transport.go   |   6 +-
 5 files changed, 451 insertions(+), 25 deletions(-)
```

## Gate Results

```
go build ./...                                    exit 0
go vet ./...                                      exit 0
gofmt -l .                                        0 files
go test -race ./... -count=1                       ALL PASS (11 packages)
cd mobile && npx tsc --noEmit                      clean
cd mobile && npx jest --runInBand                  474/474 pass, 35 suites
```

## Protocol Tests

```
TestInputA_DenialViaHandleWS                       PASS
TestInputB_HandleWSAcknowledgesExactGeneration...  PASS
TestInputB_HandleWSWriteFailureDoesNotAcknowledge  PASS
TestInputB_DuplicateInputIDReplaysCached           PASS
TestInputB_InputIDConflictDifferentPayload         PASS
```

## Requirement Trace (§6)

| Requirement | Status |
|---|---|
| Versioned control request (TextMessage JSON) | IMPL |
| Closed result vocabulary (8 outcomes) | IMPL + tested |
| Legacy BinaryMessage → local_dev only | IMPL |
| Paired production raw binary rejected | IMPL |
| request/result bound to session+gen+inputId | IMPL |
| 64-entry/30s recent-result cache | IMPL |
| inputId digest conflict detection | IMPL + tested |
| Duplicate replay without re-write | IMPL + tested |
| 8192/4096 byte limits | IMPL |
| inputId 64-char lowercase hex validation | IMPL |
| Atomic WriteInput (lock through Write) | IMPL |
| Retire/write race protection | IMPL |
