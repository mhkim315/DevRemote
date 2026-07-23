import React, { useEffect, useState } from 'react';
import { Linking, View, Text, StyleSheet, ActivityIndicator, Platform, TouchableOpacity } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { getNotificationStatus, type NotificationStatus } from '../lib/client';
import { exactStatusEvent } from '../lib/notificationEvent';

interface Props {
  token?: string;
  session?: string;
  eventId?: string;
  generation?: number;
  runtimeId?: string;
}

export default function GlobalFeedScreen({ token, session, eventId, generation, runtimeId }: Props) {
  const [status, setStatus] = useState<NotificationStatus | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let live = true;
    if (!session || !eventId || !Number.isFinite(generation)) {
      setLoading(false);
      return () => { live = false; };
    }
    getNotificationStatus(token || '', eventId, session, generation!, runtimeId)
      .then(value => { if (live) setStatus(value); })
      .catch(err => console.warn('notification event unavailable', err))
      .finally(() => { if (live) setLoading(false); });
    return () => { live = false; };
  }, [token, session, eventId, generation, runtimeId]);

  const event = exactStatusEvent(status, eventId);
  const missingTarget = !loading && !event;
  const openTerminal = () => session && Linking.openURL(`pokit://session/${encodeURIComponent(session)}?notice=event_missing`);

  return <SafeAreaView style={styles.container} edges={['top', 'left', 'right']}>
    <View style={styles.header}>
      <Text style={styles.headerTitle}>ACTIVITY</Text>
      {session && <Text style={styles.headerSubtitle} numberOfLines={1}>{session}{eventId ? ` · event ${eventId.slice(0, 12)}` : ''}</Text>}
    </View>
    {loading ? <ActivityIndicator size="large" color="#45EBE9" style={{ marginTop: 40 }} /> : event ? (
      <View style={[styles.eventCard, styles.highlightedItem]}>
        <Text style={styles.highlightLabel}>NOTIFICATION EVENT</Text>
        <Text style={styles.eventKind}>{event.kind}</Text>
        <Text style={styles.eventID} selectable>{event.eventId}</Text>
        <Text style={styles.eventMeta}>{event.sessionId} · generation {event.generation}</Text>
      </View>
    ) : (
      <View style={styles.missingCard}>
        <Text style={styles.missingTitle}>EVENT NO LONGER AVAILABLE</Text>
        <Text style={styles.missingText}>The exact notification event is no longer retained. Other Activity from this session is not substituted.</Text>
        {session && <TouchableOpacity onPress={openTerminal} style={styles.fallbackButton}><Text style={styles.fallbackText}>OPEN TERMINAL</Text></TouchableOpacity>}
      </View>
    )}
    {missingTarget && <Text testID="n1-missing-target" style={styles.srOnly}>missing target event</Text>}
  </SafeAreaView>;
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#000000' },
  header: { paddingHorizontal: 20, paddingVertical: 16, borderBottomWidth: 1, borderBottomColor: '#0D2D45', alignItems: 'center' },
  headerTitle: { fontSize: 20, fontWeight: '800', color: '#ffffff', letterSpacing: 1.2, fontFamily: Platform.OS === 'ios' ? 'HelveticaNeue-CondensedBold' : 'sans-serif-condensed' },
  headerSubtitle: { fontSize: 11, color: '#45EBE9', marginTop: 4, fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace' },
  eventCard: { borderWidth: 1, borderColor: '#0D2D45', borderRadius: 10, padding: 14, margin: 16, backgroundColor: '#07131d' },
  highlightedItem: { borderColor: '#45EBE9', borderWidth: 2 },
  highlightLabel: { color: '#45EBE9', fontSize: 10, fontWeight: '800', marginBottom: 6 },
  eventKind: { color: '#ffffff', fontWeight: '700', fontSize: 14 },
  eventID: { color: '#45EBE9', fontSize: 11, marginTop: 6, fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace' },
  eventMeta: { color: '#8b949e', fontSize: 11, marginTop: 6 },
  missingCard: { margin: 16, padding: 18, borderWidth: 1, borderColor: '#f85149', borderRadius: 10, backgroundColor: '#18090c' },
  missingTitle: { color: '#f85149', fontSize: 14, fontWeight: '800' },
  missingText: { color: '#c9d1d9', marginTop: 8, lineHeight: 20 },
  fallbackButton: { alignSelf: 'flex-start', marginTop: 16, paddingVertical: 9, paddingHorizontal: 12, borderRadius: 6, backgroundColor: '#1E91B3' },
  fallbackText: { color: '#fff', fontWeight: '800', fontSize: 12 },
  srOnly: { color: 'transparent', height: 0 },
});
