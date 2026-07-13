import React, { useState } from 'react';
import { resolveApproval, AgentApproval, InteractionOption } from '../lib/client';
import { View, Text, TextInput, StyleSheet, TouchableOpacity, ActivityIndicator } from 'react-native';

interface Props {
  sessionId: string;
  approval: AgentApproval;
  token?: string;
  onResolved: () => void;
}

export function ApprovalCard({ sessionId, approval, token, onResolved }: Props) {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // Per-option input values keyed by option ID.
  const [inputValues, setInputValues] = useState<Record<string, string>>({});
  const setOptionInput = (optId: string, value: string) => {
    setInputValues(prev => ({ ...prev, [optId]: value }));
  };

  const handleAction = async (action: string, input?: string) => {
    if (loading) return; // one exact request in flight at a time
    setLoading(true);
    setError(null);
    try {
      await resolveApproval(sessionId, approval.id, action, input, token);
      onResolved();
    } catch (e: any) {
      // Honest per-outcome messaging from the daemon's status. Input is preserved
      // (inputValues is untouched) so a recoverable failure does not lose typing.
      const status = e?.statusCode;
      if (status === 409) {
        setError('No longer current — refresh');
      } else if (status === 410) {
        setError('Approval expired');
      } else if (status === 502) {
        setError("Couldn't deliver — resolve in the terminal");
      } else if (status === 400) {
        setError('Action not accepted');
      } else if (status === 401 || status === 403) {
        setError('Not authorized on this device');
      } else {
        setError('Network error — tap to retry');
      }
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
        <View>
          {options.map(opt => {
            const kindStyle = getKindStyle(opt.kind);
            const optInput = inputValues[opt.id] || '';
            const needsInput = opt.input?.required && !optInput.trim();
            return (
              <View key={opt.id} style={styles.optionRow}>
                {opt.input && (
                  <TextInput
                    style={[styles.inputField, opt.input.multiline && styles.inputMultiline]}
                    placeholder={opt.input.placeholder || 'Enter text...'}
                    placeholderTextColor="#8b949e"
                    value={optInput}
                    onChangeText={v => setOptionInput(opt.id, v)}
                    multiline={opt.input.multiline}
                    editable={!loading}
                  />
                )}
                <TouchableOpacity
                  style={[styles.button, kindStyle.btn, needsInput && styles.buttonDisabled]}
                  onPress={() => handleAction(opt.id, optInput.trim() || undefined)}
                  disabled={loading || needsInput}
                >
                  <Text style={[styles.buttonText, kindStyle.text, needsInput && styles.textDisabled]}>
                    {opt.label}
                  </Text>
                </TouchableOpacity>
              </View>
            );
          })}
        </View>
      )}
    </View>
  );
}

// getKindStyle returns visual style based on server-provided semantic kind.
// Never infers meaning from option ID.
function getKindStyle(kind: string): { btn: any; text: any } {
  switch (kind) {
    case 'approve':
      return { btn: { borderColor: '#39d353', backgroundColor: '#39d353' }, text: { color: '#000000' } };
    case 'reject':
      return { btn: { borderColor: '#f85149', backgroundColor: 'transparent' }, text: { color: '#f85149' } };
    case 'cancel':
      return { btn: { borderColor: '#8b949e', backgroundColor: 'transparent' }, text: { color: '#8b949e' } };
    case 'open':
      return { btn: { borderColor: '#1E91B3', backgroundColor: 'transparent' }, text: { color: '#1E91B3' } };
    case 'neutral':
    default:
      return { btn: { borderColor: '#8b949e', backgroundColor: 'transparent' }, text: { color: '#8b949e' } };
  }
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
  optionRow: {
    marginBottom: 8,
  },
  inputField: {
    backgroundColor: '#2C2C2E',
    color: '#ffffff',
    borderRadius: 8,
    padding: 10,
    fontSize: 14,
    marginBottom: 6,
    borderWidth: 1,
    borderColor: '#0D2D45',
  },
  inputMultiline: {
    minHeight: 60,
    textAlignVertical: 'top',
  },
  buttonDisabled: {
    opacity: 0.4,
  },
  textDisabled: {
    opacity: 0.5,
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
