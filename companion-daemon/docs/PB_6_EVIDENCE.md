# PB.6 Evidence — Daemon-Wire and Mobile Cleanup

**PB.6 IMPL SHA:** `a366b65cc`
**PA4 ACCEPT SHA:** `74560edd`

## Inventory

All legacy adapter references (tmux, cmux, localpty) already removed
by previous PB waves (PB.1 localpty, PB.3 tmux, PB.4 cmux).
PB.6 confirms zero remaining references across all surfaces.

## Zero Consumers Proof

### Control


### Production Go
```
$ grep -rnE "tmux|cmux|localpty|LocalPTY" --include='*.go' . | grep -v "_test.go"
(empty — zero)
```

### Test Go
```
$ grep -rnE "tmux|cmux|localpty|LocalPTY" --include='*_test.go' .
(empty — zero)
```

### Mobile TypeScript
```
$ grep -rn "tmux|cmux|localpty" ../mobile/src/
(empty — zero)
```

### Mobile Tests
```
$ grep -rn "tmux|cmux|localpty" ../mobile/__tests__/
(empty — zero)
```

### Scripts/Packaging
```
$ grep -rn "tmux|cmux|localpty" ../scripts/ scripts/
(empty — zero)
```

## Retained

- Managed Codex/Claude provider-native evidence
- Controlled PTY lifecycle + TerminalTransport
- Transcript/replay
- Trimmed telemetry DTO
- Pairing and device authentication

## Gates

```
go build ./...                     exit 0
go vet ./...                       exit 0
gofmt -d .                         clean
go test -race ./... -count=1       ALL PASS (11 packages)
go test -race ./internal/term -run "TestPA4_" -count=20  PASS
cd mobile && npx tsc --noEmit      clean
cd mobile && npx jest              451/451 pass, 34 suites
```
