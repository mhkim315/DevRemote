import React, { useState, useEffect } from 'react';
import { View, Text, StyleSheet, SafeAreaView, TouchableOpacity, FlatList, TextInput, Alert, Modal, Platform } from 'react-native';
import * as Clipboard from 'expo-clipboard';
import { getSnippets, saveSnippet, deleteSnippet, Snippet } from '../lib/storage';

interface Props {
  onBack: () => void;
}

export default function SnippetsScreen({ onBack }: Props) {
  const [snippets, setSnippets] = useState<Snippet[]>([]);
  const [isAddModalVisible, setIsAddModalVisible] = useState(false);
  const [newTitle, setNewTitle] = useState('');
  const [newCommand, setNewCommand] = useState('');

  useEffect(() => {
    loadSnippets();
  }, []);

  const loadSnippets = async () => {
    const data = await getSnippets();
    setSnippets(data);
  };

  const handleCopy = async (command: string) => {
    await Clipboard.setStringAsync(command);
    Alert.alert('✅ Copied!', 'Snippet copied to clipboard. You can now paste it in the terminal.');
  };

  const handleDelete = (id: string) => {
    Alert.alert('Delete Snippet', 'Are you sure you want to delete this snippet?', [
      { text: 'Cancel', style: 'cancel' },
      { text: 'Delete', style: 'destructive', onPress: async () => {
        const newData = await deleteSnippet(id);
        setSnippets(newData);
      }}
    ]);
  };

  const handleSave = async () => {
    if (!newTitle.trim() || !newCommand.trim()) {
      Alert.alert('Error', 'Please fill in both title and command fields.');
      return;
    }
    const snippet: Snippet = {
      id: Date.now().toString(),
      title: newTitle.trim(),
      command: newCommand,
    };
    const newData = await saveSnippet(snippet);
    setSnippets(newData);
    setIsAddModalVisible(false);
    setNewTitle('');
    setNewCommand('');
  };

  return (
    <SafeAreaView style={styles.container}>
      <View style={styles.header}>
        <TouchableOpacity onPress={onBack} style={styles.backBtn}>
          <Text style={styles.backBtnText}>← Back</Text>
        </TouchableOpacity>
        <Text style={styles.headerTitle}>My Snippets</Text>
        <TouchableOpacity onPress={() => setIsAddModalVisible(true)} style={styles.addBtn}>
          <Text style={styles.addBtnText}>+ Add</Text>
        </TouchableOpacity>
      </View>

      <FlatList
        data={snippets}
        keyExtractor={(item) => item.id}
        contentContainerStyle={styles.list}
        ListEmptyComponent={<Text style={styles.emptyText}>No snippets saved yet.</Text>}
        renderItem={({ item }) => (
          <TouchableOpacity style={styles.card} onPress={() => handleCopy(item.command)} activeOpacity={0.7}>
            <View style={styles.cardHeader}>
              <Text style={styles.cardTitle}>{item.title}</Text>
              <TouchableOpacity onPress={() => handleDelete(item.id)} style={styles.deleteBtn}>
                <Text style={styles.deleteBtnText}>Delete</Text>
              </TouchableOpacity>
            </View>
            <View style={styles.codeBlock}>
              <Text style={styles.codeText}>{item.command}</Text>
            </View>
          </TouchableOpacity>
        )}
      />

      <Modal visible={isAddModalVisible} animationType="slide" transparent={true}>
        <View style={styles.modalBg}>
          <View style={styles.modalContent}>
            <Text style={styles.modalTitle}>Add New Snippet</Text>
            
            <Text style={styles.label}>Title</Text>
            <TextInput
              style={styles.input}
              placeholder="e.g., Start Backend Aider"
              placeholderTextColor="#666"
              value={newTitle}
              onChangeText={setNewTitle}
            />

            <Text style={styles.label}>Command</Text>
            <TextInput
              style={[styles.input, styles.textArea]}
              placeholder="cd ~/backend && aider\n"
              placeholderTextColor="#666"
              value={newCommand}
              onChangeText={setNewCommand}
              multiline={true}
              autoCapitalize="none"
              autoCorrect={false}
            />

            <View style={styles.modalActions}>
              <TouchableOpacity style={styles.cancelBtn} onPress={() => setIsAddModalVisible(false)}>
                <Text style={styles.cancelBtnText}>Cancel</Text>
              </TouchableOpacity>
              <TouchableOpacity style={styles.saveBtn} onPress={handleSave}>
                <Text style={styles.saveBtnText}>Save</Text>
              </TouchableOpacity>
            </View>
          </View>
        </View>
      </Modal>
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#090a0f' },
  header: {
    flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center',
    paddingHorizontal: 16, paddingVertical: 12, borderBottomWidth: 1, borderBottomColor: '#1e212b',
  },
  backBtn: { padding: 8 },
  backBtnText: { color: '#58a6ff', fontSize: 16, fontWeight: '500' },
  headerTitle: { color: '#fff', fontSize: 18, fontWeight: '700' },
  addBtn: { backgroundColor: '#238636', paddingHorizontal: 12, paddingVertical: 6, borderRadius: 6 },
  addBtnText: { color: '#fff', fontWeight: '600' },
  list: { padding: 16 },
  emptyText: { color: '#8b949e', textAlign: 'center', marginTop: 40 },
  card: { backgroundColor: '#161b22', borderRadius: 12, padding: 16, marginBottom: 12, borderWidth: 1, borderColor: '#30363d' },
  cardHeader: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 },
  cardTitle: { color: '#fff', fontSize: 16, fontWeight: '600' },
  deleteBtn: { padding: 4 },
  deleteBtnText: { color: '#f85149', fontSize: 12, fontWeight: '500' },
  codeBlock: { backgroundColor: '#0d1117', padding: 12, borderRadius: 8, borderWidth: 1, borderColor: '#30363d' },
  codeText: { color: '#e3b341', fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace', fontSize: 13 },
  modalBg: { flex: 1, backgroundColor: 'rgba(0,0,0,0.7)', justifyContent: 'flex-end' },
  modalContent: { backgroundColor: '#161b22', borderTopLeftRadius: 16, borderTopRightRadius: 16, padding: 20 },
  modalTitle: { color: '#fff', fontSize: 18, fontWeight: '700', marginBottom: 16 },
  label: { color: '#c9d1d9', fontSize: 14, fontWeight: '500', marginBottom: 6 },
  input: { backgroundColor: '#0d1117', color: '#fff', borderRadius: 8, borderWidth: 1, borderColor: '#30363d', padding: 12, marginBottom: 16, fontSize: 14 },
  textArea: { minHeight: 80, textAlignVertical: 'top', fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace' },
  modalActions: { flexDirection: 'row', justifyContent: 'flex-end', marginTop: 8 },
  cancelBtn: { paddingHorizontal: 16, paddingVertical: 10, marginRight: 8 },
  cancelBtnText: { color: '#8b949e', fontWeight: '600' },
  saveBtn: { backgroundColor: '#238636', paddingHorizontal: 20, paddingVertical: 10, borderRadius: 6 },
  saveBtnText: { color: '#fff', fontWeight: '600' }
});
