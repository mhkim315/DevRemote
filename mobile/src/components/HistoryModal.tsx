import React, { useEffect, useState } from 'react';
// PA3 Step 1: use getTranscript() instead of deprecated getSessionHistory().
import { getTranscript } from '../lib/client';
import type { TranscriptSegment } from '../lib/client';
import { View, Text, StyleSheet, Modal, TouchableOpacity, ScrollView, ActivityIndicator, Platform } from 'react-native';

interface Props {
  visible: boolean;
  onClose: () => void;
  session: string;
  token?: string;
}

export function HistoryModal({ visible, onClose, session, token }: Props) {
  const [history, setHistory] = useState('');
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (visible) {
      setLoading(true);
      getTranscript(session, token)
        .then(resp => {
          if (resp && resp.semantic) {
            const text = resp.semantic
              .map((s: TranscriptSegment) => s.text || '')
              .filter((t: string) => t.length > 0)
              .join('\n');
            setHistory(text);
            return;
          }
          // Legacy fallback: if null (validation failed), try legacy endpoint.
          setHistory('Transcript unavailable — validation failed.');
        })
        .catch(err => {
          console.error(err);
          setHistory('Error fetching transcript.');
        })
        .finally(() => {
          setLoading(false);
        });
    } else {
      setHistory('');
    }
  }, [visible, session]);

  return (
    <Modal visible={visible} animationType="slide" transparent={true} onRequestClose={onClose}>
      <View style={styles.modalBg}>
        <View style={styles.modalContainer}>
          <View style={styles.modalHeader}>
            <Text style={styles.modalTitle}>History 🕒</Text>
            <TouchableOpacity onPress={onClose}>
              <Text style={styles.closeBtn}>Close</Text>
            </TouchableOpacity>
          </View>
          <View style={styles.modalContent}>
            {loading ? (
              <ActivityIndicator size="large" color="#45EBE9" style={{ marginTop: 40 }} />
            ) : (
              <ScrollView style={styles.modalScroll}>
                <ScrollView horizontal={true} contentContainerStyle={{flexGrow: 1}}>
                  <Text style={styles.modalText} selectable={true}>
                    {history}
                  </Text>
                </ScrollView>
              </ScrollView>
            )}
          </View>
        </View>
      </View>
    </Modal>
  );
}

const styles = StyleSheet.create({
  modalBg: { flex: 1, backgroundColor: 'rgba(0,0,0,0.8)', justifyContent: 'flex-end' },
  modalContainer: { height: '85%', backgroundColor: '#0D2D45', borderTopLeftRadius: 16, borderTopRightRadius: 16, padding: 16 },
  modalHeader: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12 },
  modalTitle: { color: '#ffffff', fontSize: 16, fontWeight: '800', letterSpacing: 1.2 },
  closeBtn: { color: '#45EBE9', fontSize: 13, fontWeight: '700', letterSpacing: 1.17 },
  modalContent: { flex: 1 },
  modalScroll: { flex: 1, backgroundColor: '#000000', borderRadius: 4, padding: 12, borderWidth: 1, borderColor: '#1E91B3' },
  modalText: { color: '#45EBE9', fontSize: 13, fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace' },
});
