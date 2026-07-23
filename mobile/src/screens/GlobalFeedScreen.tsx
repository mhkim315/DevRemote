import React, { useState, useEffect, useRef } from 'react';
import { listSessions } from '../lib/client';
import { View, Text, StyleSheet, FlatList, ActivityIndicator, Platform } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { SessionTelemetry, AgentEvent } from '../components/AgentCard';
import { EventBubble } from '../components/EventBubble';

interface Props {
  token?: string;
  session?: string;  // deep-linked session from pokit://activity/<session>
  eventId?: string;  // deep-linked event from ?event=<id>
}

export default function GlobalFeedScreen({ token, session, eventId }: Props) {
  const [sessions, setSessions] = useState<SessionTelemetry[]>([]);
  const [loading, setLoading] = useState(true);
  const listRef = useRef<FlatList>(null);

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

  // When deep-linked via pokit://activity/<session>, scope the display to that
  // session's events. When eventId is provided, it is highlighted in the list.
  const scopedToSession = session || undefined;
  const highlightEventId = eventId || undefined;

  // For now, events are session cards — scope to matching session if provided.
  const displaySessions = scopedToSession
    ? sessions.filter(s => s.id === scopedToSession || String(s.id).includes(scopedToSession))
    : sessions;

  return (
    <SafeAreaView style={styles.container} edges={['top', 'left', 'right']}>
      <View style={styles.header}>
        <Text style={styles.headerTitle}>
          {scopedToSession ? `ACTIVITY` : 'GLOBAL FEED'}
        </Text>
        {scopedToSession && (
          <Text style={styles.headerSubtitle} numberOfLines={1}>
            {scopedToSession}{highlightEventId ? ` · event ${highlightEventId.slice(0, 12)}` : ''}
          </Text>
        )}
      </View>
      <View style={styles.content}>
        {loading && displaySessions.length === 0 ? (
          <ActivityIndicator size="large" color="#45EBE9" style={{ marginTop: 40 }} />
        ) : (
          <FlatList
            ref={listRef}
            data={displaySessions}
            keyExtractor={(item, idx) => item.id || String(idx)}
            contentContainerStyle={styles.listContainer}
            renderItem={({ item }) => {
              const isHighlighted = highlightEventId
                ? (item as any).events?.some((e: any) => e.id === highlightEventId)
                : false;
              return (
                <View style={isHighlighted ? styles.highlightedItem : undefined}>
                  <EventBubble event={item} runnerId={item.runnerId} runnerColor={item.runnerColor} agentKind={item.agentKind} />
                </View>
              );
            }}
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
  headerSubtitle: { fontSize: 11, color: '#45EBE9', marginTop: 4, fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace' },
  content: { flex: 1 },
  listContainer: { paddingVertical: 16 },
  emptyText: { color: '#8b949e', textAlign: 'center', marginTop: 40, fontSize: 14 },
  highlightedItem: { borderLeftWidth: 3, borderLeftColor: '#45EBE9', paddingLeft: 8 }
});
