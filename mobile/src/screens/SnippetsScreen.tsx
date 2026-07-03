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
          <Text style={styles.backBtnText}>← BACK</Text>
        </TouchableOpacity>
        <Text style={styles.headerTitle}>MY SNIPPETS</Text>
        <TouchableOpacity onPress={() => setIsAddModalVisible(true)} style={styles.addBtn}>
          <Text style={styles.addBtnText}>+ ADD</Text>
        </TouchableOpacity>
      </View>

      <FlatList
        data={snippets}
        keyExtractor={(item) => item.id}
        contentContainerStyle={styles.list}
        ListEmptyComponent={<Text style={styles.emptyText}>NO SNIPPETS SAVED YET.</Text>}
        renderItem={({ item }) => (
          <TouchableOpacity style={styles.card} onPress={() => handleCopy(item.command)} activeOpacity={0.7}>
            <View style={styles.cardHeader}>
              <Text style={styles.cardTitle}>{item.title.toUpperCase()}</Text>
              <TouchableOpacity onPress={() => handleDelete(item.id)} style={styles.deleteBtn}>
                <Text style={styles.deleteBtnText}>DELETE</Text>
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
            <Text style={styles.modalTitle}>ADD NEW SNIPPET</Text>
            
            <Text style={styles.label}>TITLE</Text>
            <TextInput
              style={styles.input}
              placeholder="E.G. START BACKEND AIDER"
              placeholderTextColor="#1E91B3"
              value={newTitle}
              onChangeText={setNewTitle}
            />

            <Text style={styles.label}>COMMAND</Text>
            <TextInput
              style={[styles.input, styles.textArea]}
              placeholder="cd ~/backend && aider\n"
              placeholderTextColor="#1E91B3"
              value={newCommand}
              onChangeText={setNewCommand}
              multiline={true}
              autoCapitalize="none"
              autoCorrect={false}
            />

            <View style={styles.modalActions}>
              <TouchableOpacity style={styles.cancelBtn} onPress={() => setIsAddModalVisible(false)}>
                <Text style={styles.cancelBtnText}>CANCEL</Text>
              </TouchableOpacity>
              <TouchableOpacity style={styles.saveBtn} onPress={handleSave}>
                <Text style={styles.saveBtnText}>SAVE</Text>
              </TouchableOpacity>
            </View>
          </View>
        </View>
      </Modal>
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#000000' },
  header: {
    flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center',
    paddingHorizontal: 16, paddingVertical: 12, borderBottomWidth: 1, borderBottomColor: '#0D2D45',
  },
  backBtn: { padding: 8 },
  backBtnText: { color: '#45EBE9', fontSize: 13, fontWeight: '700', letterSpacing: 1.17 },
  headerTitle: { color: '#ffffff', fontSize: 18, fontWeight: '800', letterSpacing: 1.2 },
  addBtn: { backgroundColor: 'transparent', paddingHorizontal: 12, paddingVertical: 6, borderRadius: 32, borderWidth: 1, borderColor: '#45EBE9' },
  addBtnText: { color: '#ffffff', fontWeight: '700', fontSize: 12, letterSpacing: 0.96 },
  list: { padding: 16 },
  emptyText: { color: '#1E91B3', textAlign: 'center', marginTop: 40, fontWeight: '700', letterSpacing: 0.96 },
  card: { backgroundColor: '#0D2D45', borderRadius: 8, padding: 16, marginBottom: 12, borderWidth: 1, borderColor: '#1E91B3' },
  cardHeader: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 },
  cardTitle: { color: '#ffffff', fontSize: 16, fontWeight: '800', letterSpacing: 1.2 },
  deleteBtn: { padding: 4 },
  deleteBtnText: { color: '#f85149', fontSize: 12, fontWeight: '700', letterSpacing: 0.96 },
  codeBlock: { backgroundColor: '#000000', padding: 12, borderRadius: 4, borderWidth: 1, borderColor: '#1E91B3' },
  codeText: { color: '#45EBE9', fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace', fontSize: 13 },
  modalBg: { flex: 1, backgroundColor: 'rgba(0,0,0,0.8)', justifyContent: 'flex-end' },
  modalContent: { backgroundColor: '#0D2D45', borderTopLeftRadius: 16, borderTopRightRadius: 16, padding: 20 },
  modalTitle: { color: '#ffffff', fontSize: 18, fontWeight: '800', marginBottom: 16, letterSpacing: 1.2 },
  label: { color: '#45EBE9', fontSize: 12, fontWeight: '700', marginBottom: 6, letterSpacing: 0.96 },
  input: { backgroundColor: '#000000', color: '#ffffff', borderRadius: 4, borderWidth: 1, borderColor: '#1E91B3', padding: 12, marginBottom: 16, fontSize: 14 },
  textArea: { minHeight: 80, textAlignVertical: 'top', fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace' },
  modalActions: { flexDirection: 'row', justifyContent: 'flex-end', marginTop: 8 },
  cancelBtn: { paddingHorizontal: 16, paddingVertical: 10, marginRight: 8, borderWidth: 1, borderColor: 'transparent' },
  cancelBtnText: { color: '#1E91B3', fontWeight: '700', letterSpacing: 1.17 },
  saveBtn: { backgroundColor: 'transparent', paddingHorizontal: 20, paddingVertical: 10, borderRadius: 32, borderWidth: 1, borderColor: '#45EBE9' },
  saveBtnText: { color: '#ffffff', fontWeight: '700', letterSpacing: 1.17 }
});
