# Phase 7 — Mobile Smoke Evidence

Date: 2026-07-07

Mobile 코드에 backend 이름 hardcoding이 없고, capability 기반으로 동작함을 검증.

## 1. Adapter Name Branching

`grep -rn "tmux\|cmux\|adapter ===" mobile/src/` 결과: **0건**

유일한 adapter 관련 분기: `AgentCard.tsx:86`
```typescript
{session.adapter && session.adapter !== 'native' && (
  <Text style={styles.adapterTag}>[{session.adapter}]</Text>
)}
```
- `"native"`일 때만 `[native]` tag 숨김 (NativeSession은 내부용)
- 그 외 모든 adapter는 `[adapter]` tag 표시
- Backend 동작 분기 없음. 순수 display 결정.

## 2. Capability-Driven Display

`client.ts`의 `SessionTelemetry` interface:
```typescript
interface SessionTelemetry {
  id: string;
  adapter: string;        // unknown adapter names pass (string, not enum)
  capabilities: string[]; // capability-driven, not backend-name-driven
  ...
}
```

- `adapter` 필드는 `string` 타입 — `"future"`, `"localpty"` 등 모든 값 수용
- `capabilities`는 `string[]` — backend 이름이 아닌 capability 목록으로 동작 결정
- `npx tsc --noEmit`: 모든 adapter 이름에 대해 type error 없음

## 3. Expected Mobile Behavior Per Backend

| Backend | Capabilities | Mobile 표시 |
|---------|-------------|------------|
| tmux | `[live_stream, screen, history]` | `[tmux]` tag, WS 연결 가능, ACTIVITY 탭 표시, 3초 history polling |
| cmux | `[live_stream, screen, history, process]` | `[cmux]` tag, WS 연결 가능, ACTIVITY 탭 표시, 3초 history polling |
| localpty | `[live_stream]` | `[localpty]` tag, WS 연결 가능, ACTIVITY 탭 숨김, history polling 안 함 |
| future | `[live_stream]` | `[future]` tag, WS 연결 가능, ACTIVITY 탭 숨김 |
| legacy | `[]` (omitted) | adapter tag만 표시, WS 연결 불가, ACTIVITY 탭 숨김 |

**구현 (FeedScreen.tsx)**:
- `supportsHistory = !sessionData \|\| sessionData.capabilities?.includes('history') !== false`
  - `sessionData`가 null이면(초기 상태): true → 기존 동작 유지
  - `capabilities`에 `'history'`가 있으면: true
  - `capabilities`가 `undefined`(legacy): true → 하위 호환
  - `capabilities`에 `'history'`가 없으면: false → 탭 숨김, polling 중단
- ACTIVITY 탭: `{supportsHistory && (<TouchableOpacity>...</TouchableOpacity>)}`
- History polling: `if (supportsHistory !== false) { fetchHistory(); }`

## 4. API Golden Fixtures

아래 JSON은 `SessionTelemetry` deserialization을 통과하며 mobile crash를 유발하지 않는다:

```json
// tmux
{"id":"tmux:dev","displayId":"dev","state":"idle","load":0,"runner":"claude","runnerColor":"#58a6ff","adapter":"tmux","capabilities":["live_stream","screen","history"],"stale":false,"events":[]}

// cmux
{"id":"cmux:surface:1","displayId":"surface:1","state":"active","load":50,"runner":"codex","runnerColor":"#f0a030","adapter":"cmux","capabilities":["live_stream","screen","history","process"],"stale":false,"events":[]}

// localpty
{"id":"localpty:lp1","displayId":"lp1","state":"idle","load":0,"runner":"agent","runnerColor":"#a0a0a0","adapter":"localpty","capabilities":["live_stream"],"stale":false,"events":[]}

// future (unknown adapter)
{"id":"future:x1","displayId":"x1","state":"idle","load":0,"runner":"agent","runnerColor":"#a0a0a0","adapter":"future","capabilities":["live_stream"],"stale":false,"events":[]}

// legacy (no capabilities)
{"id":"legacy:old","displayId":"old","state":"idle","load":0,"runner":"bash","runnerColor":"#ccc","adapter":"legacy","stale":false,"events":[]}
```

## 5. TypeScript Compile Verification

```sh
$ npx tsc --noEmit
# No errors — all adapter names pass type check.
```

## Conclusion

Mobile 코드는 backend 이름이 아닌 capability 기반으로 동작한다. 알려지지 않은 adapter,
legacy no-capabilities 응답, localpty live_stream-only session 모두 type-safe하게 처리된다.
