import { config } from '../config';
import React, { useState } from 'react';
import { View, Text, StyleSheet, TouchableOpacity, ActivityIndicator } from 'react-native';

interface Props {
  sessionId: string;
  promptText: string;
  token?: string;
  onResolved: () => void;
}

export function ApprovalCard({ sessionId, promptText, token, onResolved }: Props) {
  const [loading, setLoading] = useState(false);

  const handleAction = async (approve: boolean) => {
    setLoading(true);
    try {
      const res = await fetch(`${config.BASE_URL}/debug/cmd?session=${encodeURIComponent(sessionId)}`, {
        method: 'POST',
        headers: {
          'Content-Type': 'text/plain',
          ...(token ? { 'Authorization': `Bearer ${token}` } : {})
        },
        body: approve ? "y\n" : "n\n"
      });
      if (!res.ok) {
        throw new Error('Failed to send command');
      }
      onResolved();
    } catch (e) {
      console.error(e);
    } finally {
      setLoading(false);
    }
  };

  return (
    <View style={styles.card}>
      <Text style={styles.title}>APPROVAL REQUIRED</Text>
      <Text style={styles.sessionName}>Agent: {sessionId}</Text>
      <Text style={styles.prompt}>{promptText}</Text>
      
      {loading ? (
        <ActivityIndicator color="#45EBE9" style={{ marginVertical: 16 }} />
      ) : (
        <View style={styles.buttonRow}>
          <TouchableOpacity style={[styles.button, styles.rejectBtn]} onPress={() => handleAction(false)}>
            <Text style={styles.rejectText}>REJECT</Text>
          </TouchableOpacity>
          <TouchableOpacity style={[styles.button, styles.approveBtn]} onPress={() => handleAction(true)}>
            <Text style={styles.approveText}>APPROVE</Text>
          </TouchableOpacity>
        </View>
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  card: {
    backgroundColor: '#1E1E1E',
    borderRadius: 16,
    padding: 16,
    marginHorizontal: 12,
    marginBottom: 16,
    borderWidth: 1,
    borderColor: '#f85149'
  },
  title: {
    color: '#f85149',
    fontSize: 14,
    fontWeight: '800',
    marginBottom: 4,
    letterSpacing: 1
  },
  sessionName: {
    color: '#ffffff',
    fontSize: 12,
    marginBottom: 12,
    opacity: 0.8
  },
  prompt: {
    color: '#ffffff',
    fontSize: 14,
    marginBottom: 16,
    lineHeight: 20
  },
  buttonRow: {
    flexDirection: 'row',
    justifyContent: 'flex-end',
    gap: 12
  },
  button: {
    paddingVertical: 8,
    paddingHorizontal: 16,
    borderRadius: 8,
    borderWidth: 1
  },
  rejectBtn: {
    borderColor: '#8b949e',
    backgroundColor: 'transparent'
  },
  rejectText: {
    color: '#8b949e',
    fontWeight: '700'
  },
  approveBtn: {
    borderColor: '#39d353',
    backgroundColor: '#39d353'
  },
  approveText: {
    color: '#000000',
    fontWeight: '700'
  }
});
