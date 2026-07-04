import { config } from '../../config';
import React, { useState, useEffect } from 'react';
import { View, Text, StyleSheet, SafeAreaView, ActivityIndicator, FlatList, TouchableOpacity, Alert, Platform } from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';
import { AgentCard, SessionTelemetry } from '../../components/AgentCard';
import { AgentProfileModal } from '../../components/AgentProfileModal';
import { ApprovalCard } from '../../components/ApprovalCard';

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
    fetch(`${config.BASE_URL}/api/sessions`)
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
    
    // Optimistic Update
    setSessions(prev => {
      if (isEdit) {
        return prev.map(s => s.id === id ? { ...s, runner, runnerColor: color } : s);
      } else {
        return [...prev, { id, state: 'idle', load: 0, runner, runnerColor: color }];
      }
    });
    setModalVisible(false);
    setEditSession(null);

    try {
      const res = await fetch(`${config.BASE_URL}/api/sessions`, {
        method,
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${token}`
        },
        body: JSON.stringify({ id, runner, runnerColor: color })
      });
      if (!res.ok) throw new Error('API Error');
      fetchSessions();
    } catch (e) {
      // Revert optimism on error would be good, but fetchSessions poll will overwrite it anyway
      Alert.alert('Error', 'Failed to save agent profile');
      fetchSessions();
    }
  };

  const handleDeleteProfile = async (id: string) => {
    try {
      const res = await fetch(`${config.BASE_URL}/api/sessions?id=${encodeURIComponent(id)}`, {
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

  const approvalsRequired = sessions.filter(s => 
    s.state === 'waiting' && 
    s.events?.slice().reverse().find(e => e.type === 'approval_request')
  );

  return (
    <SafeAreaView style={styles.container}>
      <View style={styles.header}>
        <Text style={styles.headerTitle}>POKIT AGENTS</Text>
        <View style={{flexDirection:'row', gap:6}}>
          <TouchableOpacity onPress={() => {
            AsyncStorage.removeItem('BASE_URL').then(() => {
              Alert.alert('Reset', 'QR scanner will show on next launch');
            });
          }} style={[styles.snippetBtn, {borderColor: '#f85149'}]}>
            <Text style={[styles.snippetBtnText, {color: '#f85149'}]}>↻ RESCAN</Text>
          </TouchableOpacity>
          <TouchableOpacity onPress={onSnippets} style={styles.snippetBtn}>
            <Text style={styles.snippetBtnText}>SNIPPETS</Text>
          </TouchableOpacity>
        </View>
      </View>

      <View style={styles.content}>
        {approvalsRequired.map(s => {
          const prompt = s.events?.slice().reverse().find(e => e.type === 'approval_request')?.detail || 'Do you want to proceed?';
          return (
            <ApprovalCard 
              key={`approval-${s.id}`}
              sessionId={s.id}
              promptText={prompt}
              token={token}
              onResolved={fetchSessions}
            />
          );
        })}

        {loading ? (
          <ActivityIndicator size="large" color="#45EBE9" style={{ marginTop: 40 }} />
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
                      <Text style={styles.addCardText}>NEW AGENT</Text>
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
  container: { flex: 1, backgroundColor: '#000000' },
  header: {
    flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center',
    paddingHorizontal: 20, paddingVertical: 16, borderBottomWidth: 1, borderBottomColor: '#0D2D45'
  },
  headerTitle: { fontSize: 24, fontWeight: '800', color: '#ffffff', letterSpacing: 1.2, fontFamily: Platform.OS === 'ios' ? 'HelveticaNeue-CondensedBold' : 'sans-serif-condensed' },
  snippetBtn: { backgroundColor: 'transparent', paddingHorizontal: 16, paddingVertical: 8, borderRadius: 32, borderWidth: 1, borderColor: '#45EBE9' },
  snippetBtnText: { color: '#ffffff', fontSize: 12, fontWeight: '700', letterSpacing: 0.96 },
  content: { flex: 1 },
  gridContainer: {
    padding: 12,
  },
  addCard: {
    padding: 16, borderRadius: 16,
    borderWidth: 1.5, borderColor: '#1E91B3', borderStyle: 'dashed',
    flex: 1, alignItems: 'center', justifyContent: 'center',
    minHeight: 140,
    backgroundColor: 'transparent'
  },
  addCardPlus: { fontSize: 36, color: '#1E91B3', fontWeight: '300' },
  addCardText: { fontSize: 12, color: '#1E91B3', fontWeight: '700', marginTop: 8, letterSpacing: 0.96 }
});
