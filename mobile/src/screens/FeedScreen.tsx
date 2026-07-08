import React, {useRef, useState, useCallback, useEffect, useMemo} from 'react';
import { terminalURL, listSessions, getSessionHistory, getActivityHistory, ConnectivityFailure, PokitError } from '../lib/client';
import {View, Text, TextInput, StyleSheet, TouchableOpacity, ScrollView, Platform, Keyboard, Modal, FlatList, ActivityIndicator} from 'react-native';
import {WebView} from 'react-native-webview';
import {SafeAreaView} from 'react-native-safe-area-context';
import * as Clipboard from 'expo-clipboard';
import { EventBubble } from '../components/EventBubble';
import { SessionTelemetry } from '../components/AgentCard';

interface Props {
  onBack: () => void;
  session: string;
  token?: string;
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



export default function FeedScreen({onBack, session, token}: Props) {
  const wv = useRef<any>(null);
  const cmdRef = useRef('');
  const [cmd, setCmd] = useState('');
  const [kbHeight, setKbHeight] = useState(0);

  // E8g: three modes — Live Terminal, Activity Feed, Transcript (read-only).
  const [activeTab, setActiveTab] = useState<'terminal' | 'activity' | 'transcript'>('activity');
  const activeTabRef = useRef(activeTab);
  const [transcriptEvents, setTranscriptEvents] = useState<any[]>([]);
  const [newOutputCount, setNewOutputCount] = useState(0);
  const lastSeenSeqRef = useRef(0);
  const transcriptMaxSeqRef = useRef(0);
  const [sessionData, setSessionData] = useState<SessionTelemetry | null>(null);
  // Legacy (capabilities===undefined) is treated as no-history for safety.
  // Only sessions explicitly advertising 'history' get the ACTIVITY tab.
  const supportsHistory = !!(sessionData?.capabilities?.includes('history'));
  const sessionDataRef = useRef(sessionData);
  sessionDataRef.current = sessionData;
  const [sessionEnded, setSessionEnded] = useState(false);

  // When history is unsupported, force-switch to terminal tab.
  useEffect(() => {
    if (!supportsHistory && activeTab === 'activity') {
      setActiveTab('terminal');
    }
  }, [supportsHistory, activeTab]);
  useEffect(() => { activeTabRef.current = activeTab; }, [activeTab]);
  const [historyEvents, setHistoryEvents] = useState<any[]>([]);
  // E8: input delivery status — idle | sending | sent | failed
  const [sendStatus, setSendStatus] = useState<'idle' | 'sending' | 'sent' | 'failed'>('idle');

  // R1a: activity/transcript endpoint reachability.
  const [activityError, setActivityError] = useState('');
  const [historyError, setHistoryError] = useState('');
  // R1a: terminal WebView connection failure.
  const [terminalError, setTerminalError] = useState('');
  const [terminalErrorDetail, setTerminalErrorDetail] = useState('');

  const [copyModalVisible, setCopyModalVisible] = useState(false);
  const [copyText, setCopyText] = useState('');

  const termUrl = useMemo(() => terminalURL(session, token), [session, token]);
  const source = useMemo(() => ({uri: termUrl}), [termUrl]);

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
            setSessionEnded(false);
          } else {
            setSessionEnded(true);
          }
        })
        .catch(err => console.error(err));
    };
    
    const fetchHistory = () => {
      getSessionHistory(session, token)
        .then(data => {
          if (Array.isArray(data)) {
            setHistoryError('');
            setHistoryEvents(prev => {
              // E8: merge server events with local synthetics, dedup by ID.
              const seen = new Set<string>();
              const merged: any[] = [];
              // Server events first (source of truth), then keep local-only synthetics.
              for (const e of [...(data || []), ...prev]) {
                if (!e.id || seen.has(e.id)) continue;
                seen.add(e.id);
                merged.push(e);
              }
              // Sort by timestamp ascending.
              merged.sort((a, b) => (a.timestamp || '').localeCompare(b.timestamp || ''));
              return merged;
            });
          }
        })
        .catch(err => {
          console.error(err);
          if (err instanceof PokitError) {
            setHistoryError(err.failure === ConnectivityFailure.NetworkUnreachable
              ? 'Activity endpoint unreachable.'
              : 'Activity endpoint error (' + err.statusCode + ').');
          } else {
            setHistoryError('Activity endpoint unreachable.');
          }
        });
    };

    // E8g: fetch transcript — ref-based to avoid stale closure on activeTab.
    const fetchTranscript = () => {
      getActivityHistory(session, token)
        .then(data => {
          if (Array.isArray(data)) {
            setTranscriptEvents(data);
            setActivityError('');
            const maxSeq = data.reduce((m: number, e: any) => Math.max(m, e.seq || 0), 0);
            transcriptMaxSeqRef.current = maxSeq;
            if (activeTabRef.current === 'transcript' && maxSeq > lastSeenSeqRef.current) {
              setNewOutputCount(maxSeq - lastSeenSeqRef.current);
            }
            if (activeTabRef.current !== 'transcript') {
              lastSeenSeqRef.current = maxSeq;
            }
          }
        })
        .catch(err => {
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
    if (sessionDataRef.current?.capabilities?.includes('history')) {
      fetchHistory();
    }
    fetchTranscript();
    const interval = setInterval(() => {
      fetchSession();
      if (sessionDataRef.current?.capabilities?.includes('history')) {
        fetchHistory();
      }
      fetchTranscript();
    }, 3000);
    return () => clearInterval(interval);
  }, [session]);

  const inject = useCallback((js: string) => {
    if (wv.current) {
      wv.current.injectJavaScript(js + ';true;');
    }
  }, []);

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
      'try{w.send(' + JSON.stringify(parsedText) + ');' +
      'window.ReactNativeWebView.postMessage(JSON.stringify({type:"sendStatus",status:"sent"}));' +
      '}catch(e){' +
      'window.ReactNativeWebView.postMessage(JSON.stringify({type:"sendStatus",status:"failed"}));' +
      '}' +
      '})()'
    );
  }, [inject]);

  const send = useCallback(() => {
    if (!cmd.trim()) return;
    const textToSend = cmd;
    doSend(textToSend + '\r');
    setCmd('');

    setHistoryEvents(prev => {
      const optEvent = {
        id: 'opt-' + Date.now(),
        session: session,
        type: 'user',
        summary: 'User',
        detail: textToSend,
        timestamp: new Date().toISOString()
      };
      return [...prev, optEvent];
    });
  }, [cmd, doSend, session]);
  const sendMacro = useCallback((chars: number[]) => {
    // E8: macros use same doSend ack path.
    const macroText = String.fromCharCode.apply(null, chars);
    doSend(macroText);
  }, [doSend]);

  const handleChangeText = useCallback((text: string) => {
    cmdRef.current = text;
    if (text.endsWith('\n')) {
      doSend(text.replace(/\n/g, '') + '\r');
      setCmd('');
    } else {
      setCmd(text);
    }
  }, [doSend]);

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



  const onMessage = useCallback((event: any) => {
    try {
      const data = JSON.parse(event.nativeEvent.data);
      if (data.type === 'copy') {
        setCopyText(data.text);
        setCopyModalVisible(true);
      }
      // E8: handle real send ack from WebView.
      if (data.type === 'sendStatus') {
        setSendStatus(data.status === 'sent' ? 'sent' : 'failed');
        if (data.status === 'sent') {
          setTimeout(() => setSendStatus(s => s === 'sent' ? 'idle' : s), 1500);
        }
      }
      // E8: diagnostic instrumentation from terminal.
      if (data.type === 'e8diag') {
        console.log('E8DIAG', JSON.stringify(data));
      }
    } catch (e) {}
  }, []);

  const pinchZoomInjection = `
    (function() {
      let initialDistance = null;
      let initialFontSize = null;

      const style = document.createElement('style');
      style.innerHTML = '.xterm-viewport { } .xterm-screen { user-select: text; -webkit-user-select: text; }';
      document.head.appendChild(style);

      // Override fitTerminal to be accurate and actually call term.resize()
      window.fitTerminal = function() {
        if (!window.term) return;
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

        var rows = Math.floor(h / ch);
        var cols = Math.floor(w / cw);
        if (rows > 0 && cols > 0) {
          window.term.resize(cols, rows);
          var p = new URLSearchParams(location.search);
          var sess = p.get('session');
          var tok = p.get('token');
          var hdrs = {};
          if(tok) hdrs['Authorization']='Bearer '+tok;
          fetch('/term/size?session='+encodeURIComponent(sess)+'&rows='+rows+'&cols='+cols, {method:'POST',headers:hdrs}).catch(function(){});
        }
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
      
      true;
    })();
  `;

  return (
    <SafeAreaView style={styles.container}>
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

        <View style={styles.tabBar}>
          <TouchableOpacity 
            style={[styles.tab, activeTab === 'terminal' && styles.activeTab]} 
            onPress={() => setActiveTab('terminal')}
          >
            <Text style={[styles.tabText, activeTab === 'terminal' && styles.activeTabText]}>TERMINAL</Text>
          </TouchableOpacity>
          {supportsHistory && (
          <TouchableOpacity
            style={[styles.tab, activeTab === 'activity' && styles.activeTab]}
            onPress={() => setActiveTab('activity')}
          >
            <Text style={[styles.tabText, activeTab === 'activity' && styles.activeTabText]}>ACTIVITY</Text>
          </TouchableOpacity>
          )}
          {/* E8g: Transcript tab — read-only terminal activity history. */}
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
              source={source}
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

        <View style={[styles.activityContainer, {display: activeTab === 'activity' ? 'flex' : 'none'}]}>
          {/* R1a: activity endpoint error state */}
          {historyError ? (
            <View style={{padding: 20, alignItems: 'center'}}>
              <Text style={{color: '#f85149', fontSize: 13, textAlign: 'center', marginBottom: 8}}>{historyError}</Text>
              <Text style={{color: '#8b949e', fontSize: 11, textAlign: 'center'}}>Activity history may still be loading. Pull to refresh.</Text>
            </View>
          ) : !historyEvents || historyEvents.length === 0 ? (
            <ActivityIndicator size="large" color="#45EBE9" style={{ marginTop: 40 }} />
          ) : (
            <FlatList
              data={historyEvents.slice().reverse()}
              keyExtractor={(item, idx) => item.id || String(idx)}
              contentContainerStyle={styles.activityList}
              renderItem={({ item }) => (
                <EventBubble event={item} runnerId={sessionData?.runner} runnerColor={sessionData?.runnerColor} agentKind={sessionData?.agentKind} />
              )}
              ListEmptyComponent={
                <Text style={styles.emptyActivityText}>No activity recorded yet.</Text>
              }
              inverted={true}
            />
          )}
        </View>

        {/* E8h: Transcript Mode — best-effort readable history. Not a terminal emulator. */}
        <View style={[styles.transcriptContainer, {display: activeTab === 'transcript' ? 'flex' : 'none'}]}>
          <View style={styles.transcriptInfo}>
            <Text style={styles.transcriptInfoText}>Read-only history. Live Terminal is source of truth for interactive/TUI.</Text>
          </View>
          {newOutputCount > 0 && (
            <TouchableOpacity style={styles.newOutputBanner} onPress={() => { lastSeenSeqRef.current = transcriptMaxSeqRef.current; setNewOutputCount(0); setActiveTab('terminal'); }}>
              <Text style={styles.newOutputText}>↓ New output — Return to Live Terminal</Text>
            </TouchableOpacity>
          )}
          {/* R1a: transcript endpoint error state */}
          {activityError ? (
            <View style={{padding: 20, alignItems: 'center'}}>
              <Text style={{color: '#f85149', fontSize: 13, textAlign: 'center', marginBottom: 8}}>{activityError}</Text>
              <Text style={{color: '#8b949e', fontSize: 11, textAlign: 'center'}}>Transcript may still be loading. Retrying automatically.</Text>
            </View>
          ) : !transcriptEvents || transcriptEvents.length === 0 ? (
            <Text style={styles.emptyActivityText}>No transcript yet.</Text>
          ) : (
            <FlatList
              data={transcriptEvents}
              keyExtractor={(item) => String(item.seq || item.id || '0')}
              contentContainerStyle={styles.activityList}
              inverted={true}
              renderItem={({ item }) => (
                item.type === 'terminal_input' ? (
                  <View style={styles.transcriptInputRow}>
                    <Text style={styles.transcriptInputLabel}>[input sent]</Text>
                  </View>
                ) : (
                  <View style={styles.transcriptOutputRow}>
                    <Text style={styles.transcriptOutputText} selectable={true}>
                      {item.text}
                    </Text>
                  </View>
                )
              )}
            />
          )}
          <TouchableOpacity style={styles.returnBtn} onPress={() => { lastSeenSeqRef.current = transcriptMaxSeqRef.current; setNewOutputCount(0); setActiveTab('terminal'); }}>
            <Text style={styles.returnBtnText}>← Return to Live Terminal</Text>
          </TouchableOpacity>
        </View>

        {activeTab === 'terminal' && (
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
  // E8g: Transcript mode styles.
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
  transcriptOutputRow: { paddingHorizontal: 12, paddingVertical: 1 },
  transcriptOutputText: { color: '#ccc', fontSize: 11, fontFamily: 'monospace', lineHeight: 16 },
  transcriptInputRow: { paddingHorizontal: 12, paddingVertical: 2, backgroundColor: '#0D2D45' },
  transcriptInputLabel: { color: '#45EBE9', fontSize: 10, fontFamily: 'monospace' },
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
