import React, { useState, useEffect } from 'react';
import { View, Text, StyleSheet, SafeAreaView, ActivityIndicator, FlatList, TouchableOpacity, Alert } from 'react-native';
import { AgentCard, SessionTelemetry } from '../../components/AgentCard';
import { AgentProfileModal } from '../../components/AgentProfileModal';

interface Props {
  onSelectAgent: (sessionName: string) => void;
  onSnippets: () => void;
  token?: string;
}

export default function DashboardScreen({ onSelectAgent, onSnippets, token }: Props) {
  const [sessions, setSessions] = useState<SessionTelemetry[]>([]);
  const [loading, setLoading] = useState(true);
  
  const [modalVisible, setModalVisible] = useState(false);
  const [editSession, setEditSession] = useState<SessionTelemetry | null>(null);

  const fetchSessions = () => {
    fetch('https://term.fullcount.kr/api/sessions')
      .then(res => res.json())
      .then(data => {
        const normalized = (data || []).map((s: any) =>
          typeof s === 'string' ? { id: s, state: 'idle', load: 0 } : s
        );
        setSessions(normalized);
        setLoading(false);
      })
      .catch(err => {
        console.error(err);
        setLoading(false);
      });
  };

  useEffect(() => {
    fetchSessions(); // initial fetch

    // Poll every 1.5 seconds to get real-time state for animations
    const interval = setInterval(fetchSessions, 1500);
    return () => clearInterval(interval);
  }, []);

  const handleSaveProfile = async (id: string, runner: string, color: string) => {
    const isEdit = !!editSession;
    const method = isEdit ? 'PUT' : 'POST';
    
    try {
      const res = await fetch('https://term.fullcount.kr/api/sessions', {
        method,
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${token}`
        },
        body: JSON.stringify({ id, runner, runnerColor: color })
      });
      if (!res.ok) throw new Error('API Error');
      fetchSessions();
      setModalVisible(false);
      setEditSession(null);
    } catch (e) {
      Alert.alert('Error', 'Failed to save agent profile');
    }
  };

  const handleDeleteProfile = async (id: string) => {
    try {
      const res = await fetch(`https://term.fullcount.kr/api/sessions?id=${encodeURIComponent(id)}`, {
        method: 'DELETE',
        headers: { 'Authorization': `Bearer ${token}` }
      });
      if (!res.ok) throw new Error('API Error');
      fetchSessions();
      setModalVisible(false);
      setEditSession(null);
    } catch (e) {
      Alert.alert('Error', 'Failed to terminate agent');
    }
  };

  const dataWithAdd = [...sessions, { isAddBtn: true, id: 'add-btn' }];

  return (
    <SafeAreaView style={styles.container}>
      <View style={styles.header}>
        <Text style={styles.headerTitle}>POKIT Agents</Text>
        <TouchableOpacity onPress={onSnippets} style={styles.snippetBtn}>
          <Text style={styles.snippetBtnText}>📝 Snippets</Text>
        </TouchableOpacity>
      </View>

      <View style={styles.content}>
        {loading ? (
          <ActivityIndicator size="large" color="#58a6ff" style={{ marginTop: 40 }} />
        ) : (
          <FlatList
            data={dataWithAdd}
            keyExtractor={(item) => item.id}
            numColumns={2}
            contentContainerStyle={styles.gridContainer}
            renderItem={({ item }) => {
              if ((item as any).isAddBtn) {
                return (
                  <View style={{ width: '50%', padding: 6 }}>
                    <TouchableOpacity 
                      style={styles.addCard} 
                      onPress={() => { setEditSession(null); setModalVisible(true); }}
                      activeOpacity={0.7}
                    >
                      <Text style={styles.addCardPlus}>+</Text>
                      <Text style={styles.addCardText}>New Agent</Text>
                    </TouchableOpacity>
                  </View>
                );
              }
              const s = item as SessionTelemetry;
              return (
                <AgentCard
                  session={s}
                  onPress={() => onSelectAgent(s.id)}
                  onSettings={() => { setEditSession(s); setModalVisible(true); }}
                />
              );
            }}
          />
        )}
      </View>

      <AgentProfileModal
        visible={modalVisible}
        onClose={() => { setModalVisible(false); setEditSession(null); }}
        onSave={handleSaveProfile}
        onDelete={editSession ? handleDeleteProfile : undefined}
        initialId={editSession?.id}
        initialRunner={editSession?.runner}
        initialColor={editSession?.runnerColor}
      />
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
  snippetBtn: { backgroundColor: '#21262d', paddingHorizontal: 12, paddingVertical: 6, borderRadius: 12, borderWidth: 1, borderColor: '#30363d' },
  snippetBtnText: { color: '#c9d1d9', fontSize: 13, fontWeight: '600' },
  content: { flex: 1 },
  gridContainer: {
    padding: 12,
  },
  addCard: {
    padding: 16, borderRadius: 16,
    borderWidth: 1.5, borderColor: '#30363d', borderStyle: 'dashed',
    flex: 1, alignItems: 'center', justifyContent: 'center',
    minHeight: 140,
  },
  addCardPlus: { fontSize: 36, color: '#8b949e', fontWeight: '300' },
  addCardText: { fontSize: 14, color: '#8b949e', fontWeight: '600', marginTop: 8 }
});
