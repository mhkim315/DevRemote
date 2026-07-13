import React, { useState, useEffect } from 'react';
import { View, Text, StyleSheet, TouchableOpacity } from 'react-native';
import Svg, { Path } from 'react-native-svg';
import { RUNNERS } from '../lib/runners';
import { formatAgentKind, formatAgentStatus, isDegraded } from '../lib/agentDisplay';
import { AgentActivity, deriveCardActivity, sessionNeedsApproval } from '../lib/agentActivity';
import { AgentApproval } from '../lib/client';

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
  state: 'idle' | 'thinking' | 'working' | 'waiting';
  // M3b: daemon-authoritative managed lifecycle state (Session Catalog), separate
  // from `state` (agent activity). Empty/absent for non-managed sessions.
  lifecycleState?: string;
  load: number;
  runner?: string;
  runnerColor?: string;
  adapter?: string;
  capabilities?: string[];
  adapterCapabilities?: string[];
  isAddBtn?: boolean;
  events?: AgentEvent[];
  agentKind?: string;
  agentStatus?: string;
  agentConfidence?: number;
  approvals?: AgentApproval[];
  // S1-D: advisory agent-activity dimension (validated at render), SEPARATE from
  // `state`/`lifecycleState`. Never drives lifecycle or approval actions.
  agentActivity?: AgentActivity;
}

interface Props {
  session: SessionTelemetry;
  onPress: () => void;
  onSettings?: () => void;
}

function getStatusColor(state: SessionTelemetry['state']) {
  switch (state) {
    case 'working': return '#39d353';
    case 'thinking': return '#e3b341';
    case 'waiting': return '#f85149';
    default: return '#8b949e';
  }
}

export function AgentCard({ session, onPress, onSettings }: Props) {
  const [frameIndex, setFrameIndex] = useState(0);
  const runnerDef = RUNNERS.find(r => r.id === session.runner) || RUNNERS[0];
  const runnerColor = session.runnerColor || '#58a6ff';

  useEffect(() => {
    let intervalMs = 1000;
    if (session.state === 'working') {
      intervalMs = Math.max(50, 200 - session.load); // Fast!
    } else if (session.state === 'thinking') {
      intervalMs = 300;
    } else if (session.state === 'waiting' || session.state === 'idle') {
      intervalMs = 0;
    }

    if (intervalMs === 0) {
      setFrameIndex(0);
      return;
    }

    const interval = setInterval(() => {
      setFrameIndex((prev) => (prev + 1) % runnerDef.frames.length);
    }, intervalMs);

    return () => clearInterval(interval);
  }, [session.state, session.load, runnerDef.frames.length]);

  const pathD = (session.state === 'idle' || session.state === 'waiting') 
    ? runnerDef.idle 
    : runnerDef.frames[frameIndex];

  const recentEdit = session.events?.slice().reverse().find(e => e.type === 'file_edit');
  // Phase A9 / S1-D: the approval CTA is driven by the approval store ONLY —
  // agent activity (even `waiting_approval`) never creates it.
  const needsApproval = sessionNeedsApproval(session.approvals);
  // S1-D: the card's activity render decision. When an accepted `agentActivity`
  // DTO exists it governs the activity dimension; the legacy heuristic
  // `agentStatus` is not shown as a peer authority and is never a fallback for a
  // malformed/future DTO.
  const card = deriveCardActivity(session);

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
            <View style={[styles.statusDot, { backgroundColor: getStatusColor(session.state) }]} />
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
          {/* Legacy heuristic status — shown ONLY when there is no accepted DTO. */}
          {card.showLegacyStatus && session.agentStatus && (
            <Text style={{ color: isDegraded(session.agentConfidence, session.agentKind) ? '#666' : '#888', fontSize: 11 }}>
              {formatAgentStatus(session.agentStatus)}
            </Text>
          )}
        </View>
      )}
      {/* S1-D: accepted agent-activity dimension, rendered SEPARATELY from session
          state / lifecycle. A stale/degraded record is labelled as a non-current
          state (never "Working"/"Thinking") and dimmed. A malformed/future DTO
          shows an explicit unavailable marker — never the legacy heuristic. */}
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
      <View style={styles.animationContainer}>
          <Svg width={60} height={60} viewBox="0 0 388 388">
            <Path d={pathD} fill={session.state === 'waiting' ? '#f85149' : runnerColor} />
          </Svg>
        </View>

        {recentEdit && (
          <Text style={styles.recentEdit} numberOfLines={1}>
            📝 {recentEdit.summary}
          </Text>
        )}

        <View style={styles.footer}>
          <Text style={[styles.stateText, session.state === 'waiting' && {color: '#f85149'}]}>
            {session.state === 'idle' ? 'SLEEPING' : session.state.toUpperCase()}
          </Text>
          {session.state === 'working' && (
            <Text style={styles.loadText}>{session.load}%</Text>
          )}
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
