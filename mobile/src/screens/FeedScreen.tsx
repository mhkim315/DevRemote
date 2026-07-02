import React, {useEffect, useRef, useCallback, useState} from 'react';
import {
  View, Text, TextInput, StyleSheet, TouchableOpacity, Platform,
} from 'react-native';
import {WebView} from 'react-native-webview';
import {SafeAreaView} from 'react-native-safe-area-context';
import type {Transport, TransportStatus} from '../services/types';

interface Props {
  transport: Transport;
  pushToken: string | null;
  onBack: () => void;
}

export default function FeedScreen({transport, pushToken, onBack}: Props) {
  const [status, setStatus] = useState<TransportStatus>(transport.status);
  const [stdin, setStdin] = useState('');
  const webViewRef = useRef<WebView>(null);
  const transportRef = useRef(transport);
  transportRef.current = transport;

  useEffect(() => {
    const unsubStatus = transport.onStatusChange(setStatus);
    const unsubAlert = transport.onAlert(a => {
      // Feed raw PTY output directly into xterm.js.
      if (a.type === 'pty' || a.type === 'raw') {
        const text = a.description || '';
        if (text && webViewRef.current) {
          webViewRef.current.injectJavaScript(
            `window.postMessage(${JSON.stringify(text)}, '*');true;`
          );
        }
      }
    });
    if (pushToken) transport.sendMessage({type: 'register', pushToken});
    return () => { unsubStatus(); unsubAlert(); };
  }, [transport, pushToken]);

  const onWebViewMessage = useCallback((event: any) => {
    try {
      const msg = JSON.parse(event.nativeEvent.data);
      if (msg.type === 'stdin' && msg.text) {
        transportRef.current.sendMessage({type: 'stdin', text: msg.text});
      }
    } catch {}
  }, []);

  const sendStdin = useCallback(() => {
    if (stdin.trim()) {
      const text = stdin.trim() + '\n';
      transportRef.current.sendMessage({type: 'stdin', text});
      // Also write directly to the terminal WebView for local echo.
      if (webViewRef.current) {
        webViewRef.current.injectJavaScript(
          `window.postMessage(${JSON.stringify(text)}, '*');true;`
        );
      }
      setStdin('');
    }
  }, [stdin]);

  return (
    <SafeAreaView style={styles.container}>
      {/* Header */}
      <View style={styles.header}>
        <TouchableOpacity onPress={onBack}>
          <Text style={styles.backBtn}>← 뒤로</Text>
        </TouchableOpacity>
        <Text style={styles.statusText}>
          {status === 'connected' ? '● 연결됨' : status === 'connecting' ? '◌ 연결 중...' : '○ 끊김'}
        </Text>
        <View style={{width: 50}} />
      </View>

      {/* Terminal (xterm.js WebView) */}
      <View style={styles.termContainer}>
        <WebView
          ref={webViewRef}
          source={Platform.OS === 'android'
            ? {uri: 'file:///android_asset/terminal.html'}
            : {uri: 'terminal.html'}}
          style={styles.webview}
          javaScriptEnabled
          domStorageEnabled
          onMessage={onWebViewMessage}
          originWhitelist={['*']}
        />
      </View>

      {/* Chat input */}
      <View style={styles.inputRow}>
        <TextInput
          style={styles.input}
          placeholder="채팅 입력..."
          placeholderTextColor="#666"
          value={stdin}
          onChangeText={setStdin}
          onSubmitEditing={sendStdin}
          returnKeyType="send"
          autoCorrect={false}
        />
        <TouchableOpacity onPress={sendStdin} style={styles.sendBtn}>
          <Text style={styles.sendBtnText}>전송</Text>
        </TouchableOpacity>
      </View>
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  container: {flex: 1, backgroundColor: '#0d1117'},
  header: {
    flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center',
    paddingHorizontal: 12, paddingVertical: 8, backgroundColor: '#161b22',
  },
  backBtn: {color: '#58a6ff', fontSize: 14},
  statusText: {color: '#8b949e', fontSize: 12},
  termContainer: {flex: 1},
  webview: {flex: 1, backgroundColor: '#0d1117'},
  inputRow: {
    flexDirection: 'row', padding: 8, backgroundColor: '#161b22', alignItems: 'center',
  },
  input: {
    flex: 1, backgroundColor: '#21262d', color: '#c9d1d9',
    borderRadius: 6, paddingHorizontal: 10, paddingVertical: 8, fontSize: 14,
  },
  sendBtn: {
    marginLeft: 8, backgroundColor: '#238636', borderRadius: 6,
    paddingHorizontal: 14, paddingVertical: 8,
  },
  sendBtnText: {color: '#fff', fontWeight: '600', fontSize: 13},
});
