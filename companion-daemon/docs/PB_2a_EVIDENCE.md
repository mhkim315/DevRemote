# PB.2a Evidence — Manual Link and Arbitrary Attach Removal

**PB.2a IMPL SHA:** `de2545d9c`
**PA4 ACCEPT SHA:** `74560edd`

## Consumer Map + Deletion Order

| Order | Component | Location | Action |
|-------|-----------|----------|--------|
| 1 | ManualLink agent checks | antigravity/codex/claude_adapter.go | DELETED |
| 2 | SourceManualLink constant | internal/agent/models.go | DELETED |
| 3 | ManualEvidence struct | internal/agent/detector.go | DELETED |
| 4 | LinkedLogResolver + ResolveLink | internal/term/resolver.go | DELETED |
| 5 | Detect_ManualOverride test | detector_contract_test.go | DELETED |
| R | AgentLogResolver + log resolvers | resolver.go, *_resolver.go | RETAINED |
| R | Managed attach (Codex IPC) | managed_attach.go, managed_codex.go | RETAINED |

## Zero Consumers Proof

```
$ grep -rnE "ManualLink|ManualEvidence|SourceManualLink|LinkedLogResolver|ResolveLink"     --include='*.go' . | grep -v "_test.go" | grep -v "testdata"
(empty — zero production references)

$ printf "ManualEvidence" | grep -E "ManualLink|ManualEvidence"
ManualEvidence
(exit 0 — pattern valid)
```

## 404 Route Test

```
=== RUN   TestPB2a_LinkAttachRoutesReturn404
--- PASS: TestPB2a_LinkAttachRoutesReturn404 (0.00s)
```

All 5 routes return exactly 404:
- GET /api/v2/links → 404
- POST /api/v2/links → 404
- DELETE /api/v2/links/some-uuid → 404
- POST /api/link → 404
- POST /api/attach → 404

## Managed Path Preservation

```
=== RUN   TestPB2a_ManagedPathsUnaffectedByLinkRemoval
--- PASS (catalog, lifecycle, approval, transport all intact)
```

## Gates

```
go build ./...                                    exit 0
go vet ./...                                      exit 0
gofmt -d (changed files)                          clean
git diff --check                                  exit 0
go test -race ./... -count=1                      ALL PASS (12 packages)
go test -race ./internal/term -run "TestPA4_" -count=20  PASS
cd mobile && npx tsc --noEmit                     clean
cd mobile && npx jest                             451/451 pass, 34 suites
```
