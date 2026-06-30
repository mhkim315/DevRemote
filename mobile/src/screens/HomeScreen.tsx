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

interface Props {
  onConnect: (url: string) => void;
}

export default function HomeScreen({onConnect}: Props) {
  const [host, setHost] = useState('192.168.0.');
  const [port, setPort] = useState('9171');

  const wsUrl = `ws://${host}:${port}/ws`;

  return (
    <SafeAreaView style={styles.container}>
      <KeyboardAvoidingView
        style={styles.inner}
        behavior={Platform.OS === 'ios' ? 'padding' : undefined}>
        <View style={styles.header}>
          <Text style={styles.logo}>DevRemote</Text>
          <Text style={styles.subtitle}>Claude Code 대시보드</Text>
        </View>

        <View style={styles.card}>
          <Text style={styles.label}>데몬 주소</Text>

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

          <Text style={styles.hint}>
            컴퓨터에서 {'"'}devremote watch{'"'} 실행 후 연결하세요
          </Text>
        </View>

        <TouchableOpacity
          style={styles.button}
          onPress={() => onConnect(wsUrl)}>
          <Text style={styles.buttonText}>연결</Text>
        </TouchableOpacity>
      </KeyboardAvoidingView>
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: '#0d1117',
  },
  inner: {
    flex: 1,
    justifyContent: 'center',
    paddingHorizontal: 24,
  },
  header: {
    alignItems: 'center',
    marginBottom: 48,
  },
  logo: {
    fontSize: 36,
    fontWeight: '800',
    color: '#58a6ff',
    letterSpacing: 1,
  },
  subtitle: {
    fontSize: 15,
    color: '#8b949e',
    marginTop: 6,
  },
  card: {
    backgroundColor: '#161b22',
    borderRadius: 12,
    padding: 20,
    borderWidth: 1,
    borderColor: '#30363d',
  },
  label: {
    fontSize: 13,
    fontWeight: '600',
    color: '#8b949e',
    textTransform: 'uppercase',
    letterSpacing: 1,
    marginBottom: 12,
  },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
  },
  prefix: {
    fontSize: 16,
    color: '#484f58',
    fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace',
  },
  suffix: {
    fontSize: 16,
    color: '#484f58',
    fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace',
  },
  hostInput: {
    flex: 1,
    backgroundColor: '#0d1117',
    color: '#c9d1d9',
    fontSize: 16,
    fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace',
    paddingHorizontal: 8,
    paddingVertical: 10,
    borderRadius: 6,
    borderWidth: 1,
    borderColor: '#30363d',
  },
  portInput: {
    width: 60,
    backgroundColor: '#0d1117',
    color: '#c9d1d9',
    fontSize: 16,
    fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace',
    paddingHorizontal: 8,
    paddingVertical: 10,
    borderRadius: 6,
    borderWidth: 1,
    borderColor: '#30363d',
    textAlign: 'center',
  },
  hint: {
    fontSize: 12,
    color: '#484f58',
    marginTop: 12,
    textAlign: 'center',
  },
  button: {
    backgroundColor: '#238636',
    borderRadius: 10,
    paddingVertical: 16,
    alignItems: 'center',
    marginTop: 24,
  },
  buttonText: {
    color: '#ffffff',
    fontSize: 17,
    fontWeight: '700',
  },
});
