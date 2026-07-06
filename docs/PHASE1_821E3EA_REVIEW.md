# Phase 1 커밋 `821e3ea` 검증 결과

- 검증일: 2026-07-06
- 대상 커밋: `821e3ea`
- 판정: **REJECT**
- 다음 Phase 진행: **금지**
- 상세 수정 가이드: `docs/PHASE1_821E3EA_FINAL_FIX_GUIDE.md`

## 검증 요약

통과:

- CRUD가 Registry mutation API를 호출하도록 변경
- `RegistryFromContext`가 error를 반환하도록 변경
- `go vet ./...`
- `go test ./...`
- `go test -race ./...`

실패:

- Registry context error를 여러 caller가 `_`로 무시
- 안전 검사 전에 nil Registry를 역참조하는 CRUD 임시 코드
- WebSocket/History/Links context 누락 panic 가능
- IPC 시작 오류 빈 block 유지
- acceptance test 누락
- gofmt 미적용
- trailing whitespace
- generated binary diff 유지

## 결정적 코드 문제

다음 코드는 오류 처리가 아니다.

```go
reg, _ := RegistryFromContext(r.Context())
```

`RegistryFromContext`가 error를 반환하도록 바꿨더라도 caller가 무시하면 기존 nil
panic 위험은 그대로다.

CRUD에는 안전 검사 전 다음 코드가 존재한다.

```go
reg, _ := RegistryFromContext(r.Context())
_, _ = reg.Adapter(adapterName)
```

context에 Registry가 없으면 다음 줄에서 panic이 발생해 이후 `regErr` 처리는
실행되지 않는다.

## 자동 검사

```text
go vet: PASS
go test: PASS
go test -race: PASS
full diff check: FAIL
generated binary diff: 존재
gofmt diff: 존재
```

전체 diff 오류:

```text
cmux_adapter_mock_test.go:111 trailing whitespace
cmux_adapter_mock_test.go:164 trailing whitespace
cmux_adapter_mock_test.go:219 trailing whitespace
```

## 판정

```text
Architecture: PASS
Global ownership: PASS
Registry mutation API existence: PASS
Registry mutation API wiring: PARTIAL
Missing-context safety: FAIL
Acceptance tests: FAIL
Formatting/full diff: FAIL
Decision: REJECT
```

남은 작업은 새 설계가 아니라 안전 처리와 테스트·formatting 마무리다. 실행
에이전트는 `PHASE1_821E3EA_FINAL_FIX_GUIDE.md`를 순서대로 수행한다.
