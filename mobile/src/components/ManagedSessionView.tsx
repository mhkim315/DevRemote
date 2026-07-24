// SP0.5-B — minimal native managed session surface. The managed Codex session
// is JSON-RPC-native (no PTY): this view consumes ONLY the bounded projected
// event DTO (strictly decoded fail-closed) and submits ONE bounded prompt at a
// time. It is deliberately NOT a terminal: no ANSI rendering, no keymaps, no
// approval buttons — the session is clearly labeled as native managed.
import React, { useEffect, useRef, useState } from 'react';
import {
  View, Text, TextInput, TouchableOpacity, ScrollView, StyleSheet, SafeAreaView,
} from 'react-native';
import { getManagedStatus, getManagedEvents, postManagedPrompt } from '../lib/client';
import {
  ManagedSessionController, ManagedPollerState, ManagedEvent,
} from '../lib/managedSession';

interface Props {
  session: string; // canonical codex_app_server:<local> or claude_headless:<local> id
  token?: string;
  onBack: () => void;
}

function adapterLabel(session: string): string {
  if (session.startsWith('claude_headless:')) return 'Claude';
  if (session.startsWith('codex_app_server:')) return 'Codex';
  return 'Managed';
}

const POLL_MS = 1500;

export default function ManagedSessionView({ session, token, onBack }: Props) {
  const [events, setEvents] = useState<ManagedEvent[]>([]);
  const [status, setStatus] = useState('…');
  const [statusCurrent, setStatusCurrent] = useState(false);
  const [error, setError] = useState('');
  const [prompt, setPrompt] = useState('');
  const [sending, setSending] = useState(false);
  const ctrlRef = useRef<ManagedSessionController | null>(null);
  const scrollRef = useRef<ScrollView | null>(null);

  useEffect(() => {
    // One controller per mounted session — the generation. close() on
    // unmount/session-switch ABORTS the real in-flight bootstrap/poll
    // requests and guarantees a late bootstrap response installs no poller,
    // no timer, and commits no state (SP0.5-R2).
    const ctrl = new ManagedSessionController(
      session,
      {
        fetchStatus: (signal) => getManagedStatus(session, token, signal),
        fetchEvents: (epoch, cursor, signal) => getManagedEvents(session, epoch, cursor, token, signal),
        onUpdate: (state: ManagedPollerState) => {
          setEvents(state.events);
          setStatus(state.status);
          setStatusCurrent(state.current);
        },
        onError: (message) => setError(message),
      },
      { pollMs: POLL_MS },
    );
    ctrlRef.current = ctrl;
    void ctrl.start();
    return () => {
      ctrl.close();
      if (ctrlRef.current === ctrl) ctrlRef.current = null;
    };
  }, [session, token]);

  const send = async () => {
    const text = prompt.trim();
    const epoch = ctrlRef.current?.getEpoch() ?? 0;
    if (!text || sending || epoch < 1) return;
    setSending(true);
    setError('');
    try {
      await postManagedPrompt(session, epoch, text, token);
      setPrompt('');
    } catch (e: any) {
      if (e && e.statusCode === 409) {
        setError('A turn is already running — wait for it to finish.');
      } else {
        setError('Prompt rejected.');
      }
    } finally {
      setSending(false);
    }
  };

  return (
    <SafeAreaView style={styles.root}>
      <View style={styles.header}>
        <TouchableOpacity onPress={onBack}><Text style={styles.back}>‹ Back</Text></TouchableOpacity>
        <Text style={styles.title}>Native Managed Session · {adapterLabel(session)}</Text>
        <Text style={statusCurrent ? styles.status : styles.statusStale}>
          {statusCurrent ? status : `${status === 'unavailable' ? 'unavailable' : status} (stale)`}
        </Text>
      </View>
      <Text style={styles.note}>JSON-RPC native session — bounded output, no terminal.</Text>
      <ScrollView
        style={styles.feed}
        ref={scrollRef}
        onContentSizeChange={() => scrollRef.current?.scrollToEnd({ animated: false })}
      >
        {events.map((ev) => (
          <Text key={String(ev.seq)} style={kindStyle(ev.kind)}>
            {renderEvent(ev)}
          </Text>
        ))}
      </ScrollView>
      {error !== '' && <Text style={styles.error}>{error}</Text>}
      <View style={styles.inputRow}>
        <TextInput
          style={styles.input}
          value={prompt}
          onChangeText={setPrompt}
          placeholder="Prompt (one turn at a time)"
          placeholderTextColor="#666"
          editable={!sending}
          onSubmitEditing={send}
        />
        <TouchableOpacity style={styles.send} onPress={send} disabled={sending}>
          <Text style={styles.sendText}>{sending ? '…' : 'Send'}</Text>
        </TouchableOpacity>
      </View>
    </SafeAreaView>
  );
}

function renderEvent(ev: ManagedEvent): string {
  switch (ev.kind) {
    case 'assistant': return ev.text || '';
    case 'working': return '… working';
    case 'completed': return '· idle';
    case 'exited': return '× session exited';
    case 'gap': return '~ earlier output dropped (bounded history)';
    default: return '';
  }
}

function kindStyle(kind: string) {
  return kind === 'assistant' ? styles.assistant : styles.marker;
}

const styles = StyleSheet.create({
  root: { flex: 1, backgroundColor: '#0d1117' },
  header: { flexDirection: 'row', alignItems: 'center', padding: 12, gap: 10 },
  back: { color: '#58a6ff', fontSize: 16, marginRight: 8 },
  title: { color: '#e6edf3', fontWeight: '600', flex: 1 },
  status: { color: '#8b949e', fontSize: 12 },
  statusStale: { color: '#f0883e', fontSize: 12 },
  note: { color: '#8b949e', fontSize: 11, paddingHorizontal: 12, paddingBottom: 6 },
  feed: { flex: 1, paddingHorizontal: 12 },
  assistant: { color: '#e6edf3', fontSize: 14, marginBottom: 6 },
  marker: { color: '#8b949e', fontSize: 12, marginBottom: 4 },
  error: { color: '#f85149', paddingHorizontal: 12, paddingBottom: 4, fontSize: 12 },
  inputRow: { flexDirection: 'row', padding: 10, gap: 8 },
  input: {
    flex: 1, backgroundColor: '#161b22', color: '#e6edf3', borderRadius: 8,
    paddingHorizontal: 10, paddingVertical: 8, borderWidth: 1, borderColor: '#30363d',
  },
  send: { backgroundColor: '#238636', borderRadius: 8, paddingHorizontal: 14, justifyContent: 'center' },
  sendText: { color: '#fff', fontWeight: '600' },
});
