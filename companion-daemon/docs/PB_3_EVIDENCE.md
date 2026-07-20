# PB.3 Evidence — Tmux Adapter Removal

**PB.3 IMPL SHA:** `93c31b0f0`
**PA4 ACCEPT SHA:** `74560edd`

## Consumer Inventory

| Component | Status | Files |
|-----------|--------|-------|
| tmux adapter | DELETED | tmux_adapter.go + _test.go |
| tmux NewSession | DELETED | session.go |
| TrackCmuxPanels (used tmux) | DELETED | tracker.go |
| tmux registration | REMOVED | app.go |
| tmux integration test | DELETED | integration_test.go |

## Adapter:ID Grammar Preservation

All session ID parsing tests still pass with "cmux" as example external adapter name.
The generic `<adapter>:<local-id>` grammar is unchanged:
- Valid: `cmux:devremote`, `controlled_pty:shell`, `codex_app_server:abc`
- ID parsing: `ParseSessionID`, `SessionRef`, `Canonical()` all preserved
- Unicode/local-id-with-colons round-trip tests pass

## Zero Consumers Proof

### Control
```
$ printf "tmux" | grep -E "tmux|Tmux|TMUX"
tmux
(exit 0)
```

### Production
```
$ grep -rnE "tmux|Tmux|TMUX" --include='*.go' . | grep -v "_test.go"
(empty — zero)
```

### Tests
```
$ grep -rnE "tmux|Tmux|TMUX" --include='*_test.go' .
(empty — zero)
```

### Mobile
```
$ grep -rnE "tmux|Tmux|TMUX" ../mobile/src/
(empty — zero)
```

### Scripts
```
$ grep -rnE "tmux|Tmux|TMUX" ../scripts/ scripts/
(empty — zero)
```

## Gates
```
go build ./...                     exit 0
go vet ./...                       exit 0
gofmt -d .                         clean
go test -race ./... -count=1       ALL PASS (12 packages)
npx tsc --noEmit                    clean
npx jest                            451/451 pass, 34 suites
```
