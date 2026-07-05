import { config } from '../config';
import React, {useRef, useState, useCallback, useEffect, useMemo} from 'react';
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

  const [activeTab, setActiveTab] = useState<'terminal' | 'activity'>('activity');
  const [sessionData, setSessionData] = useState<SessionTelemetry | null>(null);
  const [historyEvents, setHistoryEvents] = useState<any[]>([]);

  const [copyModalVisible, setCopyModalVisible] = useState(false);
  const [copyText, setCopyText] = useState('');

  const termUrl = useMemo(() => {
    let url = `${config.BASE_URL}/term/?session=${encodeURIComponent(session)}`;
    if (token) {
      url += `&token=${encodeURIComponent(token)}`;
    }
    return url;
  }, [session, token]);
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
      fetch(`${config.BASE_URL}/api/sessions`, { headers })
        .then(res => res.json())
        .then(data => {
          const sess = data.find((s: any) => (s.id || s) === session);
          if (sess && typeof sess !== 'string') {
            setSessionData(sess);
          }
        })
        .catch(err => console.error(err));
    };
    
    const fetchHistory = () => {
      fetch(`${config.BASE_URL}/api/sessions?history=${encodeURIComponent(session)}`, { headers })
        .then(res => res.json())
        .then(data => {
          if (Array.isArray(data)) {
            setHistoryEvents(data);
          }
        })
        .catch(err => console.error(err));
    };

    fetchSession();
    fetchHistory();
    const interval = setInterval(() => {
      fetchSession();
      fetchHistory();
    }, 3000);
    return () => clearInterval(interval);
  }, [session]);

  const inject = useCallback((js: string) => {
    if (wv.current) {
      wv.current.injectJavaScript(js + ';true;');
    }
  }, []);

  const doSend = useCallback((text: string) => {
    if (!text || !wv.current) return;
    const parsedText = text.replace(/\n/g, '\r');
    inject('if(window.ws&&window.ws.readyState===1)window.ws.send('+JSON.stringify(parsedText)+')');
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
    const jsSend = `window.ws.send(String.fromCharCode.apply(null, [${chars.join(',')}]));`;
    inject('if(window.ws&&window.ws.readyState===1){'+jsSend+'}');
  }, [inject]);

  const handleChangeText = useCallback((text: string) => {
    cmdRef.current = text;
    if (text.endsWith('\n')) {
      doSend(text.replace(/\n/g, '') + '\r');
      setCmd('');
    } else {
      setCmd(text);
    }
  }, [doSend]);

  const handleCopyRequest = useCallback(() => {
    inject('if(window.getTerminalText) window.getTerminalText();');
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
    } catch (e) {}
  }, []);

  const pinchZoomInjection = `
    (function() {
      let initialDistance = null;
      let initialFontSize = null;

      const style = document.createElement('style');
      style.innerHTML = '.xterm-viewport { overflow: hidden !important; } .xterm-screen { }';
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
          <TouchableOpacity 
            style={[styles.tab, activeTab === 'activity' && styles.activeTab]} 
            onPress={() => setActiveTab('activity')}
          >
            <Text style={[styles.tabText, activeTab === 'activity' && styles.activeTabText]}>ACTIVITY</Text>
          </TouchableOpacity>
        </View>

        <View style={{flex: 1, display: activeTab === 'terminal' ? 'flex' : 'none'}}>
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
          />
        </View>

        <View style={[styles.activityContainer, {display: activeTab === 'activity' ? 'flex' : 'none'}]}>
          {!historyEvents || historyEvents.length === 0 ? (
            <ActivityIndicator size="large" color="#45EBE9" style={{ marginTop: 40 }} />
          ) : (
            <FlatList
              data={historyEvents.slice().reverse()}
              keyExtractor={(item, idx) => item.id || String(idx)}
              contentContainerStyle={styles.activityList}
              renderItem={({ item }) => (
                <EventBubble event={item} runnerId={sessionData?.runner} runnerColor={sessionData?.runnerColor} />
              )}
              ListEmptyComponent={
                <Text style={styles.emptyActivityText}>No activity recorded yet.</Text>
              }
              inverted={true}
            />
          )}
        </View>

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
          <TouchableOpacity onPress={send} style={styles.btn}><Text style={styles.btnT}>Send</Text></TouchableOpacity>
        </View>
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
