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

export function EventBubble({ event, runnerId, runnerColor, agentKind }: Props) {
  const [expanded, setExpanded] = useState(false);

  const runnerDef = RUNNERS.find(r => r.id === runnerId) || RUNNERS[0];
  const color = runnerColor || '#58a6ff';
  const pathD = runnerDef.idle;

  const d = new Date(event.timestamp);
  const timeStr = d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });

  const isUser = event.type === 'user' || event.type === 'user_message';
  const isTool = event.type === 'tool_use' || event.type === 'tool_call_started' || event.type === 'file_edit' || event.type === 'approval_request' || event.type === 'approval_requested';
  const isResult = event.type === 'tool_result' || event.type === 'tool_call_finished';
  const isMessage = event.type === 'message' || event.type === 'assistant_message' || event.type === 'thinking' || event.type === 'agent_started';

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

  if (isResult && !expanded) {
    return (
      <View style={[styles.container, styles.leftAlign]}>
        <View style={styles.totemPlaceholder} />
        <TouchableOpacity style={styles.resultCollapsed} onPress={() => setExpanded(true)}>
          <Text style={styles.resultCollapsedText} selectable={true}>✅ {event.summary} (Tap to expand)</Text>
          <Text style={styles.time}>{timeStr}</Text>
        </TouchableOpacity>
      </View>
    );
  }

  return (
    <View style={[styles.container, styles.leftAlign]}>
      {isMessage || isTool ? (
        <View style={styles.totemContainer}>
          <Svg width={30} height={30} viewBox="0 0 388 388">
            <Path d={pathD} fill={color} />
          </Svg>
        </View>
      ) : (
        <View style={styles.totemPlaceholder} />
      )}
      
      <View style={[
        styles.bubble, 
        isTool ? styles.toolBubble : (isResult ? styles.resultBubble : styles.botBubble),
        isTool && { borderColor: color, borderWidth: 1 }
      ]}>
        <View style={styles.header}>
          <Text style={[styles.sessionName, isTool && { color: color }]}>
            {isTool ? '🛠️ ' + event.summary : (isResult ? '✅ ' + event.summary : '🤖 ' + (agentKind || runnerId || 'Agent'))}
          </Text>
        </View>
        <Text style={[styles.detail, isResult && styles.resultDetail]} selectable={true}>
          {event.detail || event.summary}
        </Text>
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
  resultBubble: {
    backgroundColor: '#121214',
    borderBottomLeftRadius: 4,
    borderWidth: 1,
    borderColor: '#2C2C2E',
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
  resultDetail: {
    color: '#8b949e',
    fontSize: 11,
  }
});
