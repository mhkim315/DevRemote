import React, { useState, useEffect } from 'react';
import { View, Text, StyleSheet, TouchableOpacity } from 'react-native';
import Svg, { Path } from 'react-native-svg';
import { RUNNERS } from '../lib/runners';
import { formatAgentKind, formatAgentStatus, isDegraded } from '../lib/agentDisplay';
import { AgentActivity, deriveCardActivity, sessionNeedsApproval } from '../lib/agentActivity';
import { SafeApproval } from '../lib/client';

export interface AgentEvent {
  id: string;
  session: string;
  type: string; // "file_edit", "approval_request", "error", "done"
  summary: string;
  detail: string;
  timestamp: string;
}

export interface SessionTelemetry {
  id: string;
  displayId?: string;
  // PA3 Step 1: `state` removed — use agentActivity.status + agentStatus instead.
  // M3b: daemon-authoritative managed lifecycle state (Session Catalog), separate
  // from agent activity. Empty/absent for non-managed sessions.
  lifecycleState?: string;
  // PA3 Step 1: `load` removed.
  // PA3 Step 1: `runner`/`runnerColor` removed — use agentKind instead.
  adapter?: string;
  capabilities?: string[];
  adapterCapabilities?: string[];
  isAddBtn?: boolean;
  // PA3 Step 1: `events` removed — use getTranscript() for event history.
  agentKind?: string;
  agentStatus?: string;
  agentConfidence?: number;
  approvals?: SafeApproval[];
  // S1-D: advisory agent-activity dimension (validated at render), SEPARATE from
  // `state`/`lifecycleState`. Never drives lifecycle or approval actions.
  agentActivity?: AgentActivity;
}

interface Props {
  session: SessionTelemetry;
  onPress: () => void;
  onSettings?: () => void;
  // S1-E: true when the daemon poll failed or last-success freshness expired; a
  // previously-fresh activity is then shown as non-current, never as live.
  connectionStale?: boolean;
}

// PA3 Step 1: status color derived from agentActivity + agentStatus, not legacy `state`.
function getStatusColor(activity: AgentActivity | undefined, agentStatus: string | undefined): string {
  const status = activity?.status || agentStatus || '';
  if (status === 'working' || status === 'tool_call_started') return '#39d353';
  if (status === 'thinking') return '#e3b341';
  if (status === 'waiting_approval' || status === 'waiting') return '#f85149';
  return '#8b949e';
}

// PA3 Step 1: animation pacing derived from agentActivity, not legacy state+load.
function getActivityPacing(activity: AgentActivity | undefined, agentStatus: string | undefined): number {
  const status = activity?.status || agentStatus || '';
  if (status === 'working' || status === 'tool_call_started') return 100; // fast
  if (status === 'thinking') return 300;
  return 0; // idle/unknown
}

export function AgentCard({ session, onPress, onSettings, connectionStale }: Props) {
  const [frameIndex, setFrameIndex] = useState(0);
  // PA3 Step 1: use agentKind for color + icon, not legacy runner.
  const runnerColor = '#58a6ff';
  const runnerDef = RUNNERS[0]; // default icon

  // PA3 Step 1: animation driven by agentActivity, not legacy state/load.
  const card = deriveCardActivity(session, { connectionStale });
  const statusColor = getStatusColor(session.agentActivity, session.agentStatus);
  const intervalMs = getActivityPacing(session.agentActivity, session.agentStatus);

  useEffect(() => {
    if (intervalMs === 0) {
      setFrameIndex(0);
      return;
    }
    const interval = setInterval(() => {
      setFrameIndex((prev) => (prev + 1) % runnerDef.frames.length);
    }, intervalMs);
    return () => clearInterval(interval);
  }, [intervalMs, runnerDef.frames.length]);

  const pathD = intervalMs === 0 ? runnerDef.idle : runnerDef.frames[frameIndex];
  const waitingColor = (session.agentActivity?.status === 'waiting_approval' || session.agentStatus === 'waiting_approval') ? '#f85149' : runnerColor;

  // Phase A9 / S1-D: the approval CTA is driven by the approval store ONLY.
  const needsApproval = sessionNeedsApproval(session.approvals);

  return (
    <View style={styles.cardWrapper}>
      <TouchableOpacity
        style={[styles.card, { borderColor: runnerColor }]}
        onPress={onPress}
        activeOpacity={0.8}
      >
        <View style={styles.header}>
          <View style={{ flexDirection: 'row', alignItems: 'center', marginBottom: 2 }}>
            {session.adapter && session.adapter !== 'native' && (
              <Text style={styles.adapterTag}>[{session.adapter}]</Text>
            )}
            <Text style={styles.sessionName} numberOfLines={1}>{session.displayId ?? session.id}</Text>
          </View>
          <View style={{ flexDirection: 'row', alignItems: 'center' }}>
            {needsApproval && (
              <View style={styles.approvalBadge}>
                <Text style={styles.approvalBadgeText}>ACTION</Text>
              </View>
            )}
            <View style={[styles.statusDot, { backgroundColor: statusColor }]} />
            {onSettings && (
              <TouchableOpacity onPress={onSettings} hitSlop={{ top: 10, bottom: 10, left: 10, right: 10 }} style={styles.settingsBtn}>
                <Text style={{ color: '#1E91B3', fontSize: 18, fontWeight: 'bold' }}>⋮</Text>
              </TouchableOpacity>
            )}
          </View>
        </View>

        {(session.agentKind || card.showLegacyStatus) && (
        <View style={{ flexDirection: 'row', marginTop: 6, gap: 8 }}>
          {session.agentKind && (
            <Text style={{ color: isDegraded(session.agentConfidence, session.agentKind) ? '#666' : '#ccc', fontSize: 11 }}>
              🤖 {formatAgentKind(session.agentKind)}
            </Text>
          )}
          {card.showLegacyStatus && session.agentStatus && (
            <Text style={{ color: isDegraded(session.agentConfidence, session.agentKind) ? '#666' : '#888', fontSize: 11 }}>
              {formatAgentStatus(session.agentStatus)}
            </Text>
          )}
        </View>
      )}
      {card.activity && (
        <View style={{ flexDirection: 'row', marginTop: 4, alignItems: 'center' }}>
          <Text style={styles.activityLabel}>ACTIVITY</Text>
          <Text
            testID="agent-activity"
            style={[styles.activityValue, !card.activity.current && styles.activityMuted]}
          >
            {card.activity.label}
          </Text>
        </View>
      )}
      {card.malformed && (
        <View style={{ flexDirection: 'row', marginTop: 4, alignItems: 'center' }}>
          <Text style={styles.activityLabel}>ACTIVITY</Text>
          <Text testID="agent-activity" style={[styles.activityValue, styles.activityMuted]}>
            Unavailable
          </Text>
        </View>
      )}
      {card.unavailable && (
        <View style={{ flexDirection: 'row', marginTop: 4, alignItems: 'center' }}>
          <Text style={styles.activityLabel}>ACTIVITY</Text>
          <Text testID="agent-activity" style={[styles.activityValue, styles.activityMuted]}>
            Unavailable
          </Text>
        </View>
      )}
      <View style={styles.animationContainer}>
          <Svg width={60} height={60} viewBox="0 0 388 388">
            <Path d={pathD} fill={waitingColor} />
          </Svg>
        </View>

        {/* PA3 Step 1: recent edit removed — events field no longer rendered. */}

        <View style={styles.footer}>
          <Text style={[styles.stateText, (session.agentActivity?.status === 'waiting_approval' || session.agentStatus === 'waiting_approval') && {color: '#f85149'}]}>
            {card.activity?.label || (session.agentStatus ? formatAgentStatus(session.agentStatus) : 'IDLE')}
          </Text>
          {/* P1a: observe-only indicator — no live_stream capability. */}
          {session.capabilities && !session.capabilities.includes('live_stream') && (
            <View style={styles.viewOnlyBadge}>
              <Text style={styles.viewOnlyText}>VIEW ONLY</Text>
            </View>
          )}
          {session.capabilities && session.capabilities.includes('live_stream') && (
            <Text style={styles.capTag}>▶</Text>
          )}
        </View>
      </TouchableOpacity>
    </View>
  );
}

const styles = StyleSheet.create({
  cardWrapper: { width: '50%', padding: 6 },
  card: {
    padding: 14, borderRadius: 16,
    borderWidth: 1.5,
    flex: 1,
    minHeight: 140,
    backgroundColor: '#000000',
  },
  header: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', marginBottom: 12 },
  sessionName: { fontSize: 14, fontWeight: '700', color: '#ffffff', flex: 1, marginRight: 8, letterSpacing: 0.96 },
  statusDot: { width: 8, height: 8, borderRadius: 4, marginRight: 8 },
  settingsBtn: { paddingHorizontal: 4 },
  animationContainer: { flex: 1, alignItems: 'center', justifyContent: 'center', marginVertical: 8 },
  footer: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'flex-end', marginTop: 4 },
  stateText: { fontSize: 11, color: '#45EBE9', fontWeight: '700', letterSpacing: 0.96 },
  loadText: { fontSize: 11, color: '#1E91B3', fontWeight: '700' },
  recentEdit: { fontSize: 10, color: '#8b949e', marginBottom: 6, marginTop: -4 },
  approvalBadge: { backgroundColor: '#f85149', paddingHorizontal: 4, paddingVertical: 2, borderRadius: 4, marginRight: 8 },
  approvalBadgeText: { fontSize: 9, color: '#ffffff', fontWeight: 'bold' },
  mirrorBadge: { backgroundColor: '#1f6feb', paddingHorizontal: 4, paddingVertical: 2, borderRadius: 4, marginRight: 8, borderWidth: 1, borderColor: '#58a6ff' },
    adapterTag: { fontSize: 9, color: '#45EBE9', fontWeight: '600', marginRight: 4 },
  viewOnlyBadge: { backgroundColor: '#2C2C2E', paddingHorizontal: 4, paddingVertical: 2, borderRadius: 4, borderWidth: 1, borderColor: '#8b949e' },
	  viewOnlyText: { fontSize: 8, color: '#8b949e', fontWeight: '700', letterSpacing: 0.5 },
	  capTag: { fontSize: 10, color: '#1E91B3', marginLeft: 6 },
  activityLabel: { fontSize: 8, color: '#6E7681', fontWeight: '700', letterSpacing: 0.5, marginRight: 6 },
  activityValue: { fontSize: 11, color: '#8b949e', fontWeight: '600' },
  activityMuted: { color: '#565f6b', fontStyle: 'italic' },
});
