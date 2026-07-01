import React, {useState} from 'react';
import {
  View,
  Text,
  TextInput,
  TouchableOpacity,
  StyleSheet,
  KeyboardAvoidingView,
  Platform,
} from 'react-native';
import {SafeAreaView} from 'react-native-safe-area-context';

export type ConnectionConfig =
  | {type: 'ws'; url: string}
  | {type: 'webrtc'; signalingUrl: string; code: string};

interface Props {
  onConnect: (config: ConnectionConfig) => void;
}

const DEFAULT_SIGNALING = 'ws://api.fullcount.kr/signal/';

export default function HomeScreen({onConnect}: Props) {
  const [host, setHost] = useState('192.168.0.');
  const [port, setPort] = useState('9171');
  const [code, setCode] = useState('');

  return (
    <SafeAreaView style={styles.container}>
      <KeyboardAvoidingView
        style={styles.inner}
        behavior={Platform.OS === 'ios' ? 'padding' : undefined}>
        <View style={styles.header}>
          <Text style={styles.logo}>DevRemote</Text>
          <Text style={styles.subtitle}>Claude Code 대시보드</Text>
        </View>

        {/* LAN 접속 */}
        <View style={styles.card}>
          <Text style={styles.label}>LAN 접속</Text>
          <View style={styles.row}>
            <Text style={styles.prefix}>ws://</Text>
            <TextInput
              style={styles.hostInput}
              value={host}
              onChangeText={setHost}
              placeholder="192.168.0.10"
              autoCapitalize="none"
              autoCorrect={false}
              keyboardType="url"
            />
            <Text style={styles.prefix}>:</Text>
            <TextInput
              style={styles.portInput}
              value={port}
              onChangeText={setPort}
              placeholder="9171"
              keyboardType="numeric"
              maxLength={5}
            />
            <Text style={styles.suffix}>/ws</Text>
          </View>
        </View>

        <TouchableOpacity
          style={styles.button}
          onPress={() => onConnect({type: 'ws', url: `ws://${host}:${port}/ws`})}>
          <Text style={styles.buttonText}>연결</Text>
        </TouchableOpacity>

        <View style={styles.divider}>
          <View style={styles.dividerLine} />
          <Text style={styles.dividerText}>또는</Text>
          <View style={styles.dividerLine} />
        </View>

        {/* 원격 접속 */}
        <View style={styles.card}>
          <Text style={styles.label}>원격 접속</Text>
          <TextInput
            style={styles.codeInput}
            value={code}
            onChangeText={setCode}
            placeholder="6자리 접속 코드"
            placeholderTextColor="#484f58"
            keyboardType="numeric"
            maxLength={6}
            autoCorrect={false}
          />
          <Text style={styles.hint}>
            데몬 실행 시 표시되는 6자리 코드를 입력하세요.{'\n'}
            최초 1회만 입력하면 이후 자동 연결됩니다.
          </Text>
        </View>

        <TouchableOpacity
          style={[styles.button, styles.remoteButton]}
          onPress={() => {
            if (code.length === 6) {
              onConnect({type: 'webrtc', signalingUrl: DEFAULT_SIGNALING, code});
            }
          }}>
          <Text style={styles.buttonText}>원격 연결</Text>
        </TouchableOpacity>
      </KeyboardAvoidingView>
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  container: {flex: 1, backgroundColor: '#0d1117'},
  inner: {flex: 1, justifyContent: 'center', paddingHorizontal: 24},
  header: {alignItems: 'center', marginBottom: 40},
  logo: {fontSize: 36, fontWeight: '800', color: '#58a6ff', letterSpacing: 1},
  subtitle: {fontSize: 15, color: '#8b949e', marginTop: 6},
  card: {
    backgroundColor: '#161b22', borderRadius: 12, padding: 20,
    borderWidth: 1, borderColor: '#30363d',
  },
  label: {
    fontSize: 13, fontWeight: '600', color: '#8b949e',
    textTransform: 'uppercase', letterSpacing: 1, marginBottom: 12,
  },
  row: {flexDirection: 'row', alignItems: 'center'},
  prefix: {
    fontSize: 16, color: '#484f58',
    fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace',
  },
  suffix: {
    fontSize: 16, color: '#484f58',
    fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace',
  },
  hostInput: {
    flex: 1, backgroundColor: '#0d1117', color: '#c9d1d9', fontSize: 16,
    fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace',
    paddingHorizontal: 8, paddingVertical: 10, borderRadius: 6,
    borderWidth: 1, borderColor: '#30363d',
  },
  portInput: {
    width: 60, backgroundColor: '#0d1117', color: '#c9d1d9', fontSize: 16,
    fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace',
    paddingHorizontal: 8, paddingVertical: 10, borderRadius: 6,
    borderWidth: 1, borderColor: '#30363d', textAlign: 'center',
  },
  codeInput: {
    backgroundColor: '#0d1117', color: '#c9d1d9', fontSize: 28,
    fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace',
    textAlign: 'center', paddingVertical: 14, borderRadius: 8,
    borderWidth: 1, borderColor: '#30363d', letterSpacing: 8,
  },
  hint: {
    fontSize: 12, color: '#484f58', marginTop: 12, textAlign: 'center', lineHeight: 18,
  },
  button: {
    backgroundColor: '#238636', borderRadius: 10, paddingVertical: 16,
    alignItems: 'center', marginTop: 16,
  },
  remoteButton: {backgroundColor: '#1f6feb'},
  buttonText: {color: '#ffffff', fontSize: 17, fontWeight: '700'},
  divider: {
    flexDirection: 'row', alignItems: 'center', marginVertical: 20, paddingHorizontal: 8,
  },
  dividerLine: {flex: 1, height: 1, backgroundColor: '#21262d'},
  dividerText: {color: '#484f58', fontSize: 13, marginHorizontal: 12},
});
