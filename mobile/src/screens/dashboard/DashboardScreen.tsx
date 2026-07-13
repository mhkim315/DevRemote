import React, { useState, useEffect, useRef } from 'react';
import { View, Text, StyleSheet, SafeAreaView, ActivityIndicator, TouchableOpacity, Alert, Platform, ScrollView } from 'react-native';
import { AgentCard, SessionTelemetry } from '../../components/AgentCard';
import { AgentProfileModal } from '../../components/AgentProfileModal';
import { NewSessionModal } from '../../components/NewSessionModal';
import { ApprovalCard } from '../../components/ApprovalCard';
import { listSessions, createOrUpdateSession, ConnectivityFailure, PokitError } from '../../lib/client';
import { createPollController } from '../../lib/sessionPoller';
import { isConnectionStale } from '../../lib/agentActivity';
import { useConnection } from '../../lib/connection';
import { isDegraded } from '../../lib/agentDisplay';

interface Props {
  onSelectAgent: (sessionName: string) => void;
  onSnippets: () => void;
  token?: string;
  authCtx?: any;  // M3-auth-4A
}

export default function DashboardScreen({ onSelectAgent, onSnippets, token }: Props) {
  const [sessions, setSessions] = useState<SessionTelemetry[]>([]);
  const [loading, setLoading] = useState(true);
  const [fetchError, setFetchError] = useState('');
  // S1-D: singleflight + mounted poll controller (no starvation, no post-unmount commit).
  const pollRef = useRef(createPollController<any[]>());
  // S1-E: last successful poll time; feeds connectivity/freshness staleness.
  const lastSuccessRef = useRef<number | null>(null);
  const { disconnect, connectionError, daemonReachable, sessionsLoaded, sessionsEmpty, failure, refreshDiagnostics } = useConnection();

  const [modalVisible, setModalVisible] = useState(false);
  const [newSessionVisible, setNewSessionVisible] = useState(false);
  const [editSession, setEditSession] = useState<SessionTelemetry | null>(null);

  const fetchSessions = () => {
    // S1-D: singleflight — skip if a poll is already in flight (no starvation on a
    // slow network); the controller also drops any response that resolves after
    // unmount.
    pollRef.current.poll(
      () => listSessions(token),
      data => {
        const normalized = (data || []).map((s: any) =>
          typeof s === 'string' ? { id: s, state: 'idle', load: 0 } : s
        ).sort((a: SessionTelemetry, b: SessionTelemetry) => String(a.id).localeCompare(String(b.id)));
        setSessions(normalized);
        setFetchError('');
        setLoading(false);
        lastSuccessRef.current = Date.now(); // S1-E: mark a fresh successful poll
      },
      err => {
        console.error(err);
        // R1a: classify the error for actionable messaging.
        if (err instanceof PokitError) {
          switch (err.failure) {
            case ConnectivityFailure.NetworkUnreachable:
              setFetchError('Daemon unreachable. Check that the daemon is running.');
              break;
            case ConnectivityFailure.Timeout:
              setFetchError('Connection timed out. Check your network or daemon URL.');
              break;
            case ConnectivityFailure.AuthError:
              setFetchError('Auth failed. Re-scan the QR code or re-enter the daemon URL.');
              break;
            case ConnectivityFailure.APIError:
              setFetchError('Sessions API error (' + err.statusCode + '). Daemon may need restart.');
              break;
            default:
              setFetchError('Cannot reach daemon. Check connection.');
          }
        } else {
          setFetchError('Cannot reach daemon. Check connection.');
        }
        // R1a: refresh connection diagnostics so daemonReachable/sessionsEmpty
        // stay consistent with the current failure, not a stale probe.
        refreshDiagnostics();
        setLoading(false);
      });
  };

  useEffect(() => {
    const poll = pollRef.current;
    fetchSessions();
    const interval = setInterval(fetchSessions, 1500);
    return () => { clearInterval(interval); poll.stop(); };
  }, []);

  const handleSaveProfile = async (id: string, runner: string, color: string) => {
    // M3a: the New path has moved to <NewSessionModal>; this handler is now
    // edit-only (color/runner presentation). Guard so a stray call without an
    // editSession does not insert a phantom card.
    if (!editSession) {
      Alert.alert('Error', 'No session to edit.');
      return;
    }
    setSessions(prev => prev.map(s => s.id === id ? { ...s, runner, runnerColor: color } : s));
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

  // M3b: destructive session lifecycle (Stop/Force Kill/Delete History) lives in
  // the session viewer (FeedScreen), gated by managedLifecycle capability and
  // authoritative state over the paired-device transport. The edit modal is
  // presentation-only (color/runner) and must NOT delete via the legacy query
  // DELETE, so no delete handler is wired here.

  // M3a: a session exists only after the server returns it. Open the returned
  // canonical controlled_pty session directly; refresh removes any staleness.
  const handleSessionCreated = (canonicalId: string) => {
    setNewSessionVisible(false);
    fetchSessions();
    onSelectAgent(canonicalId);
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
      connectionStale={isConnectionStale(fetchError, lastSuccessRef.current, Date.now(), 6000)}
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
          <Text style={{color: '#f85149', fontSize: 14, textAlign: 'center', marginBottom: 8}}>
            {fetchError || connectionError}
          </Text>
          {/* R1a: show connectivity diagnosis */}
          <Text style={{color: '#8b949e', fontSize: 11, textAlign: 'center', marginBottom: 8}}>
            {!daemonReachable
              ? 'Daemon not reachable — check URL and network.'
              : failure === ConnectivityFailure.AuthError
              ? 'Auth rejected — re-scan QR code from terminal.'
              : failure === ConnectivityFailure.APIError
              ? 'Daemon reachable but API returned error.'
              : 'Tap Retry to attempt reconnection.'}
          </Text>
          <Text style={{color: '#666', fontSize: 10, textAlign: 'center', marginBottom: 12}}>
            diagnosis: reachable={String(daemonReachable)} sessions_loaded={String(sessionsLoaded)} empty={String(sessionsEmpty)}
          </Text>
          <TouchableOpacity onPress={() => { setFetchError(''); fetchSessions(); }} style={{backgroundColor: '#1E91B3', paddingHorizontal: 20, paddingVertical: 10, borderRadius: 8}}>
            <Text style={{color: '#fff', fontWeight: '700'}}>RETRY</Text>
          </TouchableOpacity>
        </View>
      ) : loading ? (
        <ActivityIndicator size="large" color="#45EBE9" style={{ marginTop: 40 }} />
      ) : sessions.length === 0 ? (
        /* R1a: daemon reachable but zero sessions — not an error, actionable empty state */
        <View style={{flex: 1, alignItems: 'center', justifyContent: 'center', padding: 24}}>
          <Text style={{color: '#45EBE9', fontSize: 18, fontWeight: '800', letterSpacing: 1.2, marginBottom: 12}}>
            DAEMON CONNECTED
          </Text>
          <Text style={{color: '#8b949e', fontSize: 14, textAlign: 'center', marginBottom: 8}}>
            No sessions found. The daemon is reachable but has no active terminal sessions.
          </Text>
          <Text style={{color: '#666', fontSize: 12, textAlign: 'center', marginBottom: 20}}>
            Start a session in your terminal or tap + to create one.
          </Text>
          <View style={{flexDirection: 'row', gap: 12}}>
            <TouchableOpacity onPress={() => { setFetchError(''); fetchSessions(); }} style={{backgroundColor: '#1E91B3', paddingHorizontal: 20, paddingVertical: 10, borderRadius: 8}}>
              <Text style={{color: '#fff', fontWeight: '700'}}>REFRESH</Text>
            </TouchableOpacity>
            <TouchableOpacity onPress={() => setNewSessionVisible(true)} style={{backgroundColor: '#39d353', paddingHorizontal: 20, paddingVertical: 10, borderRadius: 8}}>
              <Text style={{color: '#000', fontWeight: '700'}}>+ NEW SESSION</Text>
            </TouchableOpacity>
          </View>
        </View>
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
              onPress={() => setNewSessionVisible(true)}
              activeOpacity={0.7}
            >
              <Text style={styles.addCardPlus}>+</Text>
              <Text style={styles.addCardText}>NEW SESSION</Text>
            </TouchableOpacity>
          </View>
        </ScrollView>
      )}

      <AgentProfileModal
        visible={modalVisible}
        onClose={() => { setModalVisible(false); setEditSession(null); }}
        onSave={handleSaveProfile}
        initialId={editSession?.id}
        initialRunner={editSession?.runner}
        initialColor={editSession?.runnerColor}
      />
      <NewSessionModal
        visible={newSessionVisible}
        onClose={() => setNewSessionVisible(false)}
        onCreated={handleSessionCreated}
        token={token}
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
