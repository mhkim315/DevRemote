import React from 'react';
import { View, Text, StyleSheet, TouchableOpacity, ScrollView, SafeAreaView } from 'react-native';

interface Props {
  onSelectAgent: () => void;
}

export default function DashboardScreen({ onSelectAgent }: Props) {
  return (
    <SafeAreaView style={styles.container}>
      <View style={styles.header}>
        <Text style={styles.headerTitle}>DevRemote</Text>
        <View style={styles.badge}><Text style={styles.badgeText}>Local Daemon</Text></View>
      </View>

      <ScrollView contentContainerStyle={styles.content}>
        <Text style={styles.sectionTitle}>Agent Fleet</Text>
        
        <TouchableOpacity style={styles.card} onPress={onSelectAgent} activeOpacity={0.8}>
          <View style={styles.cardHeader}>
            <View style={styles.dot} />
            <Text style={styles.cardTitle}>Claude-3.5-Sonnet</Text>
          </View>
          <Text style={styles.cardSubtitle}>Terminal Observer</Text>
          <View style={styles.statusRow}>
            <Text style={styles.statusText}>Awaiting input</Text>
            <Text style={styles.timeText}>Just now</Text>
          </View>
        </TouchableOpacity>

        <TouchableOpacity style={[styles.card, styles.cardInactive]} activeOpacity={0.8}>
          <View style={styles.cardHeader}>
            <View style={[styles.dot, styles.dotInactive]} />
            <Text style={styles.cardTitle}>GPT-4o</Text>
          </View>
          <Text style={styles.cardSubtitle}>Background Worker</Text>
          <View style={styles.statusRow}>
            <Text style={styles.statusText}>Sleeping</Text>
            <Text style={styles.timeText}>2h ago</Text>
          </View>
        </TouchableOpacity>
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
