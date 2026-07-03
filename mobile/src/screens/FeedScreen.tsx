import React, {useRef, useState, useCallback} from 'react';
import {View, Text, TextInput, StyleSheet, TouchableOpacity, ScrollView, KeyboardAvoidingView, Platform} from 'react-native';
import {WebView} from 'react-native-webview';
import {SafeAreaView} from 'react-native-safe-area-context';

interface Props {
  onBack: () => void;
  session: string;
}

// Macro code → JavaScript that sends raw bytes via WebSocket
function jsSend(chars: number[]): string {
  const arr = JSON.stringify(chars);
  return `var a=${arr};for(var i=0;i<a.length;i++)window.ws.send(String.fromCharCode(a[i]))`;
}

const MACROS: { label: string; chars: number[] }[] = [
  { label: 'Ctrl+C',  chars: [3] },                  // ETX
  { label: 'C+C x2',  chars: [3, 3] },               // double Ctrl+C for stubborn processes
  { label: 'Ctrl+D',  chars: [4] },                  // EOT / EOF
  { label: 'Esc',     chars: [27] },                 // ESC
  { label: 'Tab',     chars: [9] },                  // TAB
  { label: '↑',       chars: [27, 91, 65] },         // ESC [ A
  { label: '↓',       chars: [27, 91, 66] },         // ESC [ B
  { label: '←',       chars: [27, 91, 68] },         // ESC [ D
  { label: '→',       chars: [27, 91, 67] },         // ESC [ C
  { label: 'Y',       chars: [121, 10] },            // y + Enter
  { label: 'N',       chars: [110, 10] },            // n + Enter
  { label: 'Enter',   chars: [10] },                 // LF
];

export default function FeedScreen({onBack, session}: Props) {
  const wv = useRef<any>(null);
  const [cmd, setCmd] = useState('');

  const termUrl = `https://term.fullcount.kr/term/?session=${encodeURIComponent(session)}`;

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

  return (
    <SafeAreaView style={styles.container}>
      <KeyboardAvoidingView
        style={styles.flex}
        behavior={Platform.OS === 'ios' ? 'padding' : undefined}
      >
        <View style={styles.header}>
          <TouchableOpacity onPress={onBack}><Text style={styles.backBtn}>←</Text></TouchableOpacity>
          <Text style={styles.headerTitle}>{session}</Text>
          <View style={{width: 24}} />
        </View>

        <WebView
          ref={wv}
          source={{uri: termUrl}}
          style={styles.webview}
          javaScriptEnabled
          domStorageEnabled
          originWhitelist={['*']}
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

        <View style={styles.inputContainer}>
          <TextInput
            style={styles.input}
            placeholder="$ type a command..."
            placeholderTextColor="#666"
            value={cmd}
            onChangeText={setCmd}
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
  webview: {flex:1, backgroundColor:'#000'},
  macroContainer: { backgroundColor: '#161b22', borderTopWidth: 1, borderTopColor: '#30363d' },
  macroScroll: { paddingHorizontal: 6, paddingVertical: 6, alignItems: 'center' },
  macroBtn: { backgroundColor: '#21262d', paddingHorizontal: 10, paddingVertical: 6, borderRadius: 6, marginRight: 5, borderWidth: 1, borderColor: '#30363d' },
  macroText: { color: '#c9d1d9', fontSize: 12, fontWeight: '600' },
  inputContainer: {
    flexDirection: 'row', padding: 6, backgroundColor: '#161b22',
    borderTopWidth: 1, borderTopColor: '#30363d', alignItems: 'center',
    paddingBottom: Platform.OS === 'ios' ? 20 : 6,
  },
  input: {
    flex:1, backgroundColor: '#21262d', color: '#c9d1d9', borderRadius: 6,
    paddingHorizontal: 10, paddingVertical: 10, fontSize: 14,
  },
  btn: {marginLeft:8, backgroundColor:'#238636', borderRadius:6, paddingHorizontal:14, paddingVertical:10},
  btnT: {color:'#fff', fontWeight:'600', fontSize:13},
});
