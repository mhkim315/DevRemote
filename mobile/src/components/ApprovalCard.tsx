import React, { useState, useRef } from 'react';
import { resolveApproval, SafeApproval } from '../lib/client';
import { View, Text, TextInput, StyleSheet, TouchableOpacity, ActivityIndicator } from 'react-native';

interface Props {
  sessionId: string;
  approval: SafeApproval;
  onResolved: () => void;
}

// A stable-enough idempotency key per (approval, option) selection: generated once
// and preserved across retries of the SAME decision so a manual retry is idempotent.
// It uses the closed canonical grammar the backend enforces ([A-Za-z0-9._:-]); any
// other character in an id is replaced so the key is never rejected as malformed.
function makeKey(approvalId: string, optionId: string, nonce: number): string {
  const safe = (s: string) => s.replace(/[^A-Za-z0-9._:-]/g, '_').slice(0, 40);
  return `${safe(approvalId)}.${safe(optionId)}.${nonce}`;
}

export function ApprovalCard({ sessionId, approval, onResolved }: Props) {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [inputValues, setInputValues] = useState<Record<string, string>>({});
  // Per-(option) idempotency keys, preserved across retries.
  const keys = useRef<Record<string, string>>({});
  const nonceRef = useRef<number>(1);

  const setOptionInput = (optId: string, value: string) => {
    setInputValues(prev => ({ ...prev, [optId]: value }));
  };

  const keyFor = (optionId: string): string => {
    if (!keys.current[optionId]) {
      keys.current[optionId] = makeKey(approval.id, optionId, nonceRef.current++);
    }
    return keys.current[optionId];
  };

  const handleAction = async (optionId: string, input?: string) => {
    if (loading) return; // one exact request in flight
    setLoading(true);
    setError(null);
    try {
      await resolveApproval(sessionId, approval.id, optionId, input, keyFor(optionId));
      // Local optimistic state only updates from an accepted server result.
      onResolved();
    } catch (e: any) {
      // Honest per-outcome messaging; input and the idempotency key are preserved so
      // a safe manual retry re-uses the same key (idempotent).
      const status = e?.statusCode;
      if (status === 401 || status === 403) {
        setError('Device not authorized — pair this device');
      } else if (status === 409) {
        setError('No longer current — refresh');
      } else if (status === 410) {
        setError('Approval expired');
      } else if (status === 502) {
        setError("Couldn't deliver — resolve in the terminal");
      } else if (status === 400) {
        setError('Action not accepted');
      } else {
        setError('Network error — tap an action to retry');
      }
    } finally {
      setLoading(false);
    }
  };

  // Non-actionable (unproven mapping / intervention info): display only, no buttons.
  if (!approval.actionable) {
    return (
      <View style={styles.card}>
        <View style={styles.header}>
          <Text style={styles.title}>ATTENTION</Text>
        </View>
        <Text style={styles.prompt}>{approval.summary}</Text>
        <Text style={styles.unavailableText}>Respond in the live terminal — no remote action is available.</Text>
      </View>
    );
  }

  const options = approval.options || [];

  return (
    <View style={styles.card}>
      <View style={styles.header}>
        <Text style={styles.title}>APPROVAL REQUIRED</Text>
      </View>
      <Text style={styles.prompt}>{approval.summary}</Text>

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
            const needsInput = opt.requiresInput && !optInput.trim();
            return (
              <View key={opt.id} style={styles.optionRow}>
                {opt.requiresInput && (
                  <TextInput
                    style={styles.inputField}
                    placeholder={opt.inputPlaceholder || 'Enter text...'}
                    placeholderTextColor="#8b949e"
                    value={optInput}
                    onChangeText={v => setOptionInput(opt.id, v)}
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
});
