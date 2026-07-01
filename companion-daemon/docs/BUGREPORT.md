# DevRemote Bug Report — Phase 3 연결 테스트

> 2026-07-01. 원격 WebRTC 연결 엔드투엔드 테스트 과정에서 발견된 버그 및 해결 기록.

## BUG-001: Android WebSocket SSL 인증서 신뢰 실패

**증상**: `wss://api.fullcount.kr/signal/` 연결 시 "signaling timeout"

**원인**: React Native Android의 내장 `WebSocket`(OkHttp 기반)이 ZeroSSL ECC 인증서를 신뢰하지 않음. 모바일 브라우저는 정상 접속됨.

**시도한 해결책 (실패)**:
- `usesCleartextTraffic: true` → 효과 없음 (cleartext 문제가 아니라 SSL trust 문제)
- nginx `http2` 제거 → 효과 없음
- `ws://` 80포트 사용 → 통신사 차단 의심

**최종 해결**: WebSocket 시그널링을 HTTP REST + `fetch()` 로 전면 교체.
- `signald`를 WebSocket 서버에서 HTTP REST 서버로 재작성 (`POST /join`, `POST /send`, `GET /poll`)
- 모바일 통신을 `fetch()` 기반으로 변경 (Baseball 앱이 1년간 검증한 방식)
- 시그널링만 HTTP, 실데이터는 WebRTC P2P 데이터채널로 직통

**커밋**: `5ccc2583b` feat: replace WebSocket signaling with HTTP REST polling

---

## BUG-002: 세션 키 충돌 (Key Mismatch)

**증상**: 모바일 join은 성공했으나 데몬의 SDP offer를 수신하지 못함

**원인**: 
- Daemon: `code="125308"`로 세션 생성 시 signald가 key `A` 발급
- Mobile: AsyncStorage에 저장된 이전 세션 key `B`로 join → key `B`의 다른 세션에 접속
- Daemon과 Mobile이 서로 다른 세션에 존재 → 메시지 교환 불가

**해결**:
- `signald`: 코드(code)가 항상 키(key)보다 우선하도록 수정
- `WebRTCTransport.ts`: 저장된 키 무시하고 항상 코드로 join

**커밋**: `50ccc0d86` fix: code-priority join + poll-before-offer

---

## BUG-003: ICE/SDP 처리 순서 오류

**증상**: 모바일이 "연결 중"에서 진행되지 않음. 데몬의 SDP offer는 signald에 존재하나 모바일이 answer를 보내지 않음.

**원인**: 데몬이 ICE candidate들을 SDP offer보다 먼저 전송 (seq 3-7: ICE, seq 8: SDP). 모바일이 `addIceCandidate()`를 `setRemoteDescription()`보다 먼저 호출 → WebRTC API가 ICE 후보를 드롭.

**해결**: 모바일 poll 루프에서 SDP 메시지를 ICE보다 먼저 처리하도록 정렬.

**커밋**: `bf77b6cd4` fix: process SDP before ICE candidates in polling

---

## BUG-004: ICE Gathering 타임아웃

**증상**: 데몬 시작 후 `GatheringCompletePromise`에서 장시간 blocking → 모바일이 먼저 join해도 데몬이 응답하지 않음

**원인**: 다수의 네트워크 인터페이스(APIPA 169.254.x.x 5개)로 인해 ICE candidate 수집이 지연. 기본 타임아웃이 너무 김.

**해결**:
- ICE gathering에 15초 타임아웃 적용
- 데몬의 pollLoop를 offer 생성 전에 먼저 시작하여 모바일 응답 수신 준비

**커밋**: `50ccc0d86` fix: code-priority join + poll-before-offer

---

## BUG-005: Runtime ReferenceError — undefined peerKey

**증상**: 앱 실행 시 "property peer key doesn't exist" 오류

**원인**: AsyncStorage 키 로딩 코드 제거 과정에서 `peerKey` 변수 참조가 남아있음.
```typescript
// 삭제됨: const peerKey = await AsyncStorage.getItem(...)
// 남아있음: if (joinData.key && !peerKey) { ... }  ← ReferenceError!
```

**해결**: `peerKey` 참조 제거, 미사용 import 정리

**커밋**: `3c4a0875c` fix: remove unused peerKey reference causing runtime error

---

## BUG-006: pollLoop 시작 타이밍

**증상**: 데몬이 SDP offer 전송 후에도 모바일 answer를 수신하지 못함

**원인**: `pollLoop`가 `Start()` 함수 마지막에 시작되어, ICE gathering 중에는 polling이 동작하지 않음

**해결**: `pollLoop`를 offer 생성 전에 먼저 시작 (`go s.pollLoop(onResponse)` 위치 이동)

**커밋**: `50ccc0d86` fix: code-priority join + poll-before-offer

---

## BUG-007: nginx /signal/ 경로 누락

**증상**: `wss://api.fullcount.kr/signal/` 접속 시 404 또는 Baseball API로 라우팅

**원인**: nginx 443 포트 server 블록에 `/signal/` location이 누락됨. 초기 배포 시 port 80 블록에만 추가됨.

**해결**: 443 SSL server 블록에도 `/signal/` location 추가, `http2` 제거

---

## 에러 메시지별 진단 가이드

| 메시지 | 의미 | 해결책 |
|---|---|---|
| "signaling timeout" | WebSocket `onopen`이 10초 내 미발생 | HTTP fetch 방식으로 전환 |
| "signaling connect failed" | WebSocket `onerror` 발생 | SSL 인증서/네트워크 확인 |
| "연결 중..." 지속 | 시그널링 성공했으나 P2P 핸드셰이크 미완료 | BUG-003 참고 |
| "끊김" | WebSocket 연결 후 즉시 종료 | 네트워크/방화벽 확인 |
| "property peer key doesn't exist" | 코드 참조 오류 | BUG-005 참고 |

---

## 아키텍처 변경 요약

```
변경 전:  WebSocket(wss://, ws://) → SSL 문제, 캐리어 차단
변경 후:  fetch() + https:// → 모든 네트워크에서 안정적 연결

변경 전:  signald = WebSocket 전용 서버
변경 후:  signald = HTTP REST 서버 (POST /join, /send, GET /poll)

변경 전:  Persistent key 기반 세션 복구
변경 후:  6-digit code 기반 세션 (매번 새 코드)
```
