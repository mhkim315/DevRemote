import React, {useRef, useState, useCallback, useEffect, useMemo} from 'react';
import { terminalURL, listSessions, getTranscript, stopSession, killSession, deleteSessionHistory, ConnectivityFailure, PokitError } from '../lib/client';
import type { TranscriptSegment, TranscriptResponse } from '../lib/client';
import { E8g2Transcript } from '../components/TranscriptRenderer';
import { getWSTicket, wsTicketURL } from '../lib/wsTicket';
import type { TokenManager } from '../lib/authClient';
import { TerminalController, shouldIssueReconnect } from '../lib/terminalController';
import { deriveTerminalAuth } from '../lib/authMode';
import {
  readCapabilities, computeActionPolicy, reconcileState, isTerminalState,
  type ManagedLifecycleState, type PendingAction,
} from '../lib/lifecycle';
import { SessionLifecycleController } from '../lib/sessionLifecycleController';
import {View, Text, TextInput, StyleSheet, TouchableOpacity, ScrollView, Platform, Keyboard, Modal, FlatList, ActivityIndicator, AppState, Alert} from 'react-native';
import {WebView} from 'react-native-webview';
import {SafeAreaView} from 'react-native-safe-area-context';
import * as Clipboard from 'expo-clipboard';
// PA3 Step 1: EventBubble removed — activity tab removed.
import { SessionTelemetry } from '../components/AgentCard';
import ManagedSessionView from '../components/ManagedSessionView';

interface Props {
  onBack: () => void;
  session: string;
  token?: string;            // legacy: dev-token / Supabase JWT (explicit_local_dev only)
  authCtx?: import('../lib/authMode').AuthContext; // M3-auth-4A
  // Server-authorized session capabilities. Unknown is intentionally empty.
  caps?: string[];
  // Production remains transcript-first; explicit only for deterministic render tests.
  initialTab?: 'terminal' | 'transcript';
  // Optional server snapshot carried by a caller that already has the session
  // list response. It is also the deterministic render boundary for the
  // production component; absent data remains fail-closed.
  initialSessionData?: SessionTelemetry;
}

// serverCapabilitiesFromControl is the sole parser for the daemon's hello
// control message. Only a valid hello replaces the capability snapshot; every
// other control frame leaves authorization unchanged.
export function serverCapabilitiesFromControl(data: unknown): string[] | null {
  if (!data || typeof data !== 'object' || (data as any).type !== 'hello') return null;
  const caps = (data as any).capabilities;
  return Array.isArray(caps) ? caps.filter((cap: unknown) => typeof cap === 'string') : [];
}

function jsSend(chars: number[]): string {
  const arr = JSON.stringify(chars);
  return `window.ws.send(String.fromCharCode.apply(null, ${arr}))`;
}

const INPUT_ACK_TIMEOUT_MS = 3000;
type SendStatus = 'idle' | 'sending' | 'socket_sent' | 'delivered' | 'delivery_unknown' | 'not_delivered' | 'failed';
type PendingInput = {
  timeout: ReturnType<typeof setTimeout>;
  connectionId: string;
  sessionId: string;
  generation: number;
  operationId?: string;
  part?: 'text' | 'enter';
};
type PendingLine = {
  operationId: string;
  sentText: string;
  textId: string | null;
  enterId: string | null;
  textOutcome: string | null;
  enterOutcome: string | null;
  enterQueued: boolean;
};

// LOCAL-E2E-R2: stable testIDs for Maestro/Detox native UI automation.
// Each macro exposes a deterministic testID so native tap drivers can
// activate TouchableOpacity without relying on accessibility labels or
// coordinate injection.
const NORMAL_MACROS: { label: string; chars: number[]; testID: string }[] = [
  { label: 'Ctrl+C',  chars: [3],           testID: 'terminal-macro-ctrl-c' },
  { label: 'Esc',     chars: [27],          testID: 'terminal-macro-escape' },
  { label: 'Tab',     chars: [9],           testID: 'terminal-macro-tab' },
  { label: '↑',       chars: [27, 91, 65],  testID: 'terminal-macro-arrow-up' },
  { label: '↓',       chars: [27, 91, 66],  testID: 'terminal-macro-arrow-down' },
  { label: '←',       chars: [27, 91, 68],  testID: 'terminal-macro-arrow-left' },
  { label: '→',       chars: [27, 91, 67],  testID: 'terminal-macro-arrow-right' },
  { label: 'Y',       chars: [121, 13],     testID: 'terminal-macro-y' },
  { label: 'N',       chars: [110, 13],     testID: 'terminal-macro-n' },
  { label: 'Enter',   chars: [13],          testID: 'terminal-macro-enter' },
];




// SP0.5-B: native managed sessions (codex_app_server adapter — an adapter
// namespace branch, never agentKind inference) have NO PTY/terminal. They get
// the dedicated bounded managed view instead of the legacy terminal screen.
export default function FeedScreen(props: Props) {
  if (props.session.startsWith('codex_app_server:')) {
    return <ManagedSessionView session={props.session} token={props.token} onBack={props.onBack} />;
  }
  return <LegacyFeedScreen {...props} />;
}

function LegacyFeedScreen({onBack, session, token, authCtx, caps: initialCaps, initialTab = 'transcript', initialSessionData}: Props) {
  const { tokenMgr, baseURL, termURI } = deriveTerminalAuth(authCtx, session);
  const wv = useRef<any>(null);
  const cmdRef = useRef('');
  const [cmd, setCmd] = useState('');
  const [kbHeight, setKbHeight] = useState(0);

  // PA3 Step 1: two modes — Live Terminal, Transcript (read-only). Activity removed.
	const [activeTab, setActiveTab] = useState<'terminal' | 'transcript'>(initialTab);
  const activeTabRef = useRef(activeTab);
  const [transcriptEvents, setTranscriptEvents] = useState<any[]>([]);
  const [fallbackEvents, setFallbackEvents] = useState<any[]>([]);
  const [byteStreamSuppressed, setByteStreamSuppressed] = useState(false);
  const [newOutputCount, setNewOutputCount] = useState(0);
  const lastSeenSeqRef = useRef(0);
  const transcriptMaxSeqRef = useRef(0);
  // T3: generation counter prevents stale async responses from writing to wrong session.
  const sessionGenRef = useRef(0);
  // PA3 Step 1: Transcript generation for reset detection.
  const transcriptGenRef = useRef(0);
  const [sessionData, setSessionData] = useState<SessionTelemetry | null>(initialSessionData ?? null);
  // Legacy (capabilities===undefined) is treated as no-history for safety.
  const supportsHistory = !!(sessionData?.capabilities?.includes('history'));
  // E8g5: adapter-level capability. observe-only adapters lack liveTerminal.
  const supportsLiveTerminal = !!(sessionData?.adapterCapabilities?.includes('liveTerminal'));
  const isBestEffortTranscript = !!(sessionData?.adapterCapabilities?.includes('bestEffortTranscript'));
  const sessionDataRef = useRef(sessionData);
  sessionDataRef.current = sessionData;
  const [sessionEnded, setSessionEnded] = useState(false);

  // ── M3b: authoritative managed-lifecycle state + action policy ──
  // State is authoritative from the DAEMON only: the session-list `lifecycleState`
  // (Session Catalog, including retained terminal rows) and each Stop/Kill/Delete
  // response. The client never infers state from list presence/absence and never
  // fabricates a terminal state; WS/EOF and network loss are display hints only.
  const [lifecycleState, setLifecycleState] = useState<ManagedLifecycleState>(
    (initialSessionData?.lifecycleState as ManagedLifecycleState | undefined) ?? 'unknown',
  );
  const [pendingAction, setPendingAction] = useState<PendingAction>(null);
  const [actionError, setActionError] = useState('');
  const fetchSessionRef = useRef<(() => void) | null>(null);
  const onBackRef = useRef(onBack);
  onBackRef.current = onBack;

  const { managed, inputCapable } = readCapabilities(sessionData);
  const actionPolicy = computeActionPolicy({ managed, inputCapable, state: lifecycleState, pending: pendingAction });

  // One controller instance per FeedScreen (rebound on session change). FeedScreen
  // drives it directly — there is no parallel copy of the action logic.
  const lifecycleCtrlRef = useRef<SessionLifecycleController | null>(null);
  if (lifecycleCtrlRef.current === null) {
    lifecycleCtrlRef.current = new SessionLifecycleController(
      session, token,
      { stop: stopSession, kill: killSession, del: deleteSessionHistory },
      {
        onState: (next) => setLifecycleState(prev => reconcileState(prev, next)),
        onPending: setPendingAction,
        onError: setActionError,
        onRefresh: () => fetchSessionRef.current?.(),
        onDeleted: () => onBackRef.current(),
      },
    );
  }

  // Reset lifecycle + T3 Transcript state when the viewed session changes.
  useEffect(() => {
    setLifecycleState((initialSessionData?.lifecycleState as ManagedLifecycleState | undefined) ?? 'unknown');
    setPendingAction(null);
    setActionError('');
    setTranscriptEvents([]);
    setFallbackEvents([]);
    setByteStreamSuppressed(false);
    setReadOnlyReason('');
	setCaps(Array.isArray(initialCaps) ? initialCaps : []);
	setSessionData(initialSessionData ?? null);
    transcriptMaxSeqRef.current = 0;
    lastSeenSeqRef.current = 0;
    transcriptGenRef.current = 0;
    sessionGenRef.current++;
    lifecycleCtrlRef.current?.setSession(session, token);
	}, [session, token, initialCaps, initialSessionData]);

  // On unmount (Back/navigation away), invalidate all in-flight async work.
  // Bump generation so late fetch responses, lifecycle actions, and state
  // mutations cannot affect the unmounted screen.
  useEffect(() => () => {
    sessionGenRef.current++;
    lifecycleCtrlRef.current?.dispose();
  }, []);

  const confirmStop = useCallback(() => {
    if (!actionPolicy.canStop) return;
    Alert.alert(
      'Stop session',
      'Stop the entire Pokit-managed terminal process group for this session. Activity and Transcript history are kept.',
      [
        { text: 'Cancel', style: 'cancel' },
        { text: 'Stop', style: 'destructive', onPress: () => lifecycleCtrlRef.current?.stop() },
      ],
    );
  }, [actionPolicy.canStop]);

  const confirmForceKill = useCallback(() => {
    if (!actionPolicy.canForceKill) return;
    Alert.alert(
      'Force kill',
      'Force-kill the managed process group now. This is a last resort after a graceful Stop did not complete.',
      [
        { text: 'Cancel', style: 'cancel' },
        { text: 'Force Kill', style: 'destructive', onPress: () => lifecycleCtrlRef.current?.forceKill() },
      ],
    );
  }, [actionPolicy.canForceKill]);

  const confirmDeleteHistory = useCallback(() => {
    if (!actionPolicy.canDelete) return;
    Alert.alert(
      'Delete history',
      'Permanently remove this ended session’s record and its captured Activity/Transcript history. This cannot be undone.',
      [
        { text: 'Cancel', style: 'cancel' },
        { text: 'Delete', style: 'destructive', onPress: () => lifecycleCtrlRef.current?.deleteHistory() },
      ],
    );
  }, [actionPolicy.canDelete]);

  // PA3 Step 1: when live terminal unsupported, force to transcript.
  useEffect(() => {
    if (!supportsLiveTerminal && activeTab === 'terminal') {
      setActiveTab('transcript');
    }
  }, [supportsLiveTerminal, activeTab]);
  useEffect(() => { activeTabRef.current = activeTab; }, [activeTab]);
  // E8: input delivery status — idle | sending | sent | failed
  const [sendStatus, setSendStatus] = useState<SendStatus>('idle');
  // PB.7 Input-B: pending ACKs tracked by inputId → timeout.
  const pendingInputRef = useRef<Map<string, PendingInput>>(new Map());
  // 2-frame submitLine: text+Enter = 2 requests. Track both ACKs.
  const pendingLineRef = useRef<PendingLine | null>(null);
  const connectionRef = useRef<{ connectionId: string; sessionId: string; generation: number } | null>(null);
  // IDs belonging to a failed two-frame operation never regain delivery
  // status if a sibling result arrives later.
  const abandonedInputRef = useRef<Set<string>>(new Set());
  const lineOperationSeqRef = useRef(0);
  // PB.7 Input-A: caps is a server-authorized capability snapshot. Missing,
  // stale, or not-yet-announced capability data is read-only before any frame.
  const [caps, setCaps] = useState<string[]>(() => Array.isArray(initialCaps) ? initialCaps : []);
  // readOnlyReason is display-only. It never grants or denies input.
  const [readOnlyReason, setReadOnlyReason] = useState('');
  const deviceCanInput = caps.includes('terminal:input');
  const terminalInputEnabled = actionPolicy.inputEnabled && deviceCanInput;

  // R1a: transcript endpoint reachability.
  const [activityError, setActivityError] = useState('');
  // R1a: terminal WebView connection failure.
  const [terminalError, setTerminalError] = useState('');
  const [terminalErrorDetail, setTerminalErrorDetail] = useState('');

  const [copyModalVisible, setCopyModalVisible] = useState(false);
  const [copyText, setCopyText] = useState('');

  // M3-auth-4A: TerminalController owns the bootstrap + ticket lifecycle.
  // The legacy token path is ONLY for local dev mode; remote mode never falls
  // back to it.
  const [termSource, setTermSource] = useState<{ html: string; baseUrl: string } | { uri: string } | null>(null);
  const [termError, setTermError] = useState<string>('');
  const ctrlRef = useRef<TerminalController | null>(null);
  const currentAttemptIdRef = useRef<number>(0);

  useEffect(() => () => {
    for (const pending of pendingInputRef.current.values()) clearTimeout(pending.timeout);
    pendingInputRef.current.clear();
    abandonedInputRef.current.clear();
  }, []);
  useEffect(() => {
    for (const pending of pendingInputRef.current.values()) clearTimeout(pending.timeout);
    pendingInputRef.current.clear();
    pendingLineRef.current = null;
    abandonedInputRef.current.clear();
  }, [session]);

  const doBootstrap = useCallback(async (sess: string, mgr: TokenManager, base: string) => {
    const ctrl = new TerminalController();
    ctrlRef.current?.cancel();
    ctrlRef.current = ctrl;
    try {
      const { attemptId, result } = await ctrl.bootstrap(sess, mgr, base);
      if (!result) return; // stale (attemptId mismatch from later cancel)
      currentAttemptIdRef.current = attemptId;
      connIdRef.current = ctrl.connId;
      setTermSource({ html: result.html, baseUrl: result.baseUrl });
      setTermError('');
    } catch (e: any) {
      setTermError(e instanceof Error ? e.message : 'connection failed');
    }
  }, []);

  useEffect(() => {
    if (tokenMgr && baseURL) {
      // paired_device: async ticket bootstrap.
      setTermSource(null); setTermError('');
      doBootstrap(session, tokenMgr, baseURL);
    } else if (termURI) {
      // explicit_local_dev: direct legacy URL.
      ctrlRef.current?.cancel();
      ctrlRef.current = null;
      setTermSource({ uri: termURI });
      setTermError('');
    } else {
      // initializing / pairing_required / failed: no WebView source.
      ctrlRef.current?.cancel();
      ctrlRef.current = null;
      setTermSource(null);
      setTermError('');
    }
    return () => { ctrlRef.current?.cancel(); ctrlRef.current = null; };
  }, [session, token, tokenMgr, baseURL, termURI, doBootstrap]);

  const doRetry = useCallback(() => {
    if (tokenMgr && baseURL) {
      setTermSource(null); setTermError('');
      doBootstrap(session, tokenMgr, baseURL);
    }
  }, [session, tokenMgr, baseURL, doBootstrap]);

  useEffect(() => {
    const show = Keyboard.addListener('keyboardDidShow', (e) => {
      setKbHeight(e.endCoordinates.height);
    });
    const hide = Keyboard.addListener('keyboardDidHide', () => {
      setKbHeight(0);
    });
    return () => { show.remove(); hide.remove(); };
  }, []);

  useEffect(() => {
    const headers: any = {};
    if (token) headers['Authorization'] = `Bearer ${token}`;

    const fetchSession = () => {
      listSessions(token)
        .then(data => {
          if (!Array.isArray(data)) return;
          const sess = data.find((s: any) => (s.id || s) === session);
          if (sess && typeof sess !== 'string') {
            setSessionData(sess);
            // BLOCKER 1: use the daemon-AUTHORITATIVE lifecycleState from the list
            // (Session Catalog). No presence/absence inference. reconcileState keeps
            // it monotonic so a stale snapshot cannot regress a stopping/terminal
            // session. Empty lifecycleState (non-managed) leaves state 'unknown'.
            const ls = typeof sess.lifecycleState === 'string' ? sess.lifecycleState : '';
            if (ls) {
              setLifecycleState(prev => reconcileState(prev, ls as ManagedLifecycleState));
              setSessionEnded(isTerminalState(ls as ManagedLifecycleState));
            } else {
              setSessionEnded(false);
            }
          } else {
            // Absent from a successful list. With the daemon now RETAINING terminal
            // rows, a managed session that truly disappears was deleted/never
            // existed — surface "unavailable" WITHOUT fabricating a terminal
            // lifecycle state (Delete stays gated on an authoritative terminal row).
            setSessionEnded(true);
          }
        })
        .catch(err => console.error(err));
    };
    fetchSessionRef.current = fetchSession;

    // PA3 Step 1: fetch Transcript with cursor pagination + generation reset detection.
    const fetchTranscript = () => {
      const cursor = transcriptMaxSeqRef.current;
      const gen = sessionGenRef.current;
      getTranscript(session, token, cursor > 0 ? cursor : undefined)
        .then((resp: TranscriptResponse | null) => {
          if (gen !== sessionGenRef.current) return; // stale response
          if (resp && resp.semantic) {
            // PA3 Step 1: generation reset detection. If generation changed
            // (and not first poll), discard all cached segments.
            if (resp.generation !== undefined && transcriptGenRef.current !== 0 && resp.generation !== transcriptGenRef.current) {
              transcriptGenRef.current = resp.generation;
              setTranscriptEvents([]);
              setFallbackEvents([]);
              transcriptMaxSeqRef.current = 0;
              lastSeenSeqRef.current = 0;
              return; // re-fetch with cursor=0 on next poll
            }
            if (resp.generation !== undefined) {
              transcriptGenRef.current = resp.generation;
            }
            // Incremental: append new segments, dedup by ID.
            setTranscriptEvents((prev: any[]) => {
              const seen = new Set(prev.map((e: any) => e.id));
              const fresh = resp.semantic.filter((s: any) => !seen.has(s.id));
              return [...prev, ...fresh].sort((a: any, b: any) => a.seq - b.seq);
            });
            setFallbackEvents((prev: any[]) => {
              const seen = new Set(prev.map((e: any) => e.id));
              const fresh = (resp.fallback || []).filter((s: any) => !seen.has(s.id));
              return [...prev, ...fresh].sort((a: any, b: any) => a.seq - b.seq);
            });
            setByteStreamSuppressed(!!resp.byteStreamSuppressed);
            setActivityError('');
            const allSeqs = [...resp.semantic, ...(resp.fallback || [])];
            const maxSeq = allSeqs.reduce((m: number, e: any) => Math.max(m, e.seq || 0), 0);
            transcriptMaxSeqRef.current = maxSeq;
            if (activeTabRef.current === 'transcript' && maxSeq > lastSeenSeqRef.current) {
              setNewOutputCount(maxSeq - lastSeenSeqRef.current);
            }
            if (activeTabRef.current !== 'transcript') {
              lastSeenSeqRef.current = maxSeq;
            }
          } else if (resp === null) {
            setActivityError('Transcript response validation failed.');
          }
        })
        .catch(async (err) => {
          console.error(err);
          if (err instanceof PokitError) {
            setActivityError(err.failure === ConnectivityFailure.NetworkUnreachable
              ? 'Transcript endpoint unreachable.'
              : 'Transcript endpoint error (' + err.statusCode + ').');
          } else {
            setActivityError('Transcript endpoint unreachable.');
          }
        });
    };

    fetchSession();
    fetchTranscript();
    const interval = setInterval(() => {
      fetchSession();
      fetchTranscript();
    }, 3000);
    return () => clearInterval(interval);
  }, [session]);

  const inject = useCallback((js: string) => {
    if (wv.current) {
      wv.current.injectJavaScript(js + ';true;');
    }
  }, []);

  // Foreground reconnect: while the app is backgrounded (e.g. switching to
  // AnyDesk) the OS suspends the WebView's WebSocket and its reconnect timers,
  // so the terminal can return to a dead/stale socket. On return to 'active',
  // force a reconnect when the socket isn't open. connect() closes the old ws
  // first, so no duplicate recorder subscribers. Reuses the served HTML's own
  // globals (stopped / reconnecting / consecutiveFailures / connect).
  useEffect(() => {
    const sub = AppState.addEventListener('change', (state) => {
      if (state !== 'active') return;
      inject(
        '(function(){try{' +
        'if(typeof stopped!=="undefined"&&stopped)return;' +
        'var w=window.ws;' +
        'if(w&&w.readyState===1)return;' +
        'if(typeof reconnecting!=="undefined")reconnecting=false;' +
        'if(typeof consecutiveFailures!=="undefined")consecutiveFailures=0;' +
        'if(typeof connect==="function")connect();' +
        '}catch(e){}})()'
      );
    });
    return () => sub.remove();
  }, [inject]);

  // E8: send text via injected JS with postMessage ack back to React Native.
  // PB.7 Input-A: gated on server-authorized capabilities before any frame.
  const doSend = useCallback((text: string, operationId?: string, part?: 'text' | 'enter') => {
    if (!deviceCanInput) {
      setSendStatus('failed');
      return;
    }
    if (!text || !wv.current) {
      if (operationId && pendingLineRef.current?.operationId === operationId) pendingLineRef.current = null;
      setSendStatus('delivery_unknown');
      return;
    }
    const parsedText = text.replace(/\n/g, '\r');
    setSendStatus('sending');
    inject(
      '(function(){' +
      // PB.7 Input-A: server read_only gate — check before enqueue.
      'if(window.pokitReadOnly&&window.pokitReadOnly()){' +
      'window.ReactNativeWebView.postMessage(JSON.stringify({type:"delivery_unknown",operationId:' + JSON.stringify(operationId || null) + ',part:' + JSON.stringify(part || null) + '}));' +
      'return;' +
      '}' +
      'var w=window.ws;' +
      'if(!w||w.readyState!==1){' +
      'window.ReactNativeWebView.postMessage(JSON.stringify({type:"delivery_unknown",operationId:' + JSON.stringify(operationId || null) + ',part:' + JSON.stringify(part || null) + '}));' +
      'return;' +
      '}' +
      'try{' +
      // M3-auth-4A framing contract: raw input goes out as a BINARY frame via
      // the page's single sender. Fall back to an inline binary encode if the
      // page helper is not yet defined (still binary — never a text frame).
      'if(window.pokitSendInput){window.pokitSendInput(' + JSON.stringify(parsedText) + ',' + JSON.stringify(operationId || null) + ',' + JSON.stringify(part || null) + ');}' +
      'else{throw new Error("terminal input protocol unavailable");}' +
      '}catch(e){' +
      'window.ReactNativeWebView.postMessage(JSON.stringify({type:"delivery_unknown",operationId:' + JSON.stringify(operationId || null) + ',part:' + JSON.stringify(part || null) + '}));' +
      '}' +
      '})()'
    );
  }, [inject, deviceCanInput]);

  // submitLine sends the text and Enter as TWO separate messages. Codex treats
  // a trailing \r bundled with the text as a literal newline (paste heuristic),
  // so combined "text\r" inserts a newline instead of submitting; a standalone
  // \r submits — which is why the custom Enter macro ([13]) worked but Send did
  // not. Splitting makes Send equivalent to "type text, then press Enter". The
  // small delay keeps the \r in a separate PTY read so Codex sees a discrete
  // Enter keypress. Text goes out once, \r once — no accumulated-buffer bug.
  const submitLine = useCallback((text: string) => {
    if (!deviceCanInput) { setSendStatus('failed'); return; }
    if (pendingLineRef.current) {
      // One line operation owns the text/Enter pair; do not let an overlapping
      // command steal its ACK slots or overwrite a partial-delivery result.
      setSendStatus('not_delivered');
      return;
    }
    // PB.7 Input-B: 2-frame operation. Track both text+Enter ACKs.
    const line: PendingLine = { operationId: `line-${++lineOperationSeqRef.current}`, sentText: text, textId: null, enterId: null, textOutcome: null, enterOutcome: null, enterQueued: false };
    pendingLineRef.current = line;
    if (text) doSend(text, line.operationId, 'text');
    setTimeout(() => {
      // A failed/timeout first frame cancels this operation; never send a
      // delayed Enter into a later command's two-frame state.
      if (pendingLineRef.current !== line) return;
      line.enterQueued = true;
      doSend('\r', line.operationId, 'enter');
    }, 40);
  }, [doSend, deviceCanInput]);

  const send = useCallback(() => {
    const currentCmd = cmdRef.current || cmd;
    if (!currentCmd.trim()) return;
    submitLine(currentCmd);
    // PB.7 Input-B: preserve command until ACK. Clear only after both
    // text+Enter receive accepted results (handled in the input_result
    // handler — when pendingInputRef is empty, SendStatus shows Delivered).
  }, [submitLine, session]);
  const sendMacro = useCallback((chars: number[]) => {
    if (!deviceCanInput) { setSendStatus('failed'); return; }
    const macroText = String.fromCharCode.apply(null, chars);
    doSend(macroText);
  }, [doSend, deviceCanInput]);

  const handleChangeText = useCallback((text: string) => {
    cmdRef.current = text;
    // Soft keyboard Enter may insert \n without firing onSubmitEditing.
    // Detect trailing \n, strip it, and send the command.
    if (text.endsWith('\n')) {
      const cmd = text.replace(/\n$/, '');
      // Treat soft-keyboard Enter exactly like Send: preserve the command
      // until both acknowledged frames settle.
      setCmd(cmd);
      cmdRef.current = cmd;
      if (cmd.trim()) {
        submitLine(cmd);
      }
    } else {
      setCmd(text);
    }
  }, [submitLine]);

  const handleCopyRequest = useCallback(async () => {
    // Get terminal text and copy directly
    const js = '(function(){var s="";for(var i=0;i<term.rows;i++){var l=term.buffer.active.getLine(i);if(l)s+=l.translateToString(true)+"\n"}return s})()';
    inject('window.ReactNativeWebView.postMessage(JSON.stringify({type:"copy",text:'+js+'}))');
    // Also try to get via clipboard directly from the WebView
    try {
      inject('window.ReactNativeWebView.postMessage(JSON.stringify({type:"copy",text:(function(){var s="";for(var i=0;i<term.rows;i++){var l=term.buffer.active.getLine(i);if(l)s+=l.translateToString(true)+"\n"}return s})()}))');
    } catch(e) {}
  }, [inject]);

  const handlePasteRequest = useCallback(async () => {
    if (!deviceCanInput) { setSendStatus('failed'); return; }
    const text = await Clipboard.getStringAsync();
    if (text) {
      doSend(text);
    }
  }, [doSend, deviceCanInput]);



  // Reconnect bridge: WebView messages {type:'pokit-reconnect-request'} are
  // received here. The controller issues a fresh ticket and posts it back.
  const connIdRef = useRef<number>(0);
  const handleReconnect = useCallback(async (data: any) => {
    if (!tokenMgr || !baseURL) return;
    // Stale session/connId/attemptId requests issue no ticket (see predicate).
    if (!shouldIssueReconnect(data, {
      session,
      connId: connIdRef.current,
      attemptId: currentAttemptIdRef.current,
    })) return;
    const ctrl = ctrlRef.current;
    if (!ctrl) return;
    const { ticket } = await ctrl.reconnectTicket(session, tokenMgr, baseURL);
    if (!ticket || ctrl !== ctrlRef.current) return;
    wv.current?.postMessage(JSON.stringify({ type: 'pokit-ticket', ticket }));
  }, [session, tokenMgr, baseURL]);

  const onMessage = useCallback((event: any) => {
    try {
      const data = JSON.parse(event.nativeEvent.data);
      if (data.type === 'copy') {
        setCopyText(data.text);
        setCopyModalVisible(true);
      }
      if (data.type === 'sendStatus') {
        setSendStatus(data.status === 'sent' ? 'socket_sent' : 'failed');
      }
      if (data.type === 'hello') {
        const nextCaps = serverCapabilitiesFromControl(data);
        if (nextCaps !== null && typeof data.connectionId === 'string' && data.connectionId && data.sessionId === session && Number.isInteger(data.generation)) {
          const nextConnection = { connectionId: data.connectionId, sessionId: data.sessionId, generation: data.generation };
          const currentConnection = connectionRef.current;
          const isNewConnection = currentConnection === null ||
            currentConnection.connectionId !== nextConnection.connectionId ||
            currentConnection.sessionId !== nextConnection.sessionId ||
            currentConnection.generation !== nextConnection.generation;
          // TERM-C1: a repeated hello from the same bridge snapshot is a
          // permission refresh, not a reconnect. Only a genuinely new server
          // connection makes outstanding acknowledged input delivery unknown.
          if (isNewConnection) {
            if (pendingInputRef.current.size > 0 || pendingLineRef.current !== null) {
              setSendStatus('delivery_unknown');
            }
            for (const pending of pendingInputRef.current.values()) clearTimeout(pending.timeout);
            pendingInputRef.current.clear();
            pendingLineRef.current = null;
            abandonedInputRef.current.clear();
          }
          connectionRef.current = nextConnection;
          setCaps(nextCaps);
          setReadOnlyReason(nextCaps.includes('terminal:input') ? '' : 'Terminal input not authorized — view only');
        } else if (nextCaps !== null) {
          // A hello not bound to this screen cannot grant input.
          connectionRef.current = null;
          setCaps([]);
          setReadOnlyReason('Terminal input not authorized — view only');
        }
      }
      // PB.7 Input-B: pending/result are accepted only when all connection
      // identities match the server hello snapshot and the individual request.
      if (data.type === 'input_pending' && typeof data.connectionId === 'string' && typeof data.sessionId === 'string' && Number.isInteger(data.generation) && typeof data.inputId === 'string') {
        const conn = connectionRef.current;
        if (!conn || data.connectionId !== conn.connectionId || data.sessionId !== conn.sessionId || data.generation !== conn.generation) return;
        const key = data.inputId as string;
        const line = pendingLineRef.current;
        const operationId = typeof data.operationId === 'string' ? data.operationId : undefined;
        const part = data.part === 'text' || data.part === 'enter' ? data.part as 'text' | 'enter' : undefined;
        // Only the page-tagged frames from this exact line operation own its
        // two slots. Macro/paste/raw keyboard pending events cannot steal them
        // merely by arriving first.
        if (line && operationId === line.operationId && part === 'text' && line.textId === null) line.textId = key;
        else if (line && operationId === line.operationId && part === 'enter' && line.enterQueued && line.enterId === null) line.enterId = key;
        const existing = pendingInputRef.current.get(key);
        if (existing) clearTimeout(existing.timeout);
        const timeout = setTimeout(() => {
          const pending = pendingInputRef.current.get(key);
          if (!pending) return;
          pendingInputRef.current.delete(key);
          const activeLine = pendingLineRef.current;
          if (activeLine && (key === activeLine.textId || key === activeLine.enterId)) {
            for (const sibling of [activeLine.textId, activeLine.enterId]) {
              if (!sibling) continue;
              abandonedInputRef.current.add(sibling);
              const siblingPending = pendingInputRef.current.get(sibling);
              if (siblingPending) {
                clearTimeout(siblingPending.timeout);
                pendingInputRef.current.delete(sibling);
              }
            }
            pendingLineRef.current = null;
          }
          setSendStatus('delivery_unknown');
        }, INPUT_ACK_TIMEOUT_MS);
        pendingInputRef.current.set(key, { timeout, connectionId: conn.connectionId, sessionId: conn.sessionId, generation: conn.generation, operationId, part });
        setSendStatus('socket_sent');
      }
      if (data.type === 'input_result' && typeof data.inputId === 'string' && typeof data.outcome === 'string' && typeof data.connectionId === 'string' && typeof data.sessionId === 'string' && Number.isInteger(data.generation)) {
        const key = data.inputId as string;
        if (abandonedInputRef.current.has(key)) return;
        const pending = pendingInputRef.current.get(key);
        if (!pending || data.connectionId !== pending.connectionId || data.sessionId !== pending.sessionId || data.generation !== pending.generation) return;
        clearTimeout(pending.timeout);
        pendingInputRef.current.delete(key);
        const line = pendingLineRef.current;
        if (line && (key === line.textId || key === line.enterId)) {
          if (key === line.textId) line.textOutcome = data.outcome;
          if (key === line.enterId) line.enterOutcome = data.outcome;
          if (data.outcome !== 'accepted') {
            for (const sibling of [line.textId, line.enterId]) {
              if (!sibling) continue;
              abandonedInputRef.current.add(sibling);
              const siblingPending = pendingInputRef.current.get(sibling);
              if (siblingPending) {
                clearTimeout(siblingPending.timeout);
                pendingInputRef.current.delete(sibling);
              }
            }
            pendingLineRef.current = null;
            // PB.7 Input-B R9: if the sibling frame was already accepted,
            // bytes reached the PTY. ANY non-accepted outcome on the second
            // frame is possible partial delivery — never definite failure.
            const siblingAccepted = (key === line.textId && line.enterOutcome === 'accepted') ||
                                    (key === line.enterId && line.textOutcome === 'accepted');
            const anyNonAccepted = data.outcome !== 'accepted';
            // write_failed = short write, bytes partially consumed.
            const isShortWrite = data.outcome === 'write_failed';
            if (anyNonAccepted && (siblingAccepted || isShortWrite)) {
              setSendStatus('delivery_unknown');
            } else {
              setSendStatus(anyNonAccepted ? 'not_delivered' : 'delivered');
            }
          } else if (line.enterQueued && line.textId !== null && line.enterId !== null && line.textOutcome === 'accepted' && line.enterOutcome === 'accepted') {
            pendingLineRef.current = null;
            setSendStatus('delivered');
            // Do not erase a newer edit made while this operation awaited ACKs.
            if (cmdRef.current === line.sentText) {
              setCmd('');
              cmdRef.current = '';
            }
          }
        } else if (!line) {
          // PB.7 Input-B R9: standalone macro/paste/keyboard input.
          // write_failed = short write implies bytes may have been consumed.
          setSendStatus(data.outcome === 'accepted' ? 'delivered' :
                        data.outcome === 'write_failed' ? 'delivery_unknown' : 'not_delivered');
        }
      }
      if (data.type === 'delivery_unknown') {
        const operationId = typeof data.operationId === 'string' ? data.operationId : null;
        const line = pendingLineRef.current;
        if (operationId && line && line.operationId === operationId) {
          // One unknown sibling makes the entire two-frame command unknown.
          // Otherwise a late accepted text/Enter ACK could falsely settle the
          // command as Delivered after the page failed to submit its sibling.
          for (const sibling of [line.textId, line.enterId]) {
            if (!sibling) continue;
            abandonedInputRef.current.add(sibling);
            const siblingPending = pendingInputRef.current.get(sibling);
            if (siblingPending) {
              clearTimeout(siblingPending.timeout);
              pendingInputRef.current.delete(sibling);
            }
          }
          pendingLineRef.current = null;
          setSendStatus('delivery_unknown');
        } else if (!line) {
          // Macro/paste/keyboard failures are independent operations. They
          // may update the status only when no line awaits both ACKs.
          setSendStatus('delivery_unknown');
        }
      }
      // PB.7 Input-A: server read_only denial — surfaces permission reason from daemon.
      // Reason persists until the server explicitly re-grants or the session changes.
      if (data.type === 'read_only') {
        setReadOnlyReason(data.reason || 'Read only');
        setSendStatus('failed');
      }
      if (data.type === 'e8diag') {
        console.log('E8DIAG', JSON.stringify(data));
      }
      // M3-auth-4A reconnect bridge.
      if (data?.type === 'pokit-reconnect-request') {
        handleReconnect(data);
      }
    } catch (e) {}
  }, [handleReconnect, session]);

  const pinchZoomInjection = `
    (function() {
      let initialDistance = null;
      let initialFontSize = null;

      const style = document.createElement('style');
      // Allow the terminal to be wider than the phone viewport and scroll/swipe
      // horizontally, instead of reflowing to a narrow width (see fitTerminal).
      style.innerHTML =
        '.xterm-screen { user-select: text; -webkit-user-select: text; }' +
        '#t { overflow: auto !important; -webkit-overflow-scrolling: touch; }' +
        '.xterm { width: max-content !important; min-width: 100% !important; }';
      document.head.appendChild(style);

      // TERM-C1: geometry belongs solely to the served-page control bridge.
      // Viewport changes may alter scrollability and font scale, never rows or
      // columns. The daemon page's resize hook calls this no-op replacement.
      window.fitTerminal = function() {};

      document.addEventListener('touchstart', function(e) {
        if(e.touches.length === 2 && window.term) {
          initialDistance = Math.hypot(
            e.touches[0].pageX - e.touches[1].pageX,
            e.touches[0].pageY - e.touches[1].pageY
          );
          initialFontSize = window.term.options.fontSize || 12;
        }
      });

      document.addEventListener('touchmove', function(e) {
        if(e.touches.length === 2 && initialDistance && window.term) {
          const currentDistance = Math.hypot(
            e.touches[0].pageX - e.touches[1].pageX,
            e.touches[0].pageY - e.touches[1].pageY
          );
          const scale = currentDistance / initialDistance;
          const newFontSize = Math.max(6, Math.min(60, Math.round(initialFontSize * scale)));
          
          if (window.term.options.fontSize !== newFontSize) {
            window.term.options.fontSize = newFontSize;
          }
        }
      });

      document.addEventListener('touchend', function(e) {
        if(e.touches.length < 2) {
          initialDistance = null;
        }
      });
      
      window.getTerminalText = function() {
        if (window.term && window.term.hasSelection()) {
          window.ReactNativeWebView.postMessage(JSON.stringify({
            type: 'copy',
            text: window.term.getSelection()
          }));
        }
      };

      true;
    })();
  `;

  return (
    <SafeAreaView style={styles.container}>
      {termError ? (
        <View style={{ padding: 20, alignItems: 'center' }}>
          <Text style={{ color: '#f85149', fontSize: 14, textAlign: 'center', marginBottom: 8 }}>
            Could not connect to terminal: {termError}
          </Text>
          <TouchableOpacity onPress={doRetry} style={{ backgroundColor: '#1E91B3', paddingHorizontal: 20, paddingVertical: 10, borderRadius: 8 }}>
            <Text style={{ color: '#fff', fontWeight: '700' }}>RETRY</Text>
          </TouchableOpacity>
        </View>
      ) : null}
      <View style={styles.flex}>
        <View style={styles.header}>
          <TouchableOpacity onPress={onBack}><Text style={styles.backBtn}>←</Text></TouchableOpacity>
          <Text style={styles.headerTitle}>{session}</Text>
          <View style={{flexDirection: 'row', alignItems: 'center'}}>
            {activeTab === 'terminal' && (
              <>
                <TouchableOpacity onPress={handleCopyRequest} style={styles.textBtn}><Text style={styles.textBtnText}>COPY</Text></TouchableOpacity>
                <TouchableOpacity disabled={!deviceCanInput} onPress={handlePasteRequest} style={styles.textBtn}><Text style={styles.textBtnText}>PASTE</Text></TouchableOpacity>
                <TouchableOpacity onPress={() => {
                  if (wv.current) wv.current.reload();
                }}><Text style={styles.reloadBtn}>↻</Text></TouchableOpacity>
              </>
            )}
          </View>
        </View>

        {/* M3b: managed session lifecycle actions. Rendered ONLY for sessions the
            daemon reports as managedLifecycle; capability/state drive visibility.
            Back (←) above is a viewer detach only and never calls these. */}
        {managed && (
          <View style={styles.lifecycleBar}>
            <Text style={styles.lifecycleStatus}>{actionPolicy.statusLabel}</Text>
            <View style={styles.lifecycleActions}>
              {actionPolicy.canStop && (
                <TouchableOpacity
                  style={[styles.lifecycleBtn, styles.lifecycleStopBtn]}
                  disabled={pendingAction !== null}
                  onPress={confirmStop}
                >
                  <Text style={styles.lifecycleStopText}>{pendingAction === 'stop' ? 'STOPPING…' : 'STOP'}</Text>
                </TouchableOpacity>
              )}
              {lifecycleState === 'stopping' && (
                <TouchableOpacity
                  style={[styles.lifecycleBtn, styles.lifecycleKillBtn]}
                  disabled={pendingAction !== null || !actionPolicy.canForceKill}
                  onPress={confirmForceKill}
                >
                  <Text style={styles.lifecycleKillText}>{pendingAction === 'kill' ? 'KILLING…' : 'FORCE KILL'}</Text>
                </TouchableOpacity>
              )}
              {actionPolicy.canDelete && (
                <TouchableOpacity
                  style={[styles.lifecycleBtn, styles.lifecycleDeleteBtn]}
                  disabled={pendingAction !== null}
                  onPress={confirmDeleteHistory}
                >
                  <Text style={styles.lifecycleDeleteText}>{pendingAction === 'delete' ? 'DELETING…' : 'DELETE HISTORY'}</Text>
                </TouchableOpacity>
              )}
            </View>
            {actionError ? <Text style={styles.lifecycleError}>{actionError}</Text> : null}
          </View>
        )}

        <View style={styles.tabBar}>
          {/* E8g5: Terminal tab only for liveTerminal-capable adapters.
              Legacy sessions without adapterCapabilities default to showing Terminal. */}
          {(supportsLiveTerminal || !sessionData?.adapterCapabilities) && (
          <TouchableOpacity
            style={[styles.tab, activeTab === 'terminal' && styles.activeTab]}
            onPress={() => setActiveTab('terminal')}
          >
            <Text style={[styles.tabText, activeTab === 'terminal' && styles.activeTabText]}>TERMINAL</Text>
          </TouchableOpacity>
          )}
          {/* PA3 Step 1: Transcript tab — sole history/activity read path. */}
          <TouchableOpacity
            style={[styles.tab, activeTab === 'transcript' && styles.activeTab]}
            onPress={() => { lastSeenSeqRef.current = transcriptMaxSeqRef.current; setNewOutputCount(0); setActiveTab('transcript'); }}
          >
            <Text style={[styles.tabText, activeTab === 'transcript' && styles.activeTabText]}>TRANSCRIPT</Text>
          </TouchableOpacity>
        </View>

        <View style={{flex: 1, display: activeTab === 'terminal' ? 'flex' : 'none'}}>
          {terminalError ? (
            /* R1a: terminal WebView connection failure — visible diagnostic. */
            <View style={styles.endedContainer}>
              <Text style={styles.endedTitle}>TERMINAL CONNECTION FAILED</Text>
              <Text style={styles.endedText}>
                Check daemon URL / tunnel / network.{'\n'}
                The daemon or tunnel may be unreachable.
              </Text>
              {terminalErrorDetail ? (
                <Text style={styles.errorDetail}>{terminalErrorDetail}</Text>
              ) : null}
              <TouchableOpacity
                style={{marginTop: 16, backgroundColor: '#1E91B3', paddingHorizontal: 20, paddingVertical: 10, borderRadius: 8}}
                onPress={() => { setTerminalError(''); setTerminalErrorDetail(''); if (wv.current) wv.current.reload(); }}
              >
                <Text style={{color: '#fff', fontWeight: '700'}}>RETRY</Text>
              </TouchableOpacity>
            </View>
          ) : sessionEnded ? (
            <View style={styles.endedContainer}>
              <Text style={styles.endedTitle}>SESSION ENDED</Text>
              <Text style={styles.endedText}>This terminal session is no longer available.</Text>
            </View>
          ) : (
            <WebView
              ref={wv}
              source={termSource || { uri: 'about:blank' }}
              style={styles.webview}
              javaScriptEnabled
              domStorageEnabled
              injectedJavaScript={pinchZoomInjection}
              originWhitelist={['*']}
              cacheEnabled={false}
              onMessage={onMessage}
              onError={(e) => {
                const desc = e?.nativeEvent?.description || '';
                setTerminalError(desc || 'WebView failed to load terminal.');
                setTerminalErrorDetail('Error: ' + (desc || 'unknown'));
              }}
              onHttpError={(e) => {
                const status = e?.nativeEvent?.statusCode || 0;
                const desc = e?.nativeEvent?.description || '';
                setTerminalError('Terminal HTTP ' + (status || 'error') + '. Check daemon URL.');
                setTerminalErrorDetail('HTTP ' + status + (desc ? ': ' + desc : ''));
              }}
            />
          )}
        </View>

        {/* PA3 Step 1: Transcript Mode — sole read-only history. */}
        <View style={[styles.transcriptContainer, {display: activeTab === 'transcript' ? 'flex' : 'none'}]}>
          {/* E8g6: show observe/degraded state for best-effort transcript. */}
          {isBestEffortTranscript && (
            <View style={[styles.transcriptInfo, {backgroundColor: '#1a1a0a'}]}>
              <Text style={styles.transcriptInfoText}>Best-effort transcript — screen snapshot based. May show duplicates.</Text>
            </View>
          )}
          {!isBestEffortTranscript && (
          <View style={styles.transcriptInfo}>
            <Text style={styles.transcriptInfoText}>Read-only history. Live Terminal is source of truth for interactive/TUI.</Text>
          </View>
          )}
          {newOutputCount > 0 && (
            <TouchableOpacity style={styles.newOutputBanner} onPress={() => {
              lastSeenSeqRef.current = transcriptMaxSeqRef.current;
              setNewOutputCount(0);
              if (supportsLiveTerminal) { setActiveTab('terminal'); }
            }}>
              <Text style={styles.newOutputText}>
                {supportsLiveTerminal ? '↓ New output — Return to Live Terminal' : '↓ New output'}
              </Text>
            </TouchableOpacity>
          )}
          {/* T3: byte-stream suppressed after terminal input */}
          {byteStreamSuppressed && (
            <View style={styles.suppressedBanner}>
              <Text style={styles.suppressedBannerText}>
                Transcript capture paused after terminal input. Live output may not appear here.
              </Text>
            </View>
          )}
          {/* R1a: transcript endpoint error state */}
          {activityError ? (
            <View style={{padding: 20, alignItems: 'center'}}>
              <Text style={{color: '#f85149', fontSize: 13, textAlign: 'center', marginBottom: 8}}>{activityError}</Text>
              <Text style={{color: '#8b949e', fontSize: 11, textAlign: 'center'}}>Transcript may still be loading. Retrying automatically.</Text>
            </View>
          ) : !transcriptEvents || transcriptEvents.length === 0 ? (
            <>
              {/* T3: fallback may have content even when semantic is empty (snapshot-only) */}
              {fallbackEvents && fallbackEvents.length > 0 ? (
                <View style={styles.fallbackSection}>
                  <Text style={styles.fallbackHeader}>⌇ Terminal output (snapshot / degraded)</Text>
                  <E8g2Transcript events={fallbackEvents} />
                </View>
              ) : (
                <Text style={styles.emptyActivityText}>No transcript yet — open Terminal to start capture.</Text>
              )}
            </>
          ) : (
            <>
              <E8g2Transcript events={transcriptEvents} />
              {fallbackEvents && fallbackEvents.length > 0 && (
                <View style={styles.fallbackSection}>
                  <Text style={styles.fallbackHeader}>⌇ Terminal output (degraded)</Text>
                  <E8g2Transcript events={fallbackEvents} />
                </View>
              )}
            </>
          )}
          {supportsLiveTerminal && (
          <TouchableOpacity style={styles.returnBtn} onPress={() => { lastSeenSeqRef.current = transcriptMaxSeqRef.current; setNewOutputCount(0); setActiveTab('terminal'); }}>
            <Text style={styles.returnBtnText}>← Return to Live Terminal</Text>
          </TouchableOpacity>
          )}
        </View>

        {/* Input requires both transport state and the server-authorized
            terminal:input capability. Missing capabilities fail closed before
            any user frame; readOnlyReason is only the explanation. */}
        {activeTab === 'terminal' && !sessionEnded && !terminalInputEnabled && (
          <View style={styles.readOnlyBar}>
            <Text style={styles.readOnlyText}>{readOnlyReason || 'View only — terminal input not authorized'}</Text>
          </View>
        )}
        {activeTab === 'terminal' && !sessionEnded && (
        <>
        <View style={styles.macroContainer}>
          <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={styles.macroScroll}>
            {NORMAL_MACROS.map((m, i) => (
              <TouchableOpacity key={i} testID={m.testID} style={styles.macroBtn} disabled={!terminalInputEnabled} onPress={() => sendMacro(m.chars)}>
                <Text style={styles.macroText}>{m.label}</Text>
              </TouchableOpacity>
            ))}
          </ScrollView>
        </View>

        <View style={[styles.inputContainer, {paddingBottom: Math.max(kbHeight, Platform.OS === 'ios' ? 20 : 6)}]}>
          <TextInput
            style={styles.input}
            placeholder="$ type a command..."
            placeholderTextColor="#666"
            value={cmd}
            onChangeText={handleChangeText}
            onSubmitEditing={send}
            returnKeyType="send"
            autoCorrect={false}
            autoCapitalize="none"
            multiline={false}
            blurOnSubmit={false}
            editable={terminalInputEnabled}
            testID="terminal-input"
          />
          {/* E8: input delivery status indicator */}
          {sendStatus !== 'idle' && (
            <Text testID="terminal-send-status" style={[styles.sendStatus, (sendStatus === 'failed' || sendStatus === 'not_delivered' || sendStatus === 'delivery_unknown') && styles.sendFailed]}>
              {sendStatus === 'sending' ? '↑' : sendStatus === 'socket_sent' ? 'Sent to socket' : sendStatus === 'delivered' ? 'Delivered to terminal' : sendStatus === 'delivery_unknown' ? 'Possible partial delivery' : sendStatus === 'not_delivered' ? 'Not delivered' : '✗'}
            </Text>
          )}
          <TouchableOpacity testID="terminal-send" disabled={!terminalInputEnabled} onPress={send} style={styles.btn}><Text style={styles.btnT}>Send</Text></TouchableOpacity>
        </View>
        </>
        )}
      </View>

      <Modal
        visible={copyModalVisible}
        animationType="slide"
        transparent={true}
        onRequestClose={() => setCopyModalVisible(false)}
      >
        <View style={styles.modalBg}>
          <View style={styles.modalContainer}>
            <View style={styles.modalHeader}>
              <Text style={styles.modalTitle}>Select & Copy Text</Text>
              <TouchableOpacity onPress={() => setCopyModalVisible(false)}>
                <Text style={styles.closeBtn}>Close</Text>
              </TouchableOpacity>
            </View>
            <ScrollView style={styles.modalScroll}>
              <Text style={styles.modalText} selectable={true}>
                {copyText}
              </Text>
            </ScrollView>
            <View style={styles.modalFooter}>
              <Text style={styles.modalHint}>Long press on the text above to select and copy.</Text>
            </View>
          </View>
        </View>
      </Modal>
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  container: {flex:1, backgroundColor:'#000000'},
  flex: {flex:1},
  header: {
    flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center',
    paddingHorizontal: 12, paddingVertical: 10, borderBottomWidth: 1, borderBottomColor: '#0D2D45',
  },
  headerTitle: {fontSize: 14, color: '#1E91B3', fontWeight: '700', letterSpacing: 0.96},
  backBtn: {fontSize: 24, color: '#45EBE9'},
  reloadBtn: {fontSize: 20, color: '#45EBE9', paddingHorizontal: 4},
  textBtn: {backgroundColor: 'transparent', paddingHorizontal: 10, paddingVertical: 4, borderRadius: 32, borderWidth: 1, borderColor: '#1E91B3', marginRight: 10},
  textBtnText: {color: '#1E91B3', fontSize: 12, fontWeight: '700', letterSpacing: 0.96},
  
  tabBar: {
    flexDirection: 'row',
    borderBottomWidth: 1,
    borderBottomColor: '#0D2D45',
  },
  tab: {
    flex: 1,
    paddingVertical: 12,
    alignItems: 'center',
  },
  activeTab: {
    borderBottomWidth: 2,
    borderBottomColor: '#45EBE9',
  },
  tabText: {
    color: '#8b949e',
    fontSize: 12,
    fontWeight: '700',
    letterSpacing: 1,
  },
  activeTabText: {
    color: '#45EBE9',
  },

  webview: {flex:1, backgroundColor:'#000'},
  endedContainer: {flex: 1, backgroundColor: '#000', alignItems: 'center', justifyContent: 'center', padding: 24},
  endedTitle: {color: '#f85149', fontSize: 16, fontWeight: '800', letterSpacing: 1.2, marginBottom: 8},
  endedText: {color: '#8b949e', fontSize: 13, textAlign: 'center', lineHeight: 20},
  errorDetail: {color: '#666', fontSize: 10, textAlign: 'center', marginTop: 8, fontFamily: 'monospace'},
  macroContainer: { backgroundColor: '#000000', borderTopWidth: 1, borderTopColor: '#0D2D45' },
  macroScroll: { paddingHorizontal: 6, paddingVertical: 6, alignItems: 'center' },
  // M3b lifecycle action bar.
  lifecycleBar: { backgroundColor: '#000000', borderBottomWidth: 1, borderBottomColor: '#0D2D45', paddingHorizontal: 12, paddingVertical: 8 },
  lifecycleStatus: { color: '#8b949e', fontSize: 11, fontWeight: '700', letterSpacing: 1, marginBottom: 6 },
  lifecycleActions: { flexDirection: 'row', flexWrap: 'wrap', gap: 8 },
  lifecycleBtn: { paddingHorizontal: 14, paddingVertical: 7, borderRadius: 32, borderWidth: 1 },
  lifecycleStopBtn: { borderColor: '#d29922' },
  lifecycleStopText: { color: '#d29922', fontSize: 12, fontWeight: '800', letterSpacing: 0.96 },
  lifecycleKillBtn: { borderColor: '#f85149' },
  lifecycleKillText: { color: '#f85149', fontSize: 12, fontWeight: '800', letterSpacing: 0.96 },
  lifecycleDeleteBtn: { borderColor: '#f85149' },
  lifecycleDeleteText: { color: '#f85149', fontSize: 12, fontWeight: '800', letterSpacing: 0.96 },
  lifecycleError: { color: '#f85149', fontSize: 11, marginTop: 6 },
  macroBtn: { backgroundColor: 'transparent', paddingHorizontal: 10, paddingVertical: 6, borderRadius: 32, marginRight: 5, borderWidth: 1, borderColor: '#1E91B3' },
  macroText: { color: '#1E91B3', fontSize: 12, fontWeight: '700', letterSpacing: 0.96 },
  inputContainer: {
    flexDirection: 'row', padding: 6, backgroundColor: '#000000',
    borderTopWidth: 1, borderTopColor: '#0D2D45', alignItems: 'center',
  },
  input: {
    flex:1, backgroundColor: '#000000', color: '#ffffff', borderRadius: 4,
    borderWidth: 1, borderColor: '#1E91B3', paddingHorizontal: 10, paddingVertical: 10, fontSize: 14,
  },
  btn: {marginLeft:8, backgroundColor:'transparent', borderRadius:32, paddingHorizontal:14, paddingVertical:10, borderWidth: 1, borderColor: '#45EBE9'},
  btnT: {color:'#ffffff', fontWeight:'700', fontSize:13, letterSpacing: 1.17},
  sendStatus: {color:'#45EBE9', fontSize:14, fontWeight:'700', marginHorizontal:4},
  sendFailed: {color:'#f85149'},
  // PB.7 Input-A: read-only indicator bar shown when terminal input is denied.
  readOnlyBar: { backgroundColor: 'rgba(248, 81, 73, 0.12)', borderTopWidth: 1, borderTopColor: '#f85149', paddingHorizontal: 12, paddingVertical: 8, alignItems: 'center' },
  readOnlyText: { color: '#f85149', fontSize: 12, fontWeight: '700', letterSpacing: 0.5 },

  activityContainer: {
    flex: 1,
    backgroundColor: '#000',
  },
  activityList: {
    paddingVertical: 16,
  },
  emptyActivityText: {
    color: '#8b949e',
    textAlign: 'center',
    marginTop: 40,
    fontSize: 14,
  },
  // E8g2: Transcript mode styles — full-width, readable document layout.
  transcriptInfo: { backgroundColor: '#0D2D45', padding: 8, alignItems: 'center' },
  transcriptInfoText: { color: '#8b949e', fontSize: 10, textAlign: 'center' },
  transcriptContainer: {
    flex: 1,
    backgroundColor: '#000',
  },
  newOutputBanner: {
    backgroundColor: '#1E91B3',
    padding: 10,
    alignItems: 'center',
  },
  newOutputText: {
    color: '#fff',
    fontWeight: '700',
    fontSize: 12,
  },
  transcriptList: {
    paddingHorizontal: 0,
    paddingVertical: 8,
  },
  transcriptOutputBlock: {
    paddingHorizontal: 12,
    paddingVertical: 6,
  },
  transcriptOutputScroll: {
    // no width constraint — content determines width
  },
  transcriptOutputText: {
    color: '#ccc',
    fontSize: 13,
    fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace',
    lineHeight: 20,
    // E8i: preserve terminal-width formatting; allow horizontal scroll
    // instead of forcing phone-width wrap that breaks code/agent output.
  },
  transcriptInputDivider: {
    height: 1,
    backgroundColor: '#0D2D45',
    marginHorizontal: 12,
    marginVertical: 8,
  },
  transcriptAgentLabel: {
    color: '#45EBE9',
    fontSize: 10,
    fontWeight: '700',
    letterSpacing: 0.5,
    marginBottom: 4,
  },
  transcriptDegradedBlock: {
    paddingHorizontal: 12,
    paddingVertical: 8,
    backgroundColor: 'rgba(248, 81, 73, 0.08)',
    marginHorizontal: 8,
    marginVertical: 4,
    borderRadius: 4,
  },
  transcriptDegradedText: {
    color: '#f85149',
    fontSize: 12,
    fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace',
  },
  fallbackSection: {
    borderTopWidth: 1,
    borderTopColor: '#555',
    marginTop: 12,
    paddingTop: 8,
  },
  fallbackHeader: {
    color: '#999',
    fontSize: 10,
    fontWeight: '700',
    letterSpacing: 0.5,
    paddingHorizontal: 12,
    marginBottom: 8,
  },
  suppressedBanner: {
    backgroundColor: 'rgba(248, 181, 0, 0.1)',
    paddingHorizontal: 12,
    paddingVertical: 8,
    marginHorizontal: 8,
    marginBottom: 8,
    borderRadius: 4,
    borderWidth: 1,
    borderColor: 'rgba(248, 181, 0, 0.3)',
  },
  suppressedBannerText: {
    color: '#f8b500',
    fontSize: 11,
    textAlign: 'center',
  },
  returnBtn: {
    backgroundColor: '#1C1C1E',
    padding: 12,
    alignItems: 'center',
    borderTopWidth: 1,
    borderTopColor: '#0D2D45',
  },
  returnBtnText: {
    color: '#45EBE9',
    fontWeight: '700',
    fontSize: 13,
  },

  modalBg: {flex: 1, backgroundColor: 'rgba(0,0,0,0.8)', justifyContent: 'flex-end'},
  modalContainer: {height: '80%', backgroundColor: '#0D2D45', borderTopLeftRadius: 16, borderTopRightRadius: 16, padding: 16},
  modalHeader: {flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12},
  modalTitle: {color: '#ffffff', fontSize: 16, fontWeight: '800', letterSpacing: 1.2},
  closeBtn: {color: '#45EBE9', fontSize: 13, fontWeight: '700', letterSpacing: 1.17},
  modalScroll: {flex: 1, backgroundColor: '#000000', borderRadius: 4, padding: 12, borderWidth: 1, borderColor: '#1E91B3'},
  modalText: {color: '#45EBE9', fontSize: 13, fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace'},
  modalFooter: {marginTop: 12, alignItems: 'center'},
  modalHint: {color: '#1E91B3', fontSize: 12, fontWeight: '700', letterSpacing: 0.96},
  floatingHistoryBtn: {
    position: 'absolute',
    top: 10,
    right: 10,
    backgroundColor: 'rgba(13, 45, 69, 0.8)',
    paddingHorizontal: 12,
    paddingVertical: 6,
    borderRadius: 20,
    borderWidth: 1,
    borderColor: '#1E91B3',
    flexDirection: 'row',
    alignItems: 'center'
  },
  floatingHistoryText: {
    color: '#45EBE9',
    fontSize: 12,
    fontWeight: '700'
  },
});
