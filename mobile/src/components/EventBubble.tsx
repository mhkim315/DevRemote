import React from 'react';
import { View, Text, StyleSheet, Platform } from 'react-native';
import { AgentEvent } from './AgentCard';
import Svg, { Path } from 'react-native-svg';
import { RUNNERS } from '../lib/runners';

interface Props {
  event: AgentEvent;
  runnerId?: string;
  runnerColor?: string;
}

export function EventBubble({ event, runnerId, runnerColor }: Props) {
  const runnerDef = RUNNERS.find(r => r.id === runnerId) || RUNNERS[0];
  const color = runnerColor || '#58a6ff';
  const pathD = runnerDef.idle;

  const d = new Date(event.timestamp);
  const timeStr = d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });

  return (
    <View style={styles.container}>
      <View style={styles.totemContainer}>
        <Svg width={30} height={30} viewBox="0 0 388 388">
          <Path d={pathD} fill={color} />
        </Svg>
      </View>
      <View style={[styles.bubble, { borderColor: color }]}>
        <View style={styles.header}>
          <Text style={styles.sessionName}>{event.session}</Text>
          <Text style={styles.time}>{timeStr}</Text>
        </View>
        <Text style={styles.detail}>{event.detail || event.summary}</Text>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    flexDirection: 'row',
    marginBottom: 16,
    alignItems: 'flex-start',
    paddingHorizontal: 16,
  },
  totemContainer: {
    width: 40,
    height: 40,
    borderRadius: 20,
    backgroundColor: '#000000',
    borderWidth: 1,
    borderColor: '#0D2D45',
    justifyContent: 'center',
    alignItems: 'center',
    marginRight: 12,
  },
  bubble: {
    flex: 1,
    backgroundColor: '#000000',
    borderWidth: 1,
    borderRadius: 12,
    padding: 12,
    borderTopLeftRadius: 4,
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
    letterSpacing: 0.96,
  },
  time: {
    color: '#8b949e',
    fontSize: 10,
  },
  detail: {
    color: '#45EBE9',
    fontSize: 13,
    lineHeight: 18,
    fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace',
  }
});
