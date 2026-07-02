# DevRemote — 도움 답변 (2026-07-02)

`HELP_WANTED.md`에 기술해주신 3가지 모바일 UX 버그에 대한 상세한 원인 분석 및 해결 코드를 전달해 드립니다. 세 가지 버그 모두 프론트엔드(`WebRTCTransport.ts` 및 React 생명주기)의 상태 관리 맹점들로 인해 발생한 문제입니다.

---

## 🛠 BUG 1: 피드(Feed) 탭 갔다가 뒤로가면 알림(Alert) 탭이 끊기는 현상

### 원인
현재 `WebRTCTransport.ts` 클래스는 `onAlert`와 `onStatusChange` 호출 시 단일 콜백(Single Listener) 함수 하나만 저장하도록 되어 있습니다. (`this.alertHandler = handler;`)
따라서 FeedScreen으로 넘어가면 SessionScreen의 기존 리스너가 덮어씌워져 버리고, 뒤로가기를 눌러 다시 SessionScreen으로 돌아왔을 때는 (SessionScreen이 언마운트된 적이 없다면) 리스너를 다시 등록하지 않으므로 영영 이벤트를 받지 못하게 됩니다.

### 해결책 (다중 리스너 패턴 적용)
`WebRTCTransport.ts`를 배열이나 Set을 이용한 다중 리스너 방식으로 리팩토링하고, `useEffect` cleanup에서 이탈 시 리스너를 해제하도록 만들어야 합니다.

**`src/services/WebRTCTransport.ts` 수정:**
```typescript
// 1. 핸들러를 Set으로 변경
private alertHandlers = new Set<(alert: Alert) => void>();
private statusHandlers = new Set<(s: TransportStatus) => void>();

// 2. 등록 시 cleanup 함수를 반환하도록 수정
onStatusChange(handler: (status: TransportStatus) => void): () => void {
  this.statusHandlers.add(handler);
  return () => this.statusHandlers.delete(handler);
}

onAlert(handler: (alert: Alert) => void): () => void {
  this.alertHandlers.add(handler);
  return () => this.alertHandlers.delete(handler);
}

// 3. 내부에서 이벤트 발생 시 Set 순회 호출로 변경 (dc.onmessage 등 내부 구현)
this.alertHandlers.forEach(h => h(alert));
this.statusHandlers.forEach(h => h(this.status));
```

**`src/screens/SessionScreen.tsx` 및 `FeedScreen.tsx` 수정:**
```tsx
useEffect(() => {
  const unsubStatus = transport.onStatusChange(setStatus);
  const unsubAlert = transport.onAlert((a: Alert) => {
    // ...
  });
  return () => {
    unsubStatus();
    unsubAlert();
  };
}, [transport]);
```

---

## 🛠 BUG 2: 피드에서 채팅 입력(stdin) 미전달 현상

### 원인
WebRTC DataChannel의 아주 흔한 비동기 타이밍 버그입니다. 
`this.pc.ondatachannel` 이벤트가 발생했을 때 채널이 **이미 `open` 상태**일 수 있습니다. 이 경우 `this.dc.onopen` 콜백은 영영 불리지 않게 되고, `sendMessage`로 보낸 여러분의 텍스트 메시지는 `messageQueue` 배열에만 영원히 갇혀 발송되지 못하게 됩니다.

### 해결책
`ondatachannel`을 수신하자마자 `readyState === 'open'`인지 즉시 체크하여 큐를 비우고 `connected` 상태로 만들어야 합니다.

**`src/services/WebRTCTransport.ts` 수정:**
```typescript
this.pc.ondatachannel = (event: RTCDataChannelEvent) => {
  this.dc = event.channel;

  const handleOpen = () => {
    this.status = 'connected';
    this.statusHandlers.forEach(h => h(this.status));
    // 큐에 갇힌 메시지 방출!
    for (const msg of this.messageQueue) {
      this.dc?.send(JSON.stringify(msg));
    }
    this.messageQueue = [];
  };

  this.dc.onmessage = e => { ... };
  this.dc.onopen = handleOpen;
  this.dc.onclose = () => { ... };

  // 핵심 수정 사항: 이미 open 상태라면 즉시 큐를 비운다.
  if (this.dc.readyState === 'open') {
    handleOpen();
  }
};
```

---

## 🛠 BUG 3: 알림 폭탄 (PTY 출력이 모두 알림 카드로 렌더링됨)

### 원인
현재 데몬(`main.go`)은 모든 PTY 청크를 `type: "raw"`(혹은 `pty`)로 모바일에 방송합니다. 피드 탭은 이 데이터를 터미널 창에 뿌리기 위해 당연히 필요합니다. 하지만 `SessionScreen`의 `onAlert` 로직은 들어오는 모든 이벤트를 무조건 알림 카드로 만들어 FlatList에 밀어넣고 있기 때문에 화면이 폭주합니다.

### 해결책
모바일의 `SessionScreen` 측에서 `type === 'raw'` 또는 `type === 'pty'`인 이벤트를 알림 목록에서 가볍게 필터링해주기만 하면 완벽하게 해결됩니다.

**`src/screens/SessionScreen.tsx` 수정:**
```tsx
const connect = useCallback(() => {
  const unsubAlert = transport.onAlert((a: Alert) => {
    // 핵심 수정 사항: 터미널 미러링용 raw 데이터는 알림 카드 생성 무시!
    if (a.type === 'raw' || a.type === 'pty') return;
    
    setAlerts(prev => [
      {id: `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`, ...a},
      ...prev.slice(0, 19),
    ]);
  });
  // ...
```

---

### 결론
위 세 가지 수정을 모바일 코드(`mobile/` 디렉토리 내의 TypeScript/React Native 코드)에 적용하고 빌드하시면, 피드 전환 시 끊김, 채팅 미전송, 알림 폭탄 문제가 깔끔하게 해결될 것입니다. 행운을 빕니다!
