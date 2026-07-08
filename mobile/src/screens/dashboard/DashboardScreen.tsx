import React, { useState, useEffect } from 'react';
import { View, Text, StyleSheet, SafeAreaView, ActivityIndicator, TouchableOpacity, Alert, Platform, ScrollView } from 'react-native';
import { AgentCard, SessionTelemetry } from '../../components/AgentCard';
import { AgentProfileModal } from '../../components/AgentProfileModal';
import { ApprovalCard } from '../../components/ApprovalCard';
import { listSessions, createOrUpdateSession, deleteSession } from '../../lib/client';
import { useConnection } from '../../lib/connection';
import { isDegraded } from '../../lib/agentDisplay';

interface Props {
  onSelectAgent: (sessionName: string) => void;
  onSnippets: () => void;
  token?: string;
}

export default function DashboardScreen({ onSelectAgent, onSnippets, token }: Props) {
  const [sessions, setSessions] = useState<SessionTelemetry[]>([]);
  const [loading, setLoading] = useState(true);
  const [fetchError, setFetchError] = useState('');
  const { disconnect, connectionError } = useConnection();

  const [modalVisible, setModalVisible] = useState(false);
  const [editSession, setEditSession] = useState<SessionTelemetry | null>(null);

  const fetchSessions = () => {
    listSessions(token)
      .then(data => {
        const normalized = (data || []).map((s: any) =>
          typeof s === 'string' ? { id: s, state: 'idle', load: 0 } : s
        ).sort((a: SessionTelemetry, b: SessionTelemetry) => String(a.id).localeCompare(String(b.id)));
        setSessions(normalized);
        setFetchError('');
        setLoading(false);
      })
      .catch(err => {
        console.error(err);
        setFetchError('Cannot reach daemon. Check connection.');
        setLoading(false);
      });
  };

  useEffect(() => {
    fetchSessions();
    const interval = setInterval(fetchSessions, 1500);
    return () => clearInterval(interval);
  }, []);

  const handleSaveProfile = async (id: string, runner: string, color: string) => {
    const isEdit = !!editSession;
    setSessions(prev => {
      if (isEdit) {
        return prev.map(s => s.id === id ? { ...s, runner, runnerColor: color } : s);
      } else {
        return [...prev, { id, state: 'idle' as const, load: 0, runner, runnerColor: color }];
      }
    });
    setModalVisible(false);
    setEditSession(null);
    try {
      await createOrUpdateSession(id, runner, color, token);
      fetchSessions();
    } catch (e) {
      Alert.alert('Error', 'Failed to save agent profile');
      fetchSessions();
    }
  };

  const handleDeleteProfile = async (id: string) => {
    try {
      await deleteSession(id, token);
      fetchSessions();
      setModalVisible(false);
      setEditSession(null);
    } catch (e) {
      Alert.alert('Error', 'Failed to terminate agent');
    }
  };

  // P1a: section grouping.
  const pendingApprovals = sessions.flatMap(s =>
    (s.approvals || [])
      .filter(a => a.status === 'pending')
      .map(a => ({ sessionId: s.id, approval: a }))
  );

  const sessionIdsWithPending = new Set(pendingApprovals.map(p => p.sessionId));

  const isObserveOnly = (s: SessionTelemetry): boolean => {
    // observe-only: no live_stream capability (can't open terminal).
    return !!(s.capabilities && !s.capabilities.includes('live_stream'));
  };

  const needsAttention = sessions.filter(s =>
    sessionIdsWithPending.has(s.id) || s.state === 'waiting'
  );

  const running = sessions.filter(s =>
    !sessionIdsWithPending.has(s.id) &&
    (s.state === 'working' || s.state === 'thinking')
  );

  const recentlyCompleted = sessions.filter(s =>
    !sessionIdsWithPending.has(s.id) &&
    s.state !== 'working' && s.state !== 'thinking' && s.state !== 'waiting' &&
    !isDegraded(s.agentConfidence, s.agentKind) &&
    !isObserveOnly(s)
  );

  const degradedOrViewOnly = sessions.filter(s =>
    !sessionIdsWithPending.has(s.id) &&
    s.state !== 'waiting' &&
    (isDegraded(s.agentConfidence, s.agentKind) || isObserveOnly(s))
  );

  const cardForSession = (s: SessionTelemetry) => (
    <AgentCard
      key={s.id}
      session={s}
      onPress={() => onSelectAgent(s.id)}
      onSettings={() => { setEditSession(s); setModalVisible(true); }}
    />
  );

  const sectionHeader = (title: string, emoji: string, color: string) => (
    <View style={[styles.sectionHeader, { borderLeftColor: color }]}>
      <Text style={styles.sectionEmoji}>{emoji}</Text>
      <Text style={[styles.sectionTitle, { color }]}>{title}</Text>
    </View>
  );

  return (
    <SafeAreaView style={styles.container}>
      <View style={styles.header}>
        <Text style={styles.headerTitle}>POKIT AGENTS</Text>
        <View style={{flexDirection:'row', gap:6}}>
          <TouchableOpacity onPress={async () => { await disconnect(); }} style={[styles.snippetBtn, {borderColor: '#f85149'}]}>
            <Text style={[styles.snippetBtnText, {color: '#f85149'}]}>↻ RESCAN</Text>
          </TouchableOpacity>
          <TouchableOpacity onPress={onSnippets} style={styles.snippetBtn}>
            <Text style={styles.snippetBtnText}>SNIPPETS</Text>
          </TouchableOpacity>
        </View>
      </View>

      {(fetchError || connectionError) ? (
        <View style={{padding: 20, alignItems: 'center'}}>
          <Text style={{color: '#f85149', fontSize: 14, textAlign: 'center', marginBottom: 12}}>
            {fetchError || connectionError}
          </Text>
          <TouchableOpacity onPress={() => { setFetchError(''); fetchSessions(); }} style={{backgroundColor: '#1E91B3', paddingHorizontal: 20, paddingVertical: 10, borderRadius: 8}}>
            <Text style={{color: '#fff', fontWeight: '700'}}>RETRY</Text>
          </TouchableOpacity>
        </View>
      ) : loading ? (
        <ActivityIndicator size="large" color="#45EBE9" style={{ marginTop: 40 }} />
      ) : (
        <ScrollView style={styles.content} contentContainerStyle={styles.scrollContent}>
          {/* P1a: Needs Attention — always first */}
          {needsAttention.length > 0 && (
            <View style={styles.section}>
              {sectionHeader('NEEDS ATTENTION', '⚠', '#f85149')}
              {pendingApprovals.map(({ sessionId, approval }) => (
                <ApprovalCard
                  key={`approval-${approval.id}`}
                  sessionId={sessionId}
                  approval={approval}
                  token={token}
                  onResolved={fetchSessions}
                />
              ))}
              {needsAttention.filter(s => !sessionIdsWithPending.has(s.id)).map(cardForSession)}
            </View>
          )}

          {/* Running */}
          {running.length > 0 && (
            <View style={styles.section}>
              {sectionHeader('RUNNING', '🟢', '#39d353')}
              <View style={styles.cardGrid}>
                {running.map(cardForSession)}
              </View>
            </View>
          )}

          {/* Recently Completed */}
          {recentlyCompleted.length > 0 && (
            <View style={styles.section}>
              {sectionHeader('RECENTLY COMPLETED', '✅', '#8b949e')}
              <View style={styles.cardGrid}>
                {recentlyCompleted.map(cardForSession)}
              </View>
            </View>
          )}

          {/* Degraded / View Only */}
          {degradedOrViewOnly.length > 0 && (
            <View style={styles.section}>
              {sectionHeader('DEGRADED / VIEW ONLY', '⚡', '#666')}
              <View style={styles.cardGrid}>
                {degradedOrViewOnly.map(cardForSession)}
              </View>
            </View>
          )}

          {/* Add button — always at bottom */}
          <View style={styles.section}>
            <TouchableOpacity
              style={styles.addCard}
              onPress={() => { setEditSession(null); setModalVisible(true); }}
              activeOpacity={0.7}
            >
              <Text style={styles.addCardPlus}>+</Text>
              <Text style={styles.addCardText}>NEW AGENT</Text>
            </TouchableOpacity>
          </View>
        </ScrollView>
      )}

      <AgentProfileModal
        visible={modalVisible}
        onClose={() => { setModalVisible(false); setEditSession(null); }}
        onSave={handleSaveProfile}
        onDelete={editSession ? handleDeleteProfile : undefined}
        initialId={editSession?.id}
        initialRunner={editSession?.runner}
        initialColor={editSession?.runnerColor}
      />
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#000000' },
  header: {
    flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center',
    paddingHorizontal: 20, paddingVertical: 16, borderBottomWidth: 1, borderBottomColor: '#0D2D45'
  },
  headerTitle: { fontSize: 24, fontWeight: '800', color: '#ffffff', letterSpacing: 1.2, fontFamily: Platform.OS === 'ios' ? 'HelveticaNeue-CondensedBold' : 'sans-serif-condensed' },
  snippetBtn: { backgroundColor: 'transparent', paddingHorizontal: 16, paddingVertical: 8, borderRadius: 32, borderWidth: 1, borderColor: '#45EBE9' },
  snippetBtnText: { color: '#ffffff', fontSize: 12, fontWeight: '700', letterSpacing: 0.96 },
  content: { flex: 1 },
  scrollContent: { paddingBottom: 24 },
  section: { marginBottom: 4 },
  sectionHeader: {
    flexDirection: 'row', alignItems: 'center',
    paddingHorizontal: 20, paddingVertical: 10,
    marginTop: 8, marginBottom: 4,
    borderLeftWidth: 3,
  },
  sectionEmoji: { fontSize: 14, marginRight: 8 },
  sectionTitle: { fontSize: 13, fontWeight: '800', letterSpacing: 1.2 },
  cardGrid: {
    flexDirection: 'row', flexWrap: 'wrap',
    paddingHorizontal: 12,
  },
  addCard: {
    marginHorizontal: 12, marginTop: 12,
    padding: 16, borderRadius: 16,
    borderWidth: 1.5, borderColor: '#1E91B3', borderStyle: 'dashed',
    alignItems: 'center', justifyContent: 'center',
    minHeight: 80,
    backgroundColor: 'transparent'
  },
  addCardPlus: { fontSize: 36, color: '#1E91B3', fontWeight: '300' },
  addCardText: { fontSize: 12, color: '#1E91B3', fontWeight: '700', marginTop: 8, letterSpacing: 0.96 }
});
