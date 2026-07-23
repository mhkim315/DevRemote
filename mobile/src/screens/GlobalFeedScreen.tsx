import React, { useEffect, useState } from 'react';
import { View, Text, StyleSheet, FlatList, ActivityIndicator, Platform } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { getCockpit } from '../lib/client';

interface Props {
  token?: string;
  session?: string;
  eventId?: string;
}

type TimelineEvent = {
  id: string;
  sessionId: string;
  runtimeId?: string;
  generation?: string;
  kind: string;
};

// The Cockpit notification projection is backed by the Timeline writer and
// carries canonical event IDs. SessionTelemetry intentionally has no events,
// so it must never be used to resolve a notification deep link.
function timelineEvents(value: unknown): TimelineEvent[] {
  const notifications = (value as any)?.notifications;
  if (!Array.isArray(notifications)) return [];
  return notifications.flatMap((item: any) => {
    const id = item?.origin?.evidenceRef || item?.summary;
    const sessionId = item?.origin?.sessionId;
    if (item?.kind !== 'notification' || typeof id !== 'string' || typeof sessionId !== 'string') return [];
    return [{ id, sessionId, runtimeId: item.origin.runtimeId, generation: item.origin.generation, kind: String(item.state || 'event') }];
  });
}

export default function GlobalFeedScreen({ token, session, eventId }: Props) {
  const [events, setEvents] = useState<TimelineEvent[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let live = true;
    const refresh = () => getCockpit(token)
      .then(data => { if (live) setEvents(timelineEvents(data)); })
      .catch(err => console.warn('timeline activity unavailable', err))
      .finally(() => { if (live) setLoading(false); });
    refresh();
    const interval = setInterval(refresh, 3000);
    return () => { live = false; clearInterval(interval); };
  }, [token]);

  const scoped = session ? events.filter(e => e.sessionId === session) : events;
  const ordered = eventId
    ? [...scoped].sort((a, b) => Number(b.id === eventId) - Number(a.id === eventId))
    : scoped;

  return (
    <SafeAreaView style={styles.container} edges={['top', 'left', 'right']}>
      <View style={styles.header}>
        <Text style={styles.headerTitle}>{session ? 'ACTIVITY' : 'GLOBAL FEED'}</Text>
        {session && <Text style={styles.headerSubtitle} numberOfLines={1}>{session}{eventId ? ` · event ${eventId.slice(0, 12)}` : ''}</Text>}
      </View>
      {loading ? <ActivityIndicator size="large" color="#45EBE9" style={{ marginTop: 40 }} /> : (
        <FlatList
          data={ordered}
          keyExtractor={item => item.id}
          contentContainerStyle={styles.listContainer}
          renderItem={({ item }) => <View style={[styles.eventCard, item.id === eventId && styles.highlightedItem]}>
            {item.id === eventId && <Text style={styles.highlightLabel}>NOTIFICATION EVENT</Text>}
            <Text style={styles.eventKind}>{item.kind}</Text>
            <Text style={styles.eventID} selectable>{item.id}</Text>
            <Text style={styles.eventMeta}>{item.sessionId} · generation {item.generation || 'unknown'}</Text>
          </View>}
          ListEmptyComponent={<Text style={styles.emptyText}>{eventId ? 'This event is no longer in the retained Activity timeline.' : 'No activity yet.'}</Text>}
        />
      )}
    </SafeAreaView>
  );
}

export { timelineEvents };

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#000000' },
  header: { paddingHorizontal: 20, paddingVertical: 16, borderBottomWidth: 1, borderBottomColor: '#0D2D45', alignItems: 'center' },
  headerTitle: { fontSize: 20, fontWeight: '800', color: '#ffffff', letterSpacing: 1.2, fontFamily: Platform.OS === 'ios' ? 'HelveticaNeue-CondensedBold' : 'sans-serif-condensed' },
  headerSubtitle: { fontSize: 11, color: '#45EBE9', marginTop: 4, fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace' },
  listContainer: { padding: 16 },
  eventCard: { borderWidth: 1, borderColor: '#0D2D45', borderRadius: 10, padding: 14, marginBottom: 10, backgroundColor: '#07131d' },
  highlightedItem: { borderColor: '#45EBE9', borderWidth: 2 },
  highlightLabel: { color: '#45EBE9', fontSize: 10, fontWeight: '800', marginBottom: 6 },
  eventKind: { color: '#ffffff', fontWeight: '700', fontSize: 14 },
  eventID: { color: '#45EBE9', fontSize: 11, marginTop: 6, fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace' },
  eventMeta: { color: '#8b949e', fontSize: 11, marginTop: 6 },
  emptyText: { color: '#8b949e', textAlign: 'center', marginTop: 40, fontSize: 14 },
});
