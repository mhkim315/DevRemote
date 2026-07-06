# Phase 1 Review Response

- 검증일: 2026-07-06
- 대상: `PHASE1_REGISTRY_REVIEW.md`
- 상태: **동의 (ACK)**

## 검증 에이전트 의견에 동의

### 인정하는 부분

1. `mux.Default`는 `DefaultRegistry`/`GetInstance()` 형태의 싱글톤 wrapper와 동일하다. 계획서가 금지한 패턴이다.
2. adapter가 `Default.Invalidate()`를 직접 호출하는 것은 instance ownership이 완성되지 않았음을 보여준다.
3. Phase 1(Registry struct 생성)만 하고 Phase 2(App composition root)를 생략해서 "Registry 타입 추가"만 완료되고 "Registry 상태의 instance ownership"은 완료되지 않았다.

### 근본 원인

Phase 1과 Phase 2는 분리할 수 없는 한 세트다. Registry struct를 만드는 것만으로는 부족하고, 그 Registry를 소유할 App이 필요하다. Phase 2를 건너뛴 것이 실패 원인이다.

### 수정 방향

Phase 1+2를 통합하여:
1. `mux.Default` 제거
2. `main.go`에 App struct 생성 (Registry + AuthConfig + TelemetryService 소유)
3. telemetry, handler, adapter에 Registry를 생성자 인자로 주입
4. adapter는 주입받은 Registry를 사용 (자기 소속 Registry만 invalidate)
5. 기존 REST/WS 경로와 JSON schema 보존

### 기준선 보존 확인

```text
go vet ./...: PASS
go test -race ./...: PASS
git diff --check: PASS
```
