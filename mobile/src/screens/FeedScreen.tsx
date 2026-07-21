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
}

function jsSend(chars: number[]): string {
  const arr = JSON.stringify(chars);
  return `window.ws.send(String.fromCharCode.apply(null, ${arr}))`;
}

const NORMAL_MACROS: { label: string; chars: number[] }[] = [
  { label: 'Ctrl+C',  chars: [3] },
  { label: 'Esc',     chars: [27] },
  { label: 'Tab',     chars: [9] },
  { label: '↑',       chars: [27, 91, 65] },
  { label: '↓',       chars: [27, 91, 66] },
  { label: '←',       chars: [27, 91, 68] },
  { label: '→',       chars: [27, 91, 67] },
  { label: 'Y',       chars: [121, 13] },
  { label: 'N',       chars: [110, 13] },
  { label: 'Enter',   chars: [13] },
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

function LegacyFeedScreen({onBack, session, token, authCtx}: Props) {
  const { tokenMgr, baseURL, termURI } = deriveTerminalAuth(authCtx, session);
  const wv = useRef<any>(null);
  const cmdRef = useRef('');
  const [cmd, setCmd] = useState('');
  const [kbHeight, setKbHeight] = useState(0);

  // PA3 Step 1: two modes — Live Terminal, Transcript (read-only). Activity removed.
  const [activeTab, setActiveTab] = useState<'terminal' | 'transcript'>('transcript');
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
  const [sessionData, setSessionData] = useState<SessionTelemetry | null>(null);
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
  const [lifecycleState, setLifecycleState] = useState<ManagedLifecycleState>('unknown');
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
    setLifecycleState('unknown');
    setPendingAction(null);
    setActionError('');
    setTranscriptEvents([]);
    setFallbackEvents([]);
    setByteStreamSuppressed(false);
    transcriptMaxSeqRef.current = 0;
    lastSeenSeqRef.current = 0;
    transcriptGenRef.current = 0;
    sessionGenRef.current++;
    lifecycleCtrlRef.current?.setSession(session, token);
  }, [session, token]);

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
  const [sendStatus, setSendStatus] = useState<'idle' | 'sending' | 'sent' | 'failed'>('idle');
  // PB.7 Input-A: server-issued read-only state. When the daemon rejects
  // terminal input (missing terminal:input permission) it sends a bounded
  // read_only denial. The mobile surfaces this as a visible read-only reason
  // instead of reporting send success after silent discard.
  const [readOnlyReason, setReadOnlyReason] = useState('');

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
  const doSend = useCallback((text: string) => {
    if (!text || !wv.current) {
      setSendStatus('failed');
      return;
    }
    const parsedText = text.replace(/\n/g, '\r');
    setSendStatus('sending');
    inject(
      '(function(){' +
      'var w=window.ws;' +
      'if(!w||w.readyState!==1){' +
      'window.ReactNativeWebView.postMessage(JSON.stringify({type:"sendStatus",status:"failed"}));' +
      'return;' +
      '}' +
      'try{' +
      // M3-auth-4A framing contract: raw input goes out as a BINARY frame via
      // the page's single sender. Fall back to an inline binary encode if the
      // page helper is not yet defined (still binary — never a text frame).
      'if(window.pokitSendInput){window.pokitSendInput(' + JSON.stringify(parsedText) + ');}' +
      'else{w.send(new TextEncoder().encode(' + JSON.stringify(parsedText) + '));}' +
      'window.ReactNativeWebView.postMessage(JSON.stringify({type:"sendStatus",status:"sent"}));' +
      '}catch(e){' +
      'window.ReactNativeWebView.postMessage(JSON.stringify({type:"sendStatus",status:"failed"}));' +
      '}' +
      '})()'
    );
  }, [inject]);

  // submitLine sends the text and Enter as TWO separate messages. Codex treats
  // a trailing \r bundled with the text as a literal newline (paste heuristic),
  // so combined "text\r" inserts a newline instead of submitting; a standalone
  // \r submits — which is why the custom Enter macro ([13]) worked but Send did
  // not. Splitting makes Send equivalent to "type text, then press Enter". The
  // small delay keeps the \r in a separate PTY read so Codex sees a discrete
  // Enter keypress. Text goes out once, \r once — no accumulated-buffer bug.
  const submitLine = useCallback((text: string) => {
    if (text) doSend(text);
    setTimeout(() => doSend('\r'), 40);
  }, [doSend]);

  const send = useCallback(() => {
    const currentCmd = cmdRef.current || cmd;
    if (!currentCmd.trim()) return;
    submitLine(currentCmd);
    setCmd('');
    cmdRef.current = '';
  }, [submitLine, session]);
  const sendMacro = useCallback((chars: number[]) => {
    // E8: macros use same doSend ack path.
    const macroText = String.fromCharCode.apply(null, chars);
    doSend(macroText);
  }, [doSend]);

  const handleChangeText = useCallback((text: string) => {
    cmdRef.current = text;
    // Soft keyboard Enter may insert \n without firing onSubmitEditing.
    // Detect trailing \n, strip it, and send the command.
    if (text.endsWith('\n')) {
      const cmd = text.replace(/\n$/, '');
      setCmd('');
      cmdRef.current = '';
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
    const text = await Clipboard.getStringAsync();
    if (text) {
      doSend(text);
    }
  }, [doSend]);



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
        setSendStatus(data.status === 'sent' ? 'sent' : 'failed');
        if (data.status === 'sent') {
          setTimeout(() => setSendStatus(s => s === 'sent' ? 'idle' : s), 1500);
        }
      }
      // PB.7 Input-A: server read_only denial — surfaces permission reason from daemon.
      if (data.type === 'read_only') {
        setReadOnlyReason(data.reason || 'Read only');
        setSendStatus('failed');
        setTimeout(() => setReadOnlyReason(''), 5000);
      }
      if (data.type === 'e8diag') {
        console.log('E8DIAG', JSON.stringify(data));
      }
      // M3-auth-4A reconnect bridge.
      if (data?.type === 'pokit-reconnect-request') {
        handleReconnect(data);
      }
    } catch (e) {}
  }, [handleReconnect]);

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

      // Override fitTerminal: mirror the PTY's real geometry so full-width
      // TUIs render without wrapping (Claude draws to the alternate screen at
      // the PTY width; a fixed 100-col xterm made its wide output re-wrap on
      // the phone). We fetch the live PTY size and resize xterm to match; the
      // container scrolls for overflow (see CSS). The mobile viewer never
      // resizes the shared PTY — it only matches it. Falls back to a >=100-col
      // viewport fit if the size can't be fetched.
      window.fitTerminal = function() {
        if (!window.term) return;
        var apply = function(cols, rows) {
          if (cols > 0 && rows > 0 &&
              (window.term.cols !== cols || window.term.rows !== rows)) {
            window.term.resize(cols, rows);
          }
        };
        var h = document.getElementById('t').clientHeight;
        var w = document.getElementById('t').clientWidth;
        var span = document.createElement('span');
        span.textContent = 'W';
        span.style.fontFamily = window.term.options.fontFamily;
        span.style.fontSize = window.term.options.fontSize + 'px';
        span.style.visibility = 'hidden';
        document.body.appendChild(span);
        var cw = span.getBoundingClientRect().width;
        var ch = span.getBoundingClientRect().height;
        document.body.removeChild(span);
        var fbCols = Math.max(Math.floor(w / cw), 100);
        var fbRows = Math.floor(h / ch);

        var p = new URLSearchParams(location.search);
        var sess = p.get('session');
        var tok = p.get('token');
        var hdrs = {};
        if (tok) hdrs['Authorization'] = 'Bearer ' + tok;
        fetch('/term/size?session=' + encodeURIComponent(sess), { headers: hdrs })
          .then(function(r) { return r.ok ? r.json() : null; })
          .then(function(sz) {
            if (sz && sz.cols > 0 && sz.rows > 0) apply(sz.cols, sz.rows);
            else apply(fbCols, fbRows);
          })
          .catch(function() { apply(fbCols, fbRows); });
      };

      window.addEventListener('resize', function() {
        if (window.fitTerminal) window.fitTerminal();
      });

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
            window.fitTerminal();
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

      // Force an initial resize when this script is loaded
      setTimeout(function() {
        if (window.fitTerminal) window.fitTerminal();
      }, 500);

      // Poll the PTY size so the mobile view tracks host-terminal resizes
      // (e.g. the user widens Terminal.app while attached). Re-applies only on
      // an actual change (fitTerminal guards term.resize).
      setInterval(function() {
        if (window.fitTerminal) window.fitTerminal();
      }, 3000);
      
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
                <TouchableOpacity onPress={handlePasteRequest} style={styles.textBtn}><Text style={styles.textBtnText}>PASTE</Text></TouchableOpacity>
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

        {/* M3b: input (keystrokes + macros) is hidden when a MANAGED session is
            not in a state that accepts input (starting/stopping/terminal, or no
            input capability). Non-managed sessions keep their existing behavior. */}
        {/* M3b (BLOCKER 4): input (keystrokes + macros) is gated by the policy for
            EVERY session — never a non-managed bypass. External adapters that
            declare `input` keep input; observe-only/unknown/view-only
            (missing/unknown capabilities) never show input or macros. */}
        {/* PB.7 Input-A: read-only indicator when input is disabled by server policy */}
        {activeTab === 'terminal' && !sessionEnded && !actionPolicy.inputEnabled && (
          <View style={styles.readOnlyBar}>
            <Text style={styles.readOnlyText}>
              {readOnlyReason || 'View only — terminal input not available'}
            </Text>
          </View>
        )}
        {readOnlyReason !== '' && actionPolicy.inputEnabled && (
          <View style={styles.readOnlyBar}>
            <Text style={styles.readOnlyText}>{readOnlyReason}</Text>
          </View>
        )}
        {activeTab === 'terminal' && !sessionEnded && actionPolicy.inputEnabled && (
        <>
        <View style={styles.macroContainer}>
          <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={styles.macroScroll}>
            {NORMAL_MACROS.map((m, i) => (
              <TouchableOpacity key={i} style={styles.macroBtn} onPress={() => sendMacro(m.chars)}>
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
          />
          {/* E8: input delivery status indicator */}
          {sendStatus !== 'idle' && (
            <Text style={[styles.sendStatus, sendStatus === 'failed' && styles.sendFailed]}>
              {sendStatus === 'sending' ? '↑' : sendStatus === 'sent' ? '✓' : '✗'}
            </Text>
          )}
          <TouchableOpacity onPress={send} style={styles.btn}><Text style={styles.btnT}>Send</Text></TouchableOpacity>
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
