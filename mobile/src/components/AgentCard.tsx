import React, { useEffect, useRef } from 'react';
import { View, Text, StyleSheet, TouchableOpacity, Animated } from 'react-native';
import { FontAwesome5 } from '@expo/vector-icons';
import { RUNNERS } from '../lib/runners';

export interface SessionTelemetry {
  id: string;
  state: 'idle' | 'thinking' | 'working' | 'waiting';
  load: number;
  runner?: string;
  runnerColor?: string;
  isAddBtn?: boolean;
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
  const scale = useRef(new Animated.Value(1)).current;
  const runnerDef = RUNNERS.find(r => r.id === session.runner) || RUNNERS[0];
  const runnerColor = session.runnerColor || '#58a6ff';

  useEffect(() => {
    let intervalMs = 1000;
    if (session.state === 'working') {
      intervalMs = Math.max(100, 300 - (session.load * 2));
    } else if (session.state === 'thinking') {
      intervalMs = 400;
    } else if (session.state === 'waiting' || session.state === 'idle') {
      intervalMs = 0;
    }

    if (intervalMs === 0) {
      scale.setValue(1);
      return;
    }

    const anim = Animated.loop(
      Animated.sequence([
        Animated.timing(scale, { toValue: 1.15, duration: intervalMs / 2, useNativeDriver: true }),
        Animated.timing(scale, { toValue: 1, duration: intervalMs / 2, useNativeDriver: true })
      ])
    );
    anim.start();

    return () => anim.stop();
  }, [session.state, session.load, scale]);

  return (
    <View style={styles.cardWrapper}>
      <TouchableOpacity 
        style={[styles.card, { borderColor: runnerColor, backgroundColor: runnerColor + '15' }]} 
        onPress={onPress} 
        activeOpacity={0.8}
      >
        <View style={styles.header}>
          <Text style={styles.sessionName} numberOfLines={1}>{session.id}</Text>
          <View style={[styles.statusDot, { backgroundColor: getStatusColor(session.state) }]} />
        </View>

        <View style={styles.animationContainer}>
          <Animated.View style={{ transform: [{ scale }] }}>
            <FontAwesome5 name={runnerDef.icon} size={36} color={runnerColor} />
          </Animated.View>
        </View>

        <View style={styles.footer}>
          <Text style={[styles.stateText, session.state === 'waiting' && {color: '#f85149'}]}>
            {session.state.toUpperCase()}
          </Text>
          {session.state === 'working' && (
            <Text style={styles.loadText}>{session.load}%</Text>
          )}
        </View>
      </TouchableOpacity>
      
      {onSettings && (
        <TouchableOpacity style={styles.settingsBtn} onPress={onSettings} hitSlop={{ top: 10, bottom: 10, left: 10, right: 10 }}>
          <FontAwesome5 name="ellipsis-v" size={14} color="#8b949e" />
        </TouchableOpacity>
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  cardWrapper: { width: '50%', padding: 6, position: 'relative' },
  card: {
    padding: 14, borderRadius: 16,
    borderWidth: 1.5,
    flex: 1,
    minHeight: 140,
  },
  header: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', marginBottom: 12 },
  sessionName: { fontSize: 13, fontWeight: '700', color: '#fff', flex: 1, marginRight: 8 },
  statusDot: { width: 8, height: 8, borderRadius: 4 },
  animationContainer: { flex: 1, alignItems: 'center', justifyContent: 'center', marginVertical: 8 },
  footer: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'flex-end', marginTop: 4 },
  stateText: { fontSize: 11, color: '#e3b341', fontWeight: '600' },
  loadText: { fontSize: 11, color: '#8b949e', fontWeight: '600' },
  settingsBtn: { position: 'absolute', top: 16, right: 16, width: 24, height: 24, alignItems: 'center', justifyContent: 'center' }
});
