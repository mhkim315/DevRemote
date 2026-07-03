import React, { useState, useEffect } from 'react';
import { View, Text, StyleSheet, TouchableOpacity, Modal, TextInput, ScrollView, Platform, KeyboardAvoidingView, Alert } from 'react-native';
import Svg, { Path } from 'react-native-svg';
import { RUNNERS, PRESET_COLORS } from '../lib/runners';

interface Props {
  visible: boolean;
  onClose: () => void;
  onSave: (id: string, runner: string, color: string) => void;
  onDelete?: (id: string) => void;
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
            <Text style={styles.label}>AGENT NAME</Text>
            <TextInput
              style={styles.input}
              placeholder="E.G. BACKEND-DEV"
              placeholderTextColor="#1E91B3"
              value={id}
              onChangeText={setId}
              autoCapitalize="none"
              autoCorrect={false}
              editable={!isEditMode}
            />

            <Text style={styles.label}>SELECT RUNNER</Text>
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
                    <Svg width={24} height={24} viewBox="0 0 388 388">
                      <Path d={r.frames[0]} fill={selected ? color : '#1E91B3'} />
                    </Svg>
                    <Text style={[styles.runnerText, selected && { color: '#ffffff' }]}>{r.name.toUpperCase()}</Text>
                  </TouchableOpacity>
                );
              })}
            </View>

            <Text style={styles.label}>SELECT COLOR</Text>
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
              <Text style={styles.saveBtnText}>SAVE</Text>
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
                <Text style={styles.deleteBtnText}>TERMINATE AGENT</Text>
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
  modalContent: { backgroundColor: '#0D2D45', borderTopLeftRadius: 16, borderTopRightRadius: 16, maxHeight: '90%' },
  header: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', padding: 20, borderBottomWidth: 1, borderBottomColor: '#1E91B3' },
  title: { color: '#ffffff', fontSize: 18, fontWeight: '800', letterSpacing: 1.2 },
  closeBtn: { color: '#45EBE9', fontSize: 13, fontWeight: '700', letterSpacing: 0.96 },
  scroll: { padding: 20 },
  label: { color: '#45EBE9', fontSize: 12, fontWeight: '700', marginBottom: 8, marginTop: 12, letterSpacing: 0.96 },
  input: { backgroundColor: '#000000', color: '#ffffff', padding: 12, borderRadius: 4, borderWidth: 1, borderColor: '#1E91B3', fontSize: 16, marginBottom: 8 },
  grid: { flexDirection: 'row', flexWrap: 'wrap', gap: 8, marginBottom: 8 },
  runnerBtn: { flexDirection: 'row', alignItems: 'center', gap: 8, paddingHorizontal: 12, paddingVertical: 10, borderRadius: 32, borderWidth: 1.5, borderColor: '#1E91B3', backgroundColor: 'transparent' },
  runnerText: { color: '#1E91B3', fontWeight: '700', letterSpacing: 0.96 },
  colorGrid: { flexDirection: 'row', flexWrap: 'wrap', gap: 12, marginBottom: 40 },
  colorBtn: { width: 40, height: 40, borderRadius: 20, borderWidth: 2, borderColor: 'transparent' },
  colorSelected: { borderColor: '#ffffff' },
  footer: { padding: 20, borderTopWidth: 1, borderTopColor: '#1E91B3', gap: 12 },
  saveBtn: { backgroundColor: 'transparent', borderWidth: 1, borderColor: '#45EBE9', padding: 16, borderRadius: 32, alignItems: 'center' },
  saveBtnText: { color: '#ffffff', fontSize: 13, fontWeight: '700', letterSpacing: 1.17 },
  deleteBtn: { backgroundColor: 'transparent', padding: 16, borderRadius: 32, alignItems: 'center', borderWidth: 1, borderColor: '#f85149' },
  deleteBtnText: { color: '#f85149', fontSize: 13, fontWeight: '700', letterSpacing: 1.17 }
});
