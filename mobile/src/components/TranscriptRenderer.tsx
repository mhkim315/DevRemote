import React, { useMemo } from 'react';
import { View, Text, FlatList, StyleSheet } from 'react-native';
import { classifyEvents, type OutputSpan } from '../lib/transcriptClassify';

// ── E8g2Transcript: rendered component ──

// PA3 Step 1: adjacent-only AgentEventRef dedup within a single batch.
export function E8g2Transcript({ events }: { events: any[] }) {
  const spans: OutputSpan[] = useMemo(() => classifyEvents(events), [events]);

  if (spans.length === 0) {
    return <Text style={styles.emptyActivityText}>No transcript yet.</Text>;
  }

  return (
    <FlatList
      data={spans}
      keyExtractor={(item: OutputSpan) => item.key}
      contentContainerStyle={styles.transcriptList}
      inverted
      renderItem={({ item }: { item: OutputSpan }) => {
        if (item.isInput) {
          return <View style={styles.transcriptInputDivider} />;
        }
        if (item.isDegraded) {
          return (
            <View style={styles.transcriptDegradedBlock}>
              <Text style={styles.transcriptDegradedText}>{item.text}</Text>
            </View>
          );
        }
        if (item.isAgentEvent && item.agentLabel) {
          return (
            <View style={styles.transcriptOutputBlock}>
              <Text style={styles.transcriptAgentLabel}>{item.agentLabel}</Text>
              {item.text ? (
                <Text style={styles.transcriptOutputText} selectable={true}>
                  {item.text.replace(/\r/g, '')}
                </Text>
              ) : null}
            </View>
          );
        }
        return (
          <View style={styles.transcriptOutputBlock}>
            <Text style={styles.transcriptOutputText} selectable={true}>
              {item.text.replace(/\r/g, '')}
            </Text>
          </View>
        );
      }}
    />
  );
}

const styles = StyleSheet.create({
  emptyActivityText: { color: '#888', fontSize: 14, padding: 16 },
  transcriptList: { paddingHorizontal: 12, paddingVertical: 4 },
  transcriptInputDivider: { borderBottomWidth: 1, borderBottomColor: '#444', marginVertical: 4 },
  transcriptDegradedBlock: { padding: 8, backgroundColor: '#3a1a1a', borderRadius: 4, marginVertical: 2 },
  transcriptDegradedText: { color: '#f88', fontSize: 12 },
  transcriptOutputBlock: { marginVertical: 2 },
  transcriptAgentLabel: { color: '#888', fontSize: 10, marginBottom: 2 },
  transcriptOutputText: { color: '#ccc', fontSize: 12, fontFamily: 'monospace' },
});
