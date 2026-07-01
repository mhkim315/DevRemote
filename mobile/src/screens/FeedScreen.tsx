import React, {useEffect, useRef, useState, useCallback} from 'react';
import {
  View, Text, TextInput, FlatList, StyleSheet, TouchableOpacity, Platform,
} from 'react-native';
import {SafeAreaView} from 'react-native-safe-area-context';
import type {Transport, TransportStatus, Alert} from '../services/types';

interface Props {
  transport: Transport;
  pushToken: string | null;
  onBack: () => void;
}

interface FeedItem {
  id: string;
  type: string;
  time: string;
  text: string;
}

export default function FeedScreen({transport, pushToken, onBack}: Props) {
  const [items, setItems] = useState<FeedItem[]>([]);
  const [status, setStatus] = useState<TransportStatus>(transport.status);
  const [stdin, setStdin] = useState('');
  const transportRef = useRef(transport);
  transportRef.current = transport;

  useEffect(() => {
    transport.onStatusChange(setStatus);
    transport.onAlert((a: Alert) => {
      const isRaw = a.type === 'raw';
      const label = isRaw
        ? `[${a.type}] ${a.description?.substring(0, 80) || '···'}`
        : `[${a.toolName || a.type || 'event'}] ${a.description || a.question || ''}`;
      setItems(prev => [{
        id: `${Date.now()}-${Math.random().toString(36).slice(2, 6)}`,
        type: a.type || a.toolName || 'event',
        time: new Date(a.timestamp).toLocaleTimeString('ko-KR', {hour: '2-digit', minute: '2-digit', second: '2-digit'}),
        text: label.substring(0, 200),
      }, ...prev.slice(0, 99)]);
    });
    // Don't reconnect — transport is already active from SessionScreen.
    if (pushToken) transport.sendMessage({type: 'register', pushToken});
  }, [transport, pushToken]);

  const sendStdin = useCallback(() => {
    if (stdin.trim()) {
      const text = stdin.trim();
      transportRef.current.sendMessage({type: 'stdin', text: text + '\n'});
      // Local echo
      setItems(prev => [{
        id: `${Date.now()}-me-${Math.random().toString(36).slice(2, 4)}`,
        type: 'me',
        time: new Date().toLocaleTimeString('ko-KR', {hour: '2-digit', minute: '2-digit', second: '2-digit'}),
        text,
      }, ...prev.slice(0, 99)]);
      setStdin('');
    }
  }, [stdin]);

  const sendInterrupt = useCallback(() => {
    transport.sendMessage({type: 'interrupt'});
  }, [transport]);

  const statusColor = status === 'connected' ? '#3fb950' : status === 'connecting' ? '#d29922' : '#f85149';
  const statusText = status === 'connected' ? '연결됨' : status === 'connecting' ? '연결 중...' : '끊김';

  const badgeColor = (t: string) => {
    switch (t) {
      case 'assistant': return '#58a6ff';
      case 'user': return '#3fb950';
      case 'raw': return '#484f58';
      case 'AskUserQuestion': return '#d29922';
      case 'Bash': return '#f0883e';
      case 'me': return '#238636';
      default: return '#484f58';
    }
  };

  return (
    <SafeAreaView style={styles.container}>
      <View style={styles.topBar}>
        <TouchableOpacity onPress={onBack} style={styles.backBtn}>
          <Text style={styles.backBtnText}>← 뒤로</Text>
        </TouchableOpacity>
        <View style={styles.statusRow}>
          <View style={[styles.statusDot, {backgroundColor: statusColor}]} />
          <Text style={styles.statusText}>{statusText}</Text>
        </View>
      </View>

      <FlatList
        data={items}
        keyExtractor={item => item.id}
        contentContainerStyle={styles.list}
        inverted={false}
        renderItem={({item}) => (
          <View style={styles.itemRow}>
            <Text style={styles.itemTime}>{item.time}</Text>
            <View style={[styles.itemBadge, {backgroundColor: badgeColor(item.type)}]}>
              <Text style={styles.itemBadgeText}>{item.type.substring(0, 4)}</Text>
            </View>
            <Text style={styles.itemText} numberOfLines={2}>{item.text}</Text>
          </View>
        )}
      />

      <View style={styles.inputBar}>
        <TouchableOpacity style={styles.ctrlCButton} onPress={sendInterrupt}>
          <Text style={styles.ctrlCText}>Ctrl+C</Text>
        </TouchableOpacity>
        <TextInput
          style={styles.input}
          value={stdin}
          onChangeText={setStdin}
          placeholder="텍스트 입력..."
          placeholderTextColor="#484f58"
          onSubmitEditing={sendStdin}
          returnKeyType="send"
        />
        <TouchableOpacity style={styles.sendButton} onPress={sendStdin}>
          <Text style={styles.sendText}>전송</Text>
        </TouchableOpacity>
      </View>
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  container: {flex: 1, backgroundColor: '#0d1117'},
  topBar: {
    flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center',
    paddingHorizontal: 20, paddingVertical: 12, borderBottomWidth: 1, borderBottomColor: '#21262d',
  },
  backBtn: {paddingVertical: 4, paddingRight: 12},
  backBtnText: {color: '#58a6ff', fontSize: 15},
  statusRow: {flexDirection: 'row', alignItems: 'center'},
  statusDot: {width: 8, height: 8, borderRadius: 4, marginRight: 6},
  statusText: {color: '#8b949e', fontSize: 13},
  list: {padding: 12, paddingBottom: 8},
  itemRow: {
    flexDirection: 'row', alignItems: 'center', paddingVertical: 6,
    borderBottomWidth: 1, borderBottomColor: '#161b22',
  },
  itemTime: {
    color: '#484f58', fontSize: 11, width: 52,
    fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace',
  },
  itemBadge: {
    paddingHorizontal: 5, paddingVertical: 1, borderRadius: 3, marginRight: 8,
    minWidth: 32, alignItems: 'center',
  },
  itemBadgeText: {color: '#fff', fontSize: 9, fontWeight: '700'},
  itemText: {
    color: '#c9d1d9', fontSize: 12, flex: 1,
    fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace',
  },
  inputBar: {
    flexDirection: 'row', alignItems: 'center', paddingHorizontal: 12,
    paddingVertical: 8, borderTopWidth: 1, borderTopColor: '#21262d',
  },
  ctrlCButton: {
    backgroundColor: '#f8514966', borderRadius: 6, paddingVertical: 10, paddingHorizontal: 10, marginRight: 8,
  },
  ctrlCText: {color: '#f85149', fontSize: 13, fontWeight: '700'},
  input: {
    flex: 1, backgroundColor: '#161b22', color: '#c9d1d9', fontSize: 14,
    paddingHorizontal: 12, paddingVertical: 10, borderRadius: 8,
    borderWidth: 1, borderColor: '#30363d',
  },
  sendButton: {
    backgroundColor: '#238636', borderRadius: 6, paddingVertical: 10, paddingHorizontal: 14, marginLeft: 8,
  },
  sendText: {color: '#fff', fontSize: 14, fontWeight: '600'},
});
