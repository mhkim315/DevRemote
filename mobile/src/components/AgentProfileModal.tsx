import React, { useState, useEffect } from 'react';
import { View, Text, StyleSheet, TouchableOpacity, Modal, TextInput, ScrollView, Platform, KeyboardAvoidingView, Alert } from 'react-native';
import { FontAwesome5 } from '@expo/vector-icons';
import { RUNNERS, PRESET_COLORS } from '../lib/runners';

interface Props {
  visible: boolean;
  onClose: () => void;
  onSave: (id: string, runner: string, color: string) => void;
  onDelete?: (id: string) => void; // If provided, we are in Edit mode
  initialId?: string;
  initialRunner?: string;
  initialColor?: string;
}

export function AgentProfileModal({ visible, onClose, onSave, onDelete, initialId, initialRunner, initialColor }: Props) {
  const [id, setId] = useState('');
  const [runner, setRunner] = useState('cat');
  const [color, setColor] = useState('#58a6ff');

  useEffect(() => {
    if (visible) {
      setId(initialId || '');
      setRunner(initialRunner || 'cat');
      setColor(initialColor || '#58a6ff');
    }
  }, [visible, initialId, initialRunner, initialColor]);

  const handleSave = () => {
    if (!id.trim()) {
      Alert.alert('Error', 'Please enter an agent name.');
      return;
    }
    onSave(id.trim(), runner, color);
  };

  const isEditMode = !!onDelete;

  return (
    <Modal visible={visible} animationType="slide" transparent={true}>
      <KeyboardAvoidingView behavior={Platform.OS === 'ios' ? 'padding' : undefined} style={styles.modalBg}>
        <View style={styles.modalContent}>
          <View style={styles.header}>
            <Text style={styles.title}>{isEditMode ? 'Edit Agent Profile' : 'New Agent'}</Text>
            <TouchableOpacity onPress={onClose}><Text style={styles.closeBtn}>Close</Text></TouchableOpacity>
          </View>

          <ScrollView style={styles.scroll}>
            <Text style={styles.label}>Agent Name</Text>
            <TextInput
              style={styles.input}
              placeholder="e.g. backend-dev"
              placeholderTextColor="#666"
              value={id}
              onChangeText={setId}
              autoCapitalize="none"
              autoCorrect={false}
              editable={!isEditMode} // Cannot change tmux session name easily after creation
            />

            <Text style={styles.label}>Select Runner</Text>
            <View style={styles.grid}>
              {RUNNERS.map(r => {
                const selected = r.id === runner;
                return (
                  <TouchableOpacity
                    key={r.id}
                    onPress={() => setRunner(r.id)}
                    style={[
                      styles.runnerBtn,
                      selected && { borderColor: color, backgroundColor: color + '20' }
                    ]}
                  >
                    <FontAwesome5 name={r.icon} size={18} color={selected ? color : '#8b949e'} />
                    <Text style={[styles.runnerText, selected && { color: '#fff' }]}>{r.name}</Text>
                  </TouchableOpacity>
                );
              })}
            </View>

            <Text style={styles.label}>Select Color</Text>
            <View style={styles.colorGrid}>
              {PRESET_COLORS.map(c => (
                <TouchableOpacity
                  key={c}
                  onPress={() => setColor(c)}
                  style={[styles.colorBtn, { backgroundColor: c }, color === c && styles.colorSelected]}
                />
              ))}
            </View>
          </ScrollView>

          <View style={styles.footer}>
            <TouchableOpacity style={styles.saveBtn} onPress={handleSave}>
              <Text style={styles.saveBtnText}>Save</Text>
            </TouchableOpacity>
            
            {isEditMode && onDelete && (
              <TouchableOpacity 
                style={styles.deleteBtn} 
                onPress={() => {
                  Alert.alert('Terminate Agent', `Are you sure you want to kill ${id}? This will terminate all running processes in this session.`, [
                    { text: 'Cancel', style: 'cancel' },
                    { text: 'Yes, Terminate', style: 'destructive', onPress: () => onDelete(id) }
                  ]);
                }}
              >
                <Text style={styles.deleteBtnText}>Terminate Agent</Text>
              </TouchableOpacity>
            )}
          </View>
        </View>
      </KeyboardAvoidingView>
    </Modal>
  );
}

const styles = StyleSheet.create({
  modalBg: { flex: 1, backgroundColor: 'rgba(0,0,0,0.8)', justifyContent: 'flex-end' },
  modalContent: { backgroundColor: '#161b22', borderTopLeftRadius: 16, borderTopRightRadius: 16, maxHeight: '90%' },
  header: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', padding: 20, borderBottomWidth: 1, borderBottomColor: '#30363d' },
  title: { color: '#fff', fontSize: 18, fontWeight: '700' },
  closeBtn: { color: '#8b949e', fontSize: 16 },
  scroll: { padding: 20 },
  label: { color: '#c9d1d9', fontSize: 14, fontWeight: '600', marginBottom: 8, marginTop: 12 },
  input: { backgroundColor: '#0d1117', color: '#fff', padding: 12, borderRadius: 8, borderWidth: 1, borderColor: '#30363d', fontSize: 16, marginBottom: 8 },
  grid: { flexDirection: 'row', flexWrap: 'wrap', gap: 8, marginBottom: 8 },
  runnerBtn: { flexDirection: 'row', alignItems: 'center', gap: 8, paddingHorizontal: 12, paddingVertical: 10, borderRadius: 20, borderWidth: 1.5, borderColor: '#30363d', backgroundColor: 'transparent' },
  runnerText: { color: '#8b949e', fontWeight: '500' },
  colorGrid: { flexDirection: 'row', flexWrap: 'wrap', gap: 12, marginBottom: 40 },
  colorBtn: { width: 40, height: 40, borderRadius: 20, borderWidth: 2, borderColor: 'transparent' },
  colorSelected: { borderColor: '#fff' },
  footer: { padding: 20, borderTopWidth: 1, borderTopColor: '#30363d', gap: 12 },
  saveBtn: { backgroundColor: '#238636', padding: 16, borderRadius: 8, alignItems: 'center' },
  saveBtnText: { color: '#fff', fontSize: 16, fontWeight: '600' },
  deleteBtn: { backgroundColor: 'transparent', padding: 16, borderRadius: 8, alignItems: 'center', borderWidth: 1, borderColor: '#f85149' },
  deleteBtnText: { color: '#f85149', fontSize: 16, fontWeight: '600' }
});
