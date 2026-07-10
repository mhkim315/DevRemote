import React, { useState, useEffect } from 'react';
import {
  View, Text, StyleSheet, TouchableOpacity, Modal, TextInput, ScrollView,
  Platform, KeyboardAvoidingView, ActivityIndicator,
} from 'react-native';
import { listSessionProfiles, createSession, SessionProfile, SessionLifecycle, PokitError } from '../lib/client';
import { canCreateProfile, validateName, validateCwd, isRunnable } from '../lib/lifecycle';

interface Props {
  visible: boolean;
  onClose: () => void;
  onCreated: (canonicalId: string) => void;
  token?: string;
}

export function NewSessionModal({ visible, onClose, onCreated, token }: Props) {
  const [profiles, setProfiles] = useState<SessionProfile[]>([]);
  const [loadingProfiles, setLoadingProfiles] = useState(false);
  const [profileError, setProfileError] = useState('');

  const [selectedProfileId, setSelectedProfileId] = useState('');
  const [name, setName] = useState('');
  const [cwd, setCwd] = useState('');
  const [nameError, setNameError] = useState('');
  const [cwdError, setCwdError] = useState('');

  const [submitting, setSubmitting] = useState(false);
  const [createError, setCreateError] = useState('');

  // Reset on open.
  useEffect(() => {
    if (visible) {
      setSubmitting(false);
      setCreateError('');
      setName('');
      setCwd('');
      setNameError('');
      setCwdError('');
      setProfileError('');
      loadProfiles();
    }
  }, [visible]);

  const loadProfiles = async () => {
    setLoadingProfiles(true);
    setProfileError('');
    try {
      const list = await listSessionProfiles(token);
      setProfiles(list);
      if (list.length > 0) {
        const firstAvailable = list.find(canCreateProfile);
        setSelectedProfileId(firstAvailable ? firstAvailable.id : '');
      }
    } catch (e: any) {
      setProfileError(e instanceof PokitError ? e.message : 'Could not load launch profiles.');
    } finally {
      setLoadingProfiles(false);
    }
  };

  const handleCreate = async () => {
    const nerr = validateName(name);
    const cerr = validateCwd(cwd);
    setNameError(nerr || '');
    setCwdError(cerr || '');
    if (nerr || cerr) return;

    setSubmitting(true);
    setCreateError('');
    try {
      const created = await createSession(
        { profileId: selectedProfileId, name: name.trim() || undefined, cwd: cwd.trim() || undefined },
        token,
      );
      // The server must report a Recorder-ready running controlled_pty session
      // with a canonical id before we navigate. Any other 2xx shape is an API
      // contract error and the form stays open.
      if (!isRunnable(created)) {
        setCreateError('Server returned an unexpected response. The session may not be ready yet. Try again.');
      } else {
        onCreated(created.id);
      }
    } catch (e: any) {
      setCreateError(e instanceof PokitError ? e.message : 'Failed to create session.');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Modal visible={visible} animationType="slide" transparent={true}>
      <KeyboardAvoidingView behavior={Platform.OS === 'ios' ? 'padding' : undefined} style={styles.modalBg}>
        <View style={styles.modalContent}>
          <View style={styles.header}>
            <Text style={styles.title}>NEW SESSION</Text>
            <TouchableOpacity onPress={onClose} disabled={submitting}>
              <Text style={styles.closeBtn}>Close</Text>
            </TouchableOpacity>
          </View>

          <ScrollView style={styles.scroll}>
            {/* Profiles */}
            <Text style={styles.label}>LAUNCH PROFILE</Text>
            {loadingProfiles ? (
              <ActivityIndicator color="#45EBE9" style={{ marginBottom: 12 }} />
            ) : profileError ? (
              <Text style={styles.errorText}>{profileError}</Text>
            ) : (
              <View style={styles.profileGrid}>
                {profiles.map(p => {
                  const avail = canCreateProfile(p);
                  return (
                    <TouchableOpacity
                      key={p.id}
                      disabled={!avail || submitting}
                      onPress={() => setSelectedProfileId(p.id)}
                      style={[
                        styles.profileBtn,
                        selectedProfileId === p.id && styles.profileBtnSelected,
                        !avail && styles.profileBtnDisabled,
                      ]}
                    >
                      <Text
                        style={[
                          styles.profileBtnText,
                          selectedProfileId === p.id && styles.profileBtnTextSelected,
                          !avail && styles.profileBtnTextDisabled,
                        ]}
                      >
                        {p.label}{!avail ? ' (not installed)' : ''}
                      </Text>
                    </TouchableOpacity>
                  );
                })}
              </View>
            )}

            {/* Name */}
            <Text style={styles.label}>DISPLAY NAME (optional)</Text>
            <TextInput
              style={[styles.input, nameError ? styles.inputError : undefined]}
              placeholder="e.g. backend-dev"
              placeholderTextColor="#1E91B3"
              value={name}
              onChangeText={t => { setName(t); setNameError(''); }}
              autoCapitalize="none"
              autoCorrect={false}
              editable={!submitting}
            />
            {nameError ? <Text style={styles.fieldError}>{nameError}</Text> : null}

            {/* CWD */}
            <Text style={styles.label}>WORKING DIRECTORY (optional)</Text>
            <TextInput
              style={[styles.input, cwdError ? styles.inputError : undefined]}
              placeholder="/Users/me/my-project"
              placeholderTextColor="#1E91B3"
              value={cwd}
              onChangeText={t => { setCwd(t); setCwdError(''); }}
              autoCapitalize="none"
              autoCorrect={false}
              editable={!submitting}
            />
            {cwdError ? <Text style={styles.fieldError}>{cwdError}</Text> : null}

            {/* Create error */}
            {createError ? <Text style={styles.errorText}>{createError}</Text> : null}
          </ScrollView>

          <View style={styles.footer}>
            <TouchableOpacity
              style={[styles.createBtn, submitting && styles.createBtnDisabled]}
              onPress={handleCreate}
              disabled={submitting || !selectedProfileId}
            >
              <Text style={styles.createBtnText}>
                {submitting ? 'Starting…' : 'CREATE'}
              </Text>
            </TouchableOpacity>
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
  input: { backgroundColor: '#000000', color: '#ffffff', padding: 12, borderRadius: 4, borderWidth: 1, borderColor: '#1E91B3', fontSize: 16, marginBottom: 4 },
  inputError: { borderColor: '#f85149' },
  fieldError: { color: '#f85149', fontSize: 12, marginBottom: 8 },
  errorText: { color: '#f85149', fontSize: 13, marginBottom: 8, lineHeight: 18 },
  profileGrid: { flexDirection: 'row', flexWrap: 'wrap', gap: 8, marginBottom: 8 },
  profileBtn: { paddingHorizontal: 16, paddingVertical: 12, borderRadius: 32, borderWidth: 1.5, borderColor: '#1E91B3', backgroundColor: 'transparent' },
  profileBtnSelected: { borderColor: '#39d353', backgroundColor: 'rgba(57,211,83,0.15)' },
  profileBtnDisabled: { borderColor: '#444', opacity: 0.5 },
  profileBtnText: { color: '#1E91B3', fontWeight: '700', letterSpacing: 0.96 },
  profileBtnTextSelected: { color: '#39d353' },
  profileBtnTextDisabled: { color: '#888' },
  footer: { padding: 20, borderTopWidth: 1, borderTopColor: '#1E91B3' },
  createBtn: { backgroundColor: '#39d353', padding: 16, borderRadius: 32, alignItems: 'center' },
  createBtnDisabled: { backgroundColor: '#1e5a2a' },
  createBtnText: { color: '#000000', fontSize: 13, fontWeight: '700', letterSpacing: 1.17 },
});
