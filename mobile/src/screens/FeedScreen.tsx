import React, {useRef, useState, useCallback, useEffect, useMemo} from 'react';
import {View, Text, TextInput, StyleSheet, TouchableOpacity, ScrollView, KeyboardAvoidingView, Platform, Keyboard, Animated} from 'react-native';
import {WebView} from 'react-native-webview';
import {SafeAreaView} from 'react-native-safe-area-context';

interface Props {
  onBack: () => void;
  session: string;
}

function jsSend(chars: number[]): string {
  const arr = JSON.stringify(chars);
  return `var a=${arr};for(var i=0;i<a.length;i++)window.ws.send(String.fromCharCode(a[i]))`;
}

const MACROS: { label: string; chars: number[] }[] = [
  { label: 'Ctrl+C',  chars: [3] },
  { label: 'C+C x2',  chars: [3, 3] },
  { label: 'Ctrl+D',  chars: [4] },
  { label: 'Esc',     chars: [27] },
  { label: 'Tab',     chars: [9] },
  { label: '↑',       chars: [27, 91, 65] },
  { label: '↓',       chars: [27, 91, 66] },
  { label: '←',       chars: [27, 91, 68] },
  { label: '→',       chars: [27, 91, 67] },
  { label: 'Y',       chars: [121, 10] },
  { label: 'N',       chars: [110, 10] },
  { label: 'Enter',   chars: [10] },
];

export default function FeedScreen({onBack, session}: Props) {
  const wv = useRef<any>(null);
  const [cmd, setCmd] = useState('');
  const [kbHeight, setKbHeight] = useState(0);
  const [wsReady, setWsReady] = useState(false);

  const termUrl = useMemo(() => `https://term.fullcount.kr/term/?session=${encodeURIComponent(session)}`, [session]);
  const source = useMemo(() => ({uri: termUrl}), [termUrl]);

  // Track keyboard height on Android
  useEffect(() => {
    const show = Keyboard.addListener('keyboardDidShow', (e) => {
      setKbHeight(e.endCoordinates.height);
    });
    const hide = Keyboard.addListener('keyboardDidHide', () => {
      setKbHeight(0);
    });
    return () => { show.remove(); hide.remove(); };
  }, []);

  const inject = useCallback((js: string) => {
    if (wv.current) {
      wv.current.injectJavaScript(js + ';true;');
    }
  }, []);

  const send = useCallback(() => {
    if (cmd.trim() && wv.current) {
      inject('if(window.ws&&window.ws.readyState===1)window.ws.send('+JSON.stringify(cmd.trim()+'\n')+')');
      setCmd('');
    }
  }, [cmd, inject]);

  const sendMacro = useCallback((chars: number[]) => {
    inject('if(window.ws&&window.ws.readyState===1){'+jsSend(chars)+'}');
  }, [inject]);

  const handleMessage = useCallback((e: any) => {
    try {
      const msg = JSON.parse(e.nativeEvent.data);
      if (msg.type === 'ws') {
        setWsReady(msg.state === 1);
      }
    } catch {}
  }, []);

  // JS to inject after page load — detect WS state
  const onLoadScript = `
    setTimeout(function(){
      var s=window.ws?window.ws.readyState:-1;
      window.ReactNativeWebView.postMessage(JSON.stringify({type:'ws',state:s}));
      setInterval(function(){
        var ns=window.ws?window.ws.readyState:-1;
        window.ReactNativeWebView.postMessage(JSON.stringify({type:'ws',state:ns}));
      },3000);
    },1000);
    true;
  `;

  return (
    <SafeAreaView style={styles.container}>
      <KeyboardAvoidingView
        style={styles.flex}
        behavior={Platform.OS === 'ios' ? 'padding' : undefined}
        enabled={Platform.OS === 'ios'}
      >
        <View style={styles.header}>
          <TouchableOpacity onPress={onBack}><Text style={styles.backBtn}>←</Text></TouchableOpacity>
          <View style={{flexDirection:'row',alignItems:'center',gap:6}}>
            <View style={[styles.wsDot, wsReady ? styles.wsOn : styles.wsOff]} />
            <Text style={styles.headerTitle}>{session}</Text>
          </View>
          <TouchableOpacity onPress={() => {
            if (wv.current) wv.current.reload();
          }}><Text style={styles.reloadBtn}>↻</Text></TouchableOpacity>
        </View>

        <WebView
          ref={wv}
          source={source}
          style={styles.webview}
          javaScriptEnabled
          domStorageEnabled
          originWhitelist={['*']}
          onMessage={handleMessage}
          injectedJavaScript={onLoadScript}
          cacheEnabled={false}
        />

        <View style={styles.macroContainer}>
          <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={styles.macroScroll}>
            {MACROS.map((m, i) => (
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
            onChangeText={(text) => {
              if (text.endsWith('\n')) {
                setCmd('');
                if (wv.current) {
                  wv.current.injectJavaScript('if(window.ws&&window.ws.readyState===1)window.ws.send('+JSON.stringify(text.trim()+'\n')+');true;');
                }
              } else {
                setCmd(text);
              }
            }}
            onKeyPress={({nativeEvent}) => {
              if (nativeEvent.key === 'Enter') {
                send();
              }
            }}
            onSubmitEditing={send}
            returnKeyType="send"
            autoCorrect={false}
            autoCapitalize="none"
            multiline={false}
            blurOnSubmit={false}
          />
          <TouchableOpacity onPress={send} style={styles.btn}><Text style={styles.btnT}>Send</Text></TouchableOpacity>
        </View>
      </KeyboardAvoidingView>
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  container: {flex:1, backgroundColor:'#000'},
  flex: {flex:1},
  header: {
    flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center',
    paddingHorizontal: 12, paddingVertical: 10, borderBottomWidth: 1, borderBottomColor: '#1e212b',
  },
  headerTitle: {fontSize: 14, color: '#8b949e', fontWeight: '500'},
  backBtn: {fontSize: 24, color: '#fff'},
  reloadBtn: {fontSize: 20, color: '#58a6ff', paddingHorizontal: 4},
  wsDot: {width:6, height:6, borderRadius:3},
  wsOn: {backgroundColor:'#238636'},
  wsOff: {backgroundColor:'#f85149'},
  webview: {flex:1, backgroundColor:'#000'},
  macroContainer: { backgroundColor: '#161b22', borderTopWidth: 1, borderTopColor: '#30363d' },
  macroScroll: { paddingHorizontal: 6, paddingVertical: 6, alignItems: 'center' },
  macroBtn: { backgroundColor: '#21262d', paddingHorizontal: 10, paddingVertical: 6, borderRadius: 6, marginRight: 5, borderWidth: 1, borderColor: '#30363d' },
  macroText: { color: '#c9d1d9', fontSize: 12, fontWeight: '600' },
  inputContainer: {
    flexDirection: 'row', padding: 6, backgroundColor: '#161b22',
    borderTopWidth: 1, borderTopColor: '#30363d', alignItems: 'center',
  },
  input: {
    flex:1, backgroundColor: '#21262d', color: '#c9d1d9', borderRadius: 6,
    paddingHorizontal: 10, paddingVertical: 10, fontSize: 14,
  },
  btn: {marginLeft:8, backgroundColor:'#238636', borderRadius:6, paddingHorizontal:14, paddingVertical:10},
  btnT: {color:'#fff', fontWeight:'600', fontSize:13},
});
