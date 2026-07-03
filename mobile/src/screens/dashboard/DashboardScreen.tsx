import React, { useState, useEffect } from 'react';
import { View, Text, StyleSheet, TouchableOpacity, ScrollView, SafeAreaView, ActivityIndicator } from 'react-native';

interface Props {
  onSelectAgent: (sessionName: string) => void;
}

export default function DashboardScreen({ onSelectAgent }: Props) {
  const [sessions, setSessions] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    fetch('https://term.fullcount.kr/api/sessions')
      .then(res => res.json())
      .then(data => {
        setSessions(data || []);
        setLoading(false);
      })
      .catch(err => {
        console.error(err);
        setLoading(false);
      });
  }, []);

  return (
    <SafeAreaView style={styles.container}>
      <View style={styles.header}>
        <Text style={styles.headerTitle}>DevRemote</Text>
        <View style={styles.badge}><Text style={styles.badgeText}>Local Daemon</Text></View>
      </View>

      <ScrollView contentContainerStyle={styles.content}>
        <Text style={styles.sectionTitle}>Active Sessions (Agents)</Text>
        
        {loading ? (
          <ActivityIndicator size="large" color="#58a6ff" style={{marginTop: 20}} />
        ) : sessions.length === 0 ? (
          <Text style={{color:'#8b949e', textAlign:'center', marginTop:20}}>No active sessions found.</Text>
        ) : (
          sessions.map((sessionName) => (
            <TouchableOpacity key={sessionName} style={styles.card} onPress={() => onSelectAgent(sessionName)} activeOpacity={0.8}>
              <View style={styles.cardHeader}>
                <View style={styles.dot} />
                <Text style={styles.cardTitle}>{sessionName}</Text>
              </View>
              <Text style={styles.cardSubtitle}>Terminal Observer</Text>
              <View style={styles.statusRow}>
                <Text style={styles.statusText}>Awaiting input</Text>
                <Text style={styles.timeText}>Active</Text>
              </View>
            </TouchableOpacity>
          ))
        )}
      </ScrollView>
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#090a0f' },
  header: {
    flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center',
    paddingHorizontal: 20, paddingVertical: 16, borderBottomWidth: 1, borderBottomColor: '#1e212b'
  },
  headerTitle: { fontSize: 24, fontWeight: '700', color: '#fff', letterSpacing: -0.5 },
  badge: { backgroundColor: '#161b22', paddingHorizontal: 10, paddingVertical: 4, borderRadius: 12, borderWidth: 1, borderColor: '#30363d' },
  badgeText: { color: '#58a6ff', fontSize: 12, fontWeight: '600' },
  content: { padding: 20 },
  sectionTitle: { fontSize: 18, fontWeight: '600', color: '#8b949e', marginBottom: 16 },
  card: {
    backgroundColor: '#161b22', padding: 16, borderRadius: 16,
    borderWidth: 1, borderColor: '#30363d', marginBottom: 12,
  },
  cardInactive: { opacity: 0.6 },
  cardHeader: { flexDirection: 'row', alignItems: 'center', marginBottom: 4 },
  dot: { width: 8, height: 8, borderRadius: 4, backgroundColor: '#238636', marginRight: 8 },
  dotInactive: { backgroundColor: '#484f58' },
  cardTitle: { fontSize: 16, fontWeight: '600', color: '#c9d1d9' },
  cardSubtitle: { fontSize: 14, color: '#8b949e', marginLeft: 16, marginBottom: 12 },
  statusRow: { flexDirection: 'row', justifyContent: 'space-between', marginLeft: 16 },
  statusText: { fontSize: 12, color: '#e3b341', fontWeight: '500' },
  timeText: { fontSize: 12, color: '#484f58' }
});
