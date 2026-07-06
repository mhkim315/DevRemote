import React, { useState, useEffect } from 'react';
import { listSessions } from '../lib/client';
import { View, Text, StyleSheet, FlatList, ActivityIndicator, Platform } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { SessionTelemetry, AgentEvent } from '../components/AgentCard';
import { EventBubble } from '../components/EventBubble';

interface Props {
  token?: string;
}

export default function GlobalFeedScreen({ token }: Props) {
  const [sessions, setSessions] = useState<SessionTelemetry[]>([]);
  const [loading, setLoading] = useState(true);

  const fetchSessions = () => {
    listSessions(token)
      .then(data => {
        const normalized = (data || []).map((s: any) =>
          typeof s === 'string' ? { id: s, state: 'idle', load: 0 } : s
        ).sort((a: SessionTelemetry, b: SessionTelemetry) => String(a.id).localeCompare(String(b.id)));
        setSessions(normalized);
        setLoading(false);
      })
      .catch(err => {
        console.error(err);
        setLoading(false);
      });
  };

  useEffect(() => {
    fetchSessions();
    const interval = setInterval(fetchSessions, 3000); // Polling every 3s
    return () => clearInterval(interval);
  }, []);

  const allEvents = React.useMemo(() => {
    const eventsWithRunner: (AgentEvent & { runnerId?: string, runnerColor?: string })[] = [];
    sessions.forEach(s => {
      if (s.events) {
        s.events.forEach(e => {
          eventsWithRunner.push({
            ...e,
            runnerId: s.runner,
            runnerColor: s.runnerColor,
          });
        });
      }
    });
    // Sort oldest to newest
    return eventsWithRunner.sort((a, b) => new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime());
  }, [sessions]);

  return (
    <SafeAreaView style={styles.container} edges={['top', 'left', 'right']}>
      <View style={styles.header}>
        <Text style={styles.headerTitle}>GLOBAL FEED</Text>
      </View>
      <View style={styles.content}>
        {loading && allEvents.length === 0 ? (
          <ActivityIndicator size="large" color="#45EBE9" style={{ marginTop: 40 }} />
        ) : (
          <FlatList
            data={allEvents}
            keyExtractor={(item, idx) => item.id || String(idx)}
            contentContainerStyle={styles.listContainer}
            renderItem={({ item }) => (
              <EventBubble event={item} runnerId={item.runnerId} runnerColor={item.runnerColor} />
            )}
            ListEmptyComponent={
              <Text style={styles.emptyText}>No activity yet.</Text>
            }
          />
        )}
      </View>
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#000000' },
  header: {
    paddingHorizontal: 20, paddingVertical: 16, borderBottomWidth: 1, borderBottomColor: '#0D2D45',
    alignItems: 'center'
  },
  headerTitle: { fontSize: 20, fontWeight: '800', color: '#ffffff', letterSpacing: 1.2, fontFamily: Platform.OS === 'ios' ? 'HelveticaNeue-CondensedBold' : 'sans-serif-condensed' },
  content: { flex: 1 },
  listContainer: { paddingVertical: 16 },
  emptyText: { color: '#8b949e', textAlign: 'center', marginTop: 40, fontSize: 14 }
});
