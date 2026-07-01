import React, {useEffect, useRef, useState, useCallback} from 'react';
import {
  View,
  Text,
  TextInput,
  FlatList,
  StyleSheet,
  TouchableOpacity,
  Platform,
  AppState,
  KeyboardAvoidingView,
} from 'react-native';
import {SafeAreaView} from 'react-native-safe-area-context';
import type {Transport, TransportStatus, Alert} from '../services/types';

interface Props {
  transport: Transport;
  pushToken: string | null;
  onDisconnect: () => void;
}

export default function SessionScreen({transport, pushToken, onDisconnect}: Props) {
  const [alerts, setAlerts] = useState<(Alert & {id: string; dismissed?: boolean})[]>([]);
  const [status, setStatus] = useState<TransportStatus>(transport.status);
  const [answers, setAnswers] = useState<Record<string, string>>({});
  const [errorMsg, setErrorMsg] = useState<string | null>(null);
  const transportRef = useRef(transport);

  transportRef.current = transport;

  const connect = useCallback(() => {
    transport.onStatusChange(setStatus);
    transport.onAlert((a: Alert) => {
      setAlerts(prev => [
        {id: `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`, ...a},
        ...prev,
      ]);
    });

    transport.connect().then(() => {
      if (pushToken) {
        transport.sendMessage({type: 'register', pushToken});
      }
    }).catch((err: any) => {
      setErrorMsg(err?.message || String(err));
    });
  }, [transport, pushToken]);

  useEffect(() => {
    connect();

    const sub = AppState.addEventListener('change', state => {
      if (state === 'active' && transportRef.current.status === 'disconnected') {
        connect();
      }
    });

    return () => {
      sub.remove();
      transportRef.current.disconnect();
    };
  }, [connect]);

  const dismissAlert = useCallback((id: string) => {
    setAlerts(prev => prev.map(a => (a.id === id ? {...a, dismissed: true} : a)));
  }, []);

  const respond = useCallback(
    (alert: Alert & {id: string}, approved: boolean) => {
      const payload: Record<string, unknown> = {
        type: 'response',
        approved,
        toolName: alert.toolName,
        toolUseId: alert.toolUseId,
        sessionId: alert.sessionId,
      };
      if (alert.toolName === 'AskUserQuestion' && approved) {
        payload.answer = answers[alert.id] || '';
      }
      transportRef.current.sendMessage(payload);
      dismissAlert(alert.id);
    },
    [dismissAlert, answers],
  );

  const setAnswer = useCallback((alertId: string, text: string) => {
    setAnswers(prev => ({...prev, [alertId]: text}));
  }, []);

  const formatTime = (ts: string) => {
    try {
      const d = new Date(ts);
      return d.toLocaleTimeString('ko-KR', {hour: '2-digit', minute: '2-digit', second: '2-digit'});
    } catch {
      return ts;
    }
  };

  const statusColor =
    status === 'connected' ? '#3fb950' : status === 'connecting' ? '#d29922' : '#f85149';
  const statusText =
    status === 'connected' ? '연결됨' : status === 'connecting' ? '연결 중...' : '끊김';

  const renderAlert = ({item}: {item: Alert & {id: string; dismissed?: boolean}}) => {
    const isQuestion = item.toolName === 'AskUserQuestion';

    return (
      <View style={[styles.alertCard, item.dismissed && styles.alertDismissed]}>
        <View style={styles.alertHeader}>
          <View style={styles.badgeRow}>
            <View style={[styles.badge, isQuestion ? styles.badgeQuestion : styles.badgeTool]}>
              <Text style={styles.badgeText}>
                {isQuestion ? '질문' : '도구'}
              </Text>
            </View>
            <Text style={styles.toolName}>{item.toolName}</Text>
          </View>
          <Text style={styles.timestamp}>{formatTime(item.timestamp)}</Text>
        </View>

        {isQuestion && item.question ? (
          <Text style={styles.question}>{item.question}</Text>
        ) : null}

        {item.description ? (
          <Text style={styles.description} numberOfLines={3}>
            {item.description}
          </Text>
        ) : null}

        {!item.dismissed && (
          <>
            {isQuestion ? (
              <View style={styles.answerRow}>
                <TextInput
                  style={styles.answerInput}
                  value={answers[item.id] || ''}
                  onChangeText={t => setAnswer(item.id, t)}
                  placeholder="답변 입력..."
                  placeholderTextColor="#484f58"
                  autoFocus
                />
                <View style={styles.answerBtnRow}>
                  <TouchableOpacity
                    style={[styles.actionBtn, styles.approveBtn]}
                    onPress={() => respond(item, true)}>
                    <Text style={styles.actionBtnText}>답변</Text>
                  </TouchableOpacity>
                  <TouchableOpacity
                    style={[styles.actionBtn, styles.denyBtn]}
                    onPress={() => respond(item, false)}>
                    <Text style={styles.actionBtnText}>건너뛰기</Text>
                  </TouchableOpacity>
                </View>
              </View>
            ) : (
              <View style={styles.actions}>
                <TouchableOpacity
                  style={[styles.actionBtn, styles.approveBtn, {flex: 1}]}
                  onPress={() => respond(item, true)}>
                  <Text style={styles.actionBtnText}>승인</Text>
                </TouchableOpacity>
                <TouchableOpacity
                  style={[styles.actionBtn, styles.denyBtn, {flex: 1}]}
                  onPress={() => respond(item, false)}>
                  <Text style={styles.actionBtnText}>거절</Text>
                </TouchableOpacity>
              </View>
            )}
          </>
        )}
      </View>
    );
  };

  return (
    <SafeAreaView style={styles.container}>
      <KeyboardAvoidingView
        style={styles.flex}
        behavior={Platform.OS === 'ios' ? 'padding' : undefined}>
        <View style={styles.topBar}>
          <TouchableOpacity onPress={onDisconnect} style={styles.backBtn}>
            <Text style={styles.backBtnText}>← 연결 해제</Text>
          </TouchableOpacity>
          <View style={styles.statusRow}>
            <View style={[styles.statusDot, {backgroundColor: statusColor}]} />
            <Text style={styles.statusText}>{statusText}</Text>
          </View>
          {errorMsg ? (
            <Text style={styles.errorText} numberOfLines={3}>{errorMsg}</Text>
          ) : null}
        </View>

        {alerts.length === 0 ? (
          <View style={styles.empty}>
            <Text style={styles.emptyIcon}>📡</Text>
            <Text style={styles.emptyTitle}>대기 중</Text>
            <Text style={styles.emptySub}>
              Claude가 tool을 실행하거나{'\n'}질문을 보내면 여기에 표시됩니다
            </Text>
          </View>
        ) : (
          <FlatList
            data={alerts}
            keyExtractor={item => item.id}
            renderItem={renderAlert}
            contentContainerStyle={styles.list}
            keyboardShouldPersistTaps="handled"
          />
        )}
      </KeyboardAvoidingView>
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  container: {flex: 1, backgroundColor: '#0d1117'},
  flex: {flex: 1},
  topBar: {
    flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center',
    paddingHorizontal: 20, paddingVertical: 12,
    borderBottomWidth: 1, borderBottomColor: '#21262d',
  },
  backBtn: {paddingVertical: 4, paddingRight: 12},
  backBtnText: {color: '#58a6ff', fontSize: 15},
  statusRow: {flexDirection: 'row', alignItems: 'center'},
  statusDot: {width: 8, height: 8, borderRadius: 4, marginRight: 6},
  statusText: {color: '#8b949e', fontSize: 13},
  errorText: {color: '#f85149', fontSize: 12, marginTop: 6, maxWidth: 200},
  list: {padding: 16, paddingBottom: 32},
  alertCard: {
    backgroundColor: '#161b22', borderRadius: 10, padding: 16,
    marginBottom: 12, borderWidth: 1, borderColor: '#30363d',
  },
  alertDismissed: {opacity: 0.4},
  alertHeader: {
    flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8,
  },
  badgeRow: {flexDirection: 'row', alignItems: 'center'},
  badge: {paddingHorizontal: 8, paddingVertical: 2, borderRadius: 4, marginRight: 8},
  badgeQuestion: {backgroundColor: '#1f6feb33', borderWidth: 1, borderColor: '#1f6feb66'},
  badgeTool: {backgroundColor: '#d2992233', borderWidth: 1, borderColor: '#d2992266'},
  badgeText: {color: '#c9d1d9', fontSize: 11, fontWeight: '700', textTransform: 'uppercase'},
  toolName: {
    color: '#c9d1d9', fontSize: 15, fontWeight: '600',
    fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace',
  },
  timestamp: {
    color: '#484f58', fontSize: 12,
    fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace',
  },
  question: {color: '#f0f6fc', fontSize: 15, lineHeight: 22, marginBottom: 8},
  description: {
    color: '#8b949e', fontSize: 13, lineHeight: 18,
    fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace',
    marginBottom: 4,
  },
  answerRow: {marginTop: 10},
  answerInput: {
    backgroundColor: '#0d1117', color: '#c9d1d9', fontSize: 15,
    paddingHorizontal: 12, paddingVertical: 10, borderRadius: 8,
    borderWidth: 1, borderColor: '#30363d', marginBottom: 10,
  },
  answerBtnRow: {flexDirection: 'row', gap: 10},
  actions: {flexDirection: 'row', marginTop: 14, gap: 10},
  actionBtn: {paddingVertical: 12, borderRadius: 8, alignItems: 'center'},
  approveBtn: {backgroundColor: '#238636'},
  denyBtn: {backgroundColor: '#21262d', borderWidth: 1, borderColor: '#f8514966'},
  actionBtnText: {color: '#ffffff', fontSize: 15, fontWeight: '700'},
  empty: {flex: 1, justifyContent: 'center', alignItems: 'center', paddingHorizontal: 40},
  emptyIcon: {fontSize: 48, marginBottom: 16},
  emptyTitle: {fontSize: 20, fontWeight: '700', color: '#c9d1d9', marginBottom: 8},
  emptySub: {fontSize: 14, color: '#484f58', textAlign: 'center', lineHeight: 20},
});
