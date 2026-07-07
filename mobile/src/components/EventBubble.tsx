import React, { useState } from 'react';
import { View, Text, StyleSheet, Platform, TouchableOpacity } from 'react-native';
import { AgentEvent } from './AgentCard';
import Svg, { Path } from 'react-native-svg';
import { RUNNERS } from '../lib/runners';

interface Props {
  event: AgentEvent;
  runnerId?: string;
  runnerColor?: string;
  agentKind?: string;
}

// P2: event type → bubble category mapping.
// Category is determined by event.type, never by agentKind.
type BubbleCategory = 'message' | 'thinking' | 'tool_start' | 'tool_result' | 'interaction' | 'error' | 'completed' | 'unknown';

function categoryForType(type: string): BubbleCategory {
  switch (type) {
    case 'user':
    case 'user_message':
    case 'message':
    case 'assistant_message':
    case 'agent_started':
      return 'message';
    case 'thinking':
      return 'thinking';
    case 'tool_use':
    case 'tool_call_started':
      return 'tool_start';
    case 'tool_result':
    case 'tool_call_finished':
      return 'tool_result';
    case 'approval_request':
    case 'approval_requested':
    case 'approval_resolved':
      return 'interaction';
    case 'error':
    case 'failed':
      return 'error';
    case 'done':
    case 'completed':
      return 'completed';
    default:
      return 'unknown';
  }
}

function categoryEmoji(cat: BubbleCategory): string {
  switch (cat) {
    case 'message': return '💬';
    case 'thinking': return '💭';
    case 'tool_start': return '⚙';
    case 'tool_result': return '📋';
    case 'interaction': return '⚠';
    case 'error': return '❌';
    case 'completed': return '✅';
    case 'unknown': return '❓';
  }
}

function categoryColor(cat: BubbleCategory): string {
  switch (cat) {
    case 'interaction': return '#f85149';
    case 'error': return '#f85149';
    case 'tool_start': return '#58a6ff';
    case 'completed': return '#39d353';
    case 'thinking': return '#8b949e';
    case 'unknown': return '#666';
    default: return '#58a6ff';
  }
}

export function EventBubble({ event, runnerId, runnerColor, agentKind }: Props) {
  const [expanded, setExpanded] = useState(false);
  const category = categoryForType(event.type);
  const emoji = categoryEmoji(category);
  const accentColor = categoryColor(category);

  const runnerDef = RUNNERS.find(r => r.id === runnerId) || RUNNERS[0];
  const totemColor = runnerColor || accentColor;
  const pathD = runnerDef.idle;

  const d = new Date(event.timestamp);
  const timeStr = d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });

  const isUser = category === 'message' && (event.type === 'user' || event.type === 'user_message');

  // ── User message (right-aligned) ──
  if (isUser) {
    return (
      <View style={[styles.container, styles.rightAlign]}>
        <View style={styles.timeWrapperRight}>
          <Text style={styles.timeRight}>{timeStr}</Text>
        </View>
        <View style={[styles.bubble, styles.userBubble]}>
          <Text style={styles.userText} selectable={true}>{event.detail || event.summary}</Text>
        </View>
      </View>
    );
  }

  // ── Tool result (collapsed by default) ──
  if (category === 'tool_result' && !expanded) {
    return (
      <View style={[styles.container, styles.leftAlign]}>
        <View style={styles.totemPlaceholder} />
        <TouchableOpacity style={styles.resultCollapsed} onPress={() => setExpanded(true)}>
          <Text style={styles.resultCollapsedText} selectable={true}>{emoji} {event.summary}</Text>
          <Text style={styles.time}>{timeStr}</Text>
        </TouchableOpacity>
      </View>
    );
  }

  // ── Interaction (attention signal) ──
  if (category === 'interaction') {
    return (
      <View style={[styles.container, styles.leftAlign]}>
        <View style={styles.totemContainer}>
          <Text style={{ fontSize: 18 }}>{emoji}</Text>
        </View>
        <View style={[styles.bubble, styles.interactionBubble]}>
          <View style={styles.header}>
            <Text style={[styles.sessionName, { color: accentColor }]}>
              {emoji} Attention Required
            </Text>
          </View>
          <Text style={styles.detail} selectable={true}>
            {event.summary || event.detail || 'Interaction request'}
          </Text>
        </View>
        <View style={styles.timeWrapperLeft}>
          <Text style={styles.time}>{timeStr}</Text>
        </View>
      </View>
    );
  }

  // ── Error ──
  if (category === 'error') {
    return (
      <View style={[styles.container, styles.leftAlign]}>
        <View style={styles.totemPlaceholder} />
        <View style={[styles.bubble, styles.errorBubble]}>
          <View style={styles.header}>
            <Text style={[styles.sessionName, { color: accentColor }]}>
              {emoji} Error
            </Text>
          </View>
          <Text style={styles.detail} selectable={true}>
            {event.summary || event.detail || 'An error occurred'}
          </Text>
        </View>
        <View style={styles.timeWrapperLeft}>
          <Text style={styles.time}>{timeStr}</Text>
        </View>
      </View>
    );
  }

  // ── Completed ──
  if (category === 'completed') {
    return (
      <View style={[styles.container, styles.leftAlign]}>
        <View style={styles.totemPlaceholder} />
        <View style={[styles.bubble, styles.completedBubble]}>
          <Text style={[styles.sessionName, { color: accentColor }]}>
            {emoji} {event.summary || 'Completed'}
          </Text>
        </View>
        <View style={styles.timeWrapperLeft}>
          <Text style={styles.time}>{timeStr}</Text>
        </View>
      </View>
    );
  }

  // ── Unknown (dimmed fallback) ──
  if (category === 'unknown') {
    return (
      <View style={[styles.container, styles.leftAlign]}>
        <View style={styles.totemPlaceholder} />
        <View style={[styles.bubble, styles.unknownBubble]}>
          <Text style={styles.unknownText} selectable={true}>
            {emoji} {event.type || 'Unknown event'}
          </Text>
          <Text style={styles.time}>{timeStr}</Text>
        </View>
      </View>
    );
  }

  // ── Message / Thinking / Tool (agent-side, with totem) ──
  const isTool = category === 'tool_start';
  const isThinking = category === 'thinking';
  const titleText = isTool
    ? `${emoji} ${event.summary || 'Tool call'}`
    : isThinking
    ? `${emoji} Thinking`
    : `${emoji} ${agentKind || runnerId || 'Agent'}`;

  return (
    <View style={[styles.container, styles.leftAlign]}>
      <View style={styles.totemContainer}>
        <Svg width={30} height={30} viewBox="0 0 388 388">
          <Path d={pathD} fill={totemColor} />
        </Svg>
      </View>
      <View style={[
        styles.bubble,
        isTool ? styles.toolBubble : styles.botBubble,
        isThinking && styles.thinkingBubble,
        isTool && { borderColor: accentColor, borderWidth: 1 }
      ]}>
        <View style={styles.header}>
          <Text style={[
            styles.sessionName,
            isTool && { color: accentColor },
            isThinking && { color: '#8b949e', fontStyle: 'italic' }
          ]}>
            {titleText}
          </Text>
        </View>
        {(event.detail || isTool) && (
          <Text style={[styles.detail, isThinking && styles.thinkingDetail]} selectable={true}>
            {event.detail || event.summary}
          </Text>
        )}
      </View>
      <View style={styles.timeWrapperLeft}>
        <Text style={styles.time}>{timeStr}</Text>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    flexDirection: 'row',
    marginBottom: 16,
    paddingHorizontal: 16,
    alignItems: 'flex-end',
  },
  leftAlign: {
    justifyContent: 'flex-start',
  },
  rightAlign: {
    justifyContent: 'flex-end',
  },
  totemContainer: {
    width: 36,
    height: 36,
    borderRadius: 18,
    backgroundColor: '#000000',
    borderWidth: 1,
    borderColor: '#0D2D45',
    justifyContent: 'center',
    alignItems: 'center',
    marginRight: 8,
    marginBottom: 4,
  },
  totemPlaceholder: {
    width: 36,
    height: 36,
    marginRight: 8,
  },
  bubble: {
    maxWidth: '75%',
    padding: 12,
    borderRadius: 16,
  },
  userBubble: {
    backgroundColor: '#0A84FF',
    borderBottomRightRadius: 4,
  },
  botBubble: {
    backgroundColor: '#2C2C2E',
    borderBottomLeftRadius: 4,
  },
  toolBubble: {
    backgroundColor: '#1C1C1E',
    borderBottomLeftRadius: 4,
  },
  thinkingBubble: {
    backgroundColor: '#1C1C1E',
    opacity: 0.7,
  },
  interactionBubble: {
    backgroundColor: '#1C1C1E',
    borderBottomLeftRadius: 4,
    borderWidth: 1.5,
    borderColor: '#f85149',
  },
  errorBubble: {
    backgroundColor: '#2C1414',
    borderBottomLeftRadius: 4,
    borderWidth: 1,
    borderColor: '#f85149',
  },
  completedBubble: {
    backgroundColor: '#1C2C1C',
    borderBottomLeftRadius: 4,
    borderWidth: 1,
    borderColor: '#39d353',
  },
  unknownBubble: {
    backgroundColor: '#1C1C1E',
    borderBottomLeftRadius: 4,
    borderWidth: 1,
    borderColor: '#333',
    borderStyle: 'dashed',
  },
  resultCollapsed: {
    backgroundColor: '#121214',
    paddingHorizontal: 16,
    paddingVertical: 10,
    borderRadius: 16,
    borderBottomLeftRadius: 4,
    borderWidth: 1,
    borderColor: '#2C2C2E',
    maxWidth: '75%',
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
  },
  resultCollapsedText: {
    color: '#8b949e',
    fontSize: 12,
    marginRight: 12,
  },
  userText: {
    color: '#ffffff',
    fontSize: 14,
    lineHeight: 20,
  },
  header: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    marginBottom: 6,
  },
  sessionName: {
    color: '#ffffff',
    fontSize: 12,
    fontWeight: '700',
  },
  timeWrapperLeft: {
    marginLeft: 8,
    marginBottom: 4,
  },
  timeWrapperRight: {
    marginRight: 8,
    marginBottom: 4,
  },
  time: {
    color: '#8b949e',
    fontSize: 10,
  },
  timeRight: {
    color: '#8b949e',
    fontSize: 10,
    textAlign: 'right',
  },
  detail: {
    color: '#E5E5EA',
    fontSize: 13,
    lineHeight: 18,
    fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace',
  },
  thinkingDetail: {
    color: '#8b949e',
    fontStyle: 'italic',
    fontSize: 12,
  },
  unknownText: {
    color: '#8b949e',
    fontSize: 12,
  },
});
