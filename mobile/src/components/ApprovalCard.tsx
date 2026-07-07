import React, { useState } from 'react';
import { resolveApproval, AgentApproval } from '../lib/client';
import { View, Text, StyleSheet, TouchableOpacity, ActivityIndicator } from 'react-native';

interface Props {
  sessionId: string;
  approval: AgentApproval;
  token?: string;
  onResolved: () => void;
}

export function ApprovalCard({ sessionId, approval, token, onResolved }: Props) {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleAction = async (action: string) => {
    setLoading(true);
    setError(null);
    try {
      const res = await resolveApproval(sessionId, approval.id, action, token);
      if (!res.ok) {
        if (res.status === 409) {
          setError('Already resolved');
        } else if (res.status === 410) {
          setError('Approval expired');
        } else {
          setError(`Failed (${res.status})`);
        }
      } else {
        onResolved();
      }
    } catch (e) {
      setError('Network error — tap to retry');
    } finally {
      setLoading(false);
    }
  };

  // Capability-aware: use server-provided options only.
  // Never synthesize approve/reject client-side.
  const options = approval.options || [];

  const createdAt = approval.createdAt
    ? new Date(approval.createdAt).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
    : '';

  return (
    <View style={styles.card}>
      <View style={styles.header}>
        <Text style={styles.title}>APPROVAL REQUIRED</Text>
        {createdAt ? <Text style={styles.time}>{createdAt}</Text> : null}
      </View>
      <Text style={styles.agentName}>Agent: {approval.agentKind || 'unknown'}</Text>
      <Text style={styles.prompt}>{approval.prompt}</Text>

      {error ? (
        <View style={styles.errorRow}>
          <Text style={styles.errorText}>{error}</Text>
          <TouchableOpacity onPress={() => setError(null)}>
            <Text style={styles.dismissText}>Dismiss</Text>
          </TouchableOpacity>
        </View>
      ) : null}

      {loading ? (
        <ActivityIndicator color="#45EBE9" style={{ marginVertical: 16 }} />
      ) : options.length === 0 ? (
        <Text style={styles.unavailableText}>No remote actions available</Text>
      ) : (
        <View style={styles.buttonRow}>
          {options.map(opt => {
            const isPrimary = opt.id === 'approve';
            const isDanger = opt.id === 'reject';
            return (
              <TouchableOpacity
                key={opt.id}
                style={[
                  styles.button,
                  isPrimary && styles.approveBtn,
                  isDanger && styles.rejectBtn,
                ]}
                onPress={() => handleAction(opt.id)}
                disabled={loading}
              >
                <Text
                  style={[
                    styles.buttonText,
                    isPrimary && styles.approveText,
                    isDanger && styles.rejectText,
                  ]}
                >
                  {opt.label}
                </Text>
              </TouchableOpacity>
            );
          })}
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
    borderColor: '#f85149',
  },
  header: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: 4,
  },
  title: {
    color: '#f85149',
    fontSize: 14,
    fontWeight: '800',
    letterSpacing: 1,
  },
  time: {
    color: '#8b949e',
    fontSize: 11,
  },
  agentName: {
    color: '#ffffff',
    fontSize: 12,
    marginBottom: 12,
    opacity: 0.8,
  },
  prompt: {
    color: '#ffffff',
    fontSize: 14,
    marginBottom: 16,
    lineHeight: 20,
  },
  unavailableText: {
    color: '#8b949e',
    fontSize: 13,
    fontStyle: 'italic',
    marginBottom: 12,
  },
  errorRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    backgroundColor: '#3D1C1C',
    padding: 10,
    borderRadius: 8,
    marginBottom: 12,
  },
  errorText: {
    color: '#f85149',
    fontSize: 12,
    flex: 1,
  },
  dismissText: {
    color: '#8b949e',
    fontSize: 12,
    marginLeft: 12,
  },
  buttonRow: {
    flexDirection: 'row',
    justifyContent: 'flex-end',
    gap: 12,
  },
  button: {
    paddingVertical: 8,
    paddingHorizontal: 16,
    borderRadius: 8,
    borderWidth: 1,
    borderColor: '#8b949e',
    backgroundColor: 'transparent',
  },
  buttonText: {
    color: '#8b949e',
    fontWeight: '700',
    fontSize: 13,
  },
  approveBtn: {
    borderColor: '#39d353',
    backgroundColor: '#39d353',
  },
  approveText: {
    color: '#000000',
  },
  rejectBtn: {
    borderColor: '#f85149',
    backgroundColor: 'transparent',
  },
  rejectText: {
    color: '#f85149',
  },
});
