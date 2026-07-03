import React, { useState, useEffect } from 'react';
import { View, Text, StyleSheet, SafeAreaView, ActivityIndicator, FlatList } from 'react-native';
import { AgentCard, SessionTelemetry } from '../../components/AgentCard';

interface Props {
  onSelectAgent: (sessionName: string) => void;
}

export default function DashboardScreen({ onSelectAgent }: Props) {
  const [sessions, setSessions] = useState<SessionTelemetry[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const fetchSessions = () => {
      fetch('https://term.fullcount.kr/api/sessions')
        .then(res => res.json())
        .then(data => {
          const normalized = (data || []).map((s: any) =>
            typeof s === 'string' ? { id: s, state: 'idle', load: 0 } : s
          );
          setSessions(normalized);
          setLoading(false);
        })
        .catch(err => {
          console.error(err);
          setLoading(false);
        });
    };

    fetchSessions(); // initial fetch

    // Poll every 1.5 seconds to get real-time state for animations
    const interval = setInterval(fetchSessions, 1500);
    return () => clearInterval(interval);
  }, []);

  return (
    <SafeAreaView style={styles.container}>
      <View style={styles.header}>
        <Text style={styles.headerTitle}>POKIT Agents</Text>
        <View style={styles.badge}><Text style={styles.badgeText}>Live Monitor</Text></View>
      </View>

      <View style={styles.content}>
        {loading ? (
          <ActivityIndicator size="large" color="#58a6ff" style={{ marginTop: 40 }} />
        ) : sessions.length === 0 ? (
          <Text style={styles.emptyText}>No active agents found.</Text>
        ) : (
          <FlatList
            data={sessions}
            keyExtractor={(item) => item.id}
            numColumns={2}
            contentContainerStyle={styles.gridContainer}
            renderItem={({ item }) => (
              <AgentCard
                session={item}
                onPress={() => onSelectAgent(item.id)}
              />
            )}
          />
        )}
      </View>
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
  content: { flex: 1 },
  gridContainer: {
    padding: 12,
  },
  emptyText: { color: '#8b949e', textAlign: 'center', marginTop: 40, fontSize: 16 }
});
