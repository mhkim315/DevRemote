import React from 'react';
const { spawn } = require('child_process') as { spawn: any };
const fs = require('fs') as typeof import('fs');
const os = require('os') as typeof import('os');
const path = require('path') as typeof import('path');
const vm = require('vm') as typeof import('vm');
const NodeWebSocket = require('ws') as any;
const { webcrypto } = require('crypto') as { webcrypto: Crypto };
// React 19 requires this opt-in for state updates flushed by act().
(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
// The integration test below gives FeedScreen's production injection path a
// real page VM. Existing component tests keep this unset and retain a no-op.
let mockWebViewInjection: ((source: string) => void) | null = null;
// react-dom/server has no @types package in the mobile toolchain; the runtime
// renderer is the production React implementation used by this test.
const { renderToStaticMarkup } = require('react-dom/server') as {
  renderToStaticMarkup: (element: React.ReactElement) => string;
};
const { create, act } = require('react-test-renderer') as {
  create: (element: React.ReactElement) => any;
  act: (callback: () => void | Promise<void>) => Promise<void>;
};

// Render the production FeedScreen with lightweight host-element shims. This
// keeps the test on the component path without requiring a native runtime.
jest.mock('react-native', () => {
  const R = require('react');
  const host = (tag: string) => (props: any) => {
    const { children, testID, editable, disabled, onChangeText, onPress, value } = props || {};
    const mapped = {
      ...(testID ? { 'data-testid': testID } : {}),
      ...(editable === false ? { disabled: true } : {}),
      ...(disabled ? { disabled: true } : {}),
      ...(onChangeText ? { onChangeText } : {}),
      ...(onPress ? { onPress } : {}),
      ...(value !== undefined ? { value } : {}),
    };
    return R.createElement(tag, mapped, children);
  };
  return {
    View: host('div'), Text: host('span'), TextInput: host('input'),
    TouchableOpacity: host('button'), ScrollView: host('div'),
    Modal: host('div'), FlatList: host('div'), ActivityIndicator: host('span'),
    SafeAreaView: host('div'),
    StyleSheet: { create: (styles: any) => styles },
    Platform: { OS: 'web' },
    Keyboard: { addListener: () => ({ remove: () => {} }) },
    AppState: { addEventListener: () => ({ remove: () => {} }) },
    Alert: { alert: () => {} },
  };
});
jest.mock('react-native-safe-area-context', () => ({ SafeAreaView: 'div' }));
jest.mock('react-native-webview', () => {
  const R = require('react');
  const WebView = R.forwardRef(({ onMessage }: any, ref: any) => {
    R.useImperativeHandle(ref, () => ({ injectJavaScript: (source: string) => mockWebViewInjection?.(source) }));
    return R.createElement('webview', { onMessage });
  });
  return { WebView };
});
jest.mock('expo-clipboard', () => ({ getStringAsync: async () => '', setStringAsync: async () => {} }));
jest.mock('../src/lib/client', () => {
  const actual = jest.requireActual('../src/lib/client');
  return {
    ...actual,
    listSessions: jest.fn(async () => [mockInputCapableSession]),
    getTranscript: jest.fn(async () => ({ semantic: [], fallback: [] })),
  };
});

import FeedScreen from '../src/screens/FeedScreen';

const mockInputCapableSession = {
  id: 'controlled_pty:input-a-mobile',
  lifecycleState: 'running',
  adapterCapabilities: ['managedLifecycle', 'liveTerminal', 'input'],
  capabilities: ['history'],
};

function renderFeed(caps: string[], sessionData = mockInputCapableSession): string {
  return renderToStaticMarkup(React.createElement(FeedScreen, {
    onBack: () => {},
    session: sessionData.id,
    caps,
    initialSessionData: sessionData,
    initialTab: 'terminal',
  }));
}

function controlTag(html: string, testID: string): string {
  const match = html.match(new RegExp(`<[^>]*data-testid="${testID}"[^>]*>`));
  if (!match) throw new Error(`missing ${testID}`);
  return match[0];
}

function isDisabled(tree: any, testID: string): boolean {
  return tree.root.find((node: any) => node.props['data-testid'] === testID).props.disabled === true;
}

function statusText(tree: any): string {
  return tree.root.find((node: any) => node.props['data-testid'] === 'terminal-send-status').children.join('');
}

const connectionId = '0123456789abcdef0123456789abcdef';
const inputID1 = 'aaaa111122223333444455556666777788889999aaaabbbbccccddddeeeeffff';
const inputID2 = 'bbbb111122223333444455556666777788889999aaaabbbbccccddddeeeeffff';
const inputID3 = 'cccc111122223333444455556666777788889999aaaabbbbccccddddeeeeffff';

function control(type: string, extra: Record<string, unknown> = {}) {
  return { nativeEvent: { data: JSON.stringify({ type, connectionId, sessionId: mockInputCapableSession.id, generation: 7, ...extra }) } };
}

describe('FeedScreen production input authorization', () => {
  it('renders terminal input and Send disabled for an input-capable session without terminal:input', () => {
    const html = renderFeed(['history']);

    expect(controlTag(html, 'terminal-input')).toContain('disabled');
    expect(controlTag(html, 'terminal-send')).toContain('disabled');
  });

  it('renders terminal input and Send enabled only for an input-capable session with terminal:input', () => {
    const authorizedSession = { ...mockInputCapableSession, capabilities: ['history', 'terminal:input'] };
    const html = renderFeed(['history', 'terminal:input'], authorizedSession);

    expect(controlTag(html, 'terminal-input')).not.toContain('disabled');
    expect(controlTag(html, 'terminal-send')).not.toContain('disabled');
  });

  it('renders terminal input and Send disabled for an authorized device on a non-input-capable session', () => {
    const nonInputSession = { ...mockInputCapableSession, adapterCapabilities: ['managedLifecycle'], capabilities: ['history', 'terminal:input'] };
    const html = renderFeed(['history', 'terminal:input'], nonInputSession);

    expect(controlTag(html, 'terminal-input')).toContain('disabled');
    expect(controlTag(html, 'terminal-send')).toContain('disabled');
  });

  it('renders terminal input and Send disabled when neither device nor session authorizes input', () => {
    const nonInputSession = { ...mockInputCapableSession, adapterCapabilities: ['managedLifecycle'], capabilities: ['history'] };
    const html = renderFeed(['history'], nonInputSession);

    expect(controlTag(html, 'terminal-input')).toContain('disabled');
    expect(controlTag(html, 'terminal-send')).toContain('disabled');
  });

  it('transitions one FeedScreen instance from disabled to enabled on a server hello', async () => {
    const session = { ...mockInputCapableSession, capabilities: ['history', 'terminal:input'] };
    let tree: any;
    await act(async () => {
      tree = create(React.createElement(FeedScreen, {
        onBack: () => {}, session: session.id, caps: ['history'], initialSessionData: session, initialTab: 'terminal',
      }));
    });
    expect(isDisabled(tree, 'terminal-input')).toBe(true);
    expect(isDisabled(tree, 'terminal-send')).toBe(true);

    const webview = tree.root.findByType('webview');
    await act(async () => {
      webview.props.onMessage({ nativeEvent: { data: JSON.stringify({
        type: 'hello', capabilities: ['history', 'terminal:input'], connectionId, sessionId: session.id, generation: 7,
      }) } });
    });
    expect(isDisabled(tree, 'terminal-input')).toBe(false);
    expect(isDisabled(tree, 'terminal-send')).toBe(false);
    await act(async () => tree.unmount());
  });

  it('requires both line-frame accepted results even when the first ACK arrives before Enter is pending', async () => {
    jest.useFakeTimers();
    const session = { ...mockInputCapableSession, capabilities: ['history', 'terminal:input'] };
    let tree: any;
    await act(async () => {
      tree = create(React.createElement(FeedScreen, {
        onBack: () => {}, session: session.id, caps: ['history', 'terminal:input'], initialSessionData: session, initialTab: 'terminal',
      }));
    });
    const webview = tree.root.findByType('webview');
    await act(async () => webview.props.onMessage(control('hello', { capabilities: ['history', 'terminal:input'] })));
    const input = tree.root.find((node: any) => node.props['data-testid'] === 'terminal-input');
    const send = tree.root.find((node: any) => node.props['data-testid'] === 'terminal-send');
    await act(async () => input.props.onChangeText('keep me until both ACKs'));
    await act(async () => send.props.onPress());
    // An unrelated macro/paste pending frame arrives first; operation tags
    // prevent it from claiming the text slot.
    await act(async () => webview.props.onMessage(control('input_pending', { inputId: inputID3, operationId: 'macro-1', part: 'text' })));
    await act(async () => webview.props.onMessage(control('input_result', { inputId: inputID3, outcome: 'accepted', sequence: 99 })));
    // A non-line macro ACK must not overwrite the line's in-flight status.
    expect(statusText(tree)).not.toBe('Delivered to terminal');
    await act(async () => webview.props.onMessage(control('input_pending', { inputId: inputID1, operationId: 'line-1', part: 'text' })));
    expect(statusText(tree)).toBe('Sent to socket');
    // The text ACK is deliberately before the 40 ms Enter request. It must
    // not claim delivery while the second frame is not even pending.
    await act(async () => webview.props.onMessage(control('input_result', { inputId: inputID1, outcome: 'accepted', sequence: 1 })));
    expect(statusText(tree)).not.toBe('Delivered to terminal');
    await act(async () => { jest.advanceTimersByTime(40); });
    await act(async () => webview.props.onMessage(control('input_pending', { inputId: inputID2, operationId: 'line-1', part: 'enter' })));
    await act(async () => webview.props.onMessage(control('input_result', { inputId: inputID2, outcome: 'accepted', sequence: 2 })));
    expect(statusText(tree)).toBe('Delivered to terminal');
    expect(tree.root.find((node: any) => node.props['data-testid'] === 'terminal-input').props.value).toBe('');
    await act(async () => tree.unmount());
    jest.useRealTimers();
  });

  it('preserves soft-keyboard Enter text and newer edits while an earlier line awaits both ACKs', async () => {
    jest.useFakeTimers();
    const session = { ...mockInputCapableSession, capabilities: ['history', 'terminal:input'] };
    let tree: any;
    await act(async () => { tree = create(React.createElement(FeedScreen, { onBack: () => {}, session: session.id, caps: ['history', 'terminal:input'], initialSessionData: session, initialTab: 'terminal' })); });
    const webview = tree.root.findByType('webview');
    const input = () => tree.root.find((node: any) => node.props['data-testid'] === 'terminal-input');
    await act(async () => webview.props.onMessage(control('hello', { capabilities: ['history', 'terminal:input'] })));
    // Soft-keyboard Enter must not clear before the line is acknowledged.
    await act(async () => input().props.onChangeText('keyboard command\n'));
    expect(input().props.value).toBe('keyboard command');
    await act(async () => webview.props.onMessage(control('input_pending', { inputId: inputID1, operationId: 'line-1', part: 'text' })));
    await act(async () => input().props.onChangeText('newer unsent edit'));
    await act(async () => webview.props.onMessage(control('input_result', { inputId: inputID1, outcome: 'accepted' })));
    await act(async () => { jest.advanceTimersByTime(40); });
    await act(async () => webview.props.onMessage(control('input_pending', { inputId: inputID2, operationId: 'line-1', part: 'enter' })));
    await act(async () => webview.props.onMessage(control('input_result', { inputId: inputID2, outcome: 'accepted' })));
    expect(statusText(tree)).toBe('Delivered to terminal');
    expect(input().props.value).toBe('newer unsent edit');
    await act(async () => tree.unmount());
    jest.useRealTimers();
  });

  it('makes an unknown Enter fail the text sibling and ignores late text or macro results', async () => {
    jest.useFakeTimers();
    const session = { ...mockInputCapableSession, capabilities: ['history', 'terminal:input'] };
    let tree: any;
    await act(async () => { tree = create(React.createElement(FeedScreen, { onBack: () => {}, session: session.id, caps: ['history', 'terminal:input'], initialSessionData: session, initialTab: 'terminal' })); });
    const webview = tree.root.findByType('webview');
    const input = () => tree.root.find((node: any) => node.props['data-testid'] === 'terminal-input');
    const send = () => tree.root.find((node: any) => node.props['data-testid'] === 'terminal-send');
    await act(async () => webview.props.onMessage(control('hello', { capabilities: ['history', 'terminal:input'] })));
    await act(async () => input().props.onChangeText('unknown sibling'));
    await act(async () => send().props.onPress());
    await act(async () => webview.props.onMessage(control('input_pending', { inputId: inputID1, operationId: 'line-1', part: 'text' })));
    await act(async () => { jest.advanceTimersByTime(40); });
    await act(async () => webview.props.onMessage(control('input_pending', { inputId: inputID2, operationId: 'line-1', part: 'enter' })));
    await act(async () => webview.props.onMessage({ nativeEvent: { data: JSON.stringify({ type: 'delivery_unknown', operationId: 'line-1', part: 'enter' }) } }));
    expect(statusText(tree)).toBe('Possible partial delivery');
    await act(async () => webview.props.onMessage(control('input_result', { inputId: inputID1, outcome: 'accepted' })));
    await act(async () => webview.props.onMessage({ nativeEvent: { data: JSON.stringify({ type: 'delivery_unknown', operationId: 'macro-1', part: 'text' }) } }));
    expect(statusText(tree)).toBe('Possible partial delivery');
    await act(async () => tree.unmount());
    jest.useRealTimers();
  });

  it('ignores result frames whose connection, session, generation, or input ID does not match the pending request', async () => {
    const session = { ...mockInputCapableSession, capabilities: ['history', 'terminal:input'] };
    let tree: any;
    await act(async () => { tree = create(React.createElement(FeedScreen, { onBack: () => {}, session: session.id, caps: ['history', 'terminal:input'], initialSessionData: session, initialTab: 'terminal' })); });
    const webview = tree.root.findByType('webview');
    await act(async () => webview.props.onMessage(control('hello', { capabilities: ['history', 'terminal:input'] })));
    await act(async () => webview.props.onMessage(control('input_pending', { inputId: inputID1 })));
    for (const mismatch of [
      { connectionId: 'wrong-connection' },
      { sessionId: 'controlled_pty:wrong-session' },
      { generation: 8 },
      { inputId: inputID2 },
    ]) {
      await act(async () => webview.props.onMessage(control('input_result', { inputId: inputID1, outcome: 'accepted', ...mismatch })));
      expect(statusText(tree)).toBe('Sent to socket');
    }
    await act(async () => webview.props.onMessage(control('input_result', { inputId: inputID1, outcome: 'accepted' })));
    expect(statusText(tree)).toBe('Delivered to terminal');
    await act(async () => tree.unmount());
  });

  it('clears old pending ACKs on reconnect and never lets a late sibling turn partial delivery into Delivered', async () => {
    jest.useFakeTimers();
    const session = { ...mockInputCapableSession, capabilities: ['history', 'terminal:input'] };
    let tree: any;
    await act(async () => { tree = create(React.createElement(FeedScreen, { onBack: () => {}, session: session.id, caps: ['history', 'terminal:input'], initialSessionData: session, initialTab: 'terminal' })); });
    const webview = tree.root.findByType('webview');
    const input = () => tree.root.find((node: any) => node.props['data-testid'] === 'terminal-input');
    const send = () => tree.root.find((node: any) => node.props['data-testid'] === 'terminal-send');
    await act(async () => webview.props.onMessage(control('hello', { capabilities: ['history', 'terminal:input'] })));
    await act(async () => webview.props.onMessage(control('input_pending', { inputId: inputID1 })));
    await act(async () => webview.props.onMessage(control('hello', { capabilities: ['history', 'terminal:input'], connectionId: 'fedcba9876543210fedcba9876543210' })));
    await act(async () => webview.props.onMessage(control('input_result', { inputId: inputID1, outcome: 'accepted' })));
    expect(statusText(tree)).not.toBe('Delivered to terminal');

    // Start one line and make both frames pending before failing the first.
    await act(async () => input().props.onChangeText('partial'));
    await act(async () => send().props.onPress());
    await act(async () => webview.props.onMessage(control('input_pending', { connectionId: 'fedcba9876543210fedcba9876543210', inputId: inputID2, operationId: 'line-1', part: 'text' })));
    await act(async () => { jest.advanceTimersByTime(40); });
    await act(async () => webview.props.onMessage(control('input_pending', { connectionId: 'fedcba9876543210fedcba9876543210', inputId: inputID3, operationId: 'line-1', part: 'enter' })));
    await act(async () => webview.props.onMessage(control('input_result', { connectionId: 'fedcba9876543210fedcba9876543210', inputId: inputID2, outcome: 'write_failed' })));
    expect(statusText(tree)).toBe('Possible partial delivery');
    await act(async () => webview.props.onMessage(control('input_result', { connectionId: 'fedcba9876543210fedcba9876543210', inputId: inputID3, outcome: 'accepted' })));
    expect(statusText(tree)).toBe('Possible partial delivery');
    expect(input().props.value).toBe('partial');

    // An overlapping Send cannot replace a live line operation's ACK slots.
    await act(async () => input().props.onChangeText('one'));
    await act(async () => send().props.onPress());
    await act(async () => input().props.onChangeText('two'));
    await act(async () => send().props.onPress());
    expect(statusText(tree)).toBe('Not delivered');
    await act(async () => tree.unmount());
    jest.useRealTimers();
  });

  it('keeps pending ACK state on a repeated identical hello', async () => {
    const session = { ...mockInputCapableSession, capabilities: ['history', 'terminal:input'] };
    let tree: any;
    await act(async () => { tree = create(React.createElement(FeedScreen, { onBack: () => {}, session: session.id, caps: ['history', 'terminal:input'], initialSessionData: session, initialTab: 'terminal' })); });
    const webview = tree.root.findByType('webview');
    const hello = control('hello', { capabilities: ['history', 'terminal:input'] });
    await act(async () => webview.props.onMessage(hello));
    await act(async () => webview.props.onMessage(control('input_pending', { inputId: inputID1 })));
    await act(async () => webview.props.onMessage(hello));
    await act(async () => webview.props.onMessage(control('input_result', { inputId: inputID1, outcome: 'accepted' })));
    expect(statusText(tree)).toBe('Delivered to terminal');
    await act(async () => tree.unmount());
  });

  it('fails closed when a permission-refresh hello revokes input during a pending ACK', async () => {
    const session = { ...mockInputCapableSession, capabilities: ['history', 'terminal:input'] };
    let tree: any;
    await act(async () => { tree = create(React.createElement(FeedScreen, { onBack: () => {}, session: session.id, caps: ['history', 'terminal:input'], initialSessionData: session, initialTab: 'terminal' })); });
    const webview = tree.root.findByType('webview');
    await act(async () => webview.props.onMessage(control('hello', { capabilities: ['history', 'terminal:input'] })));
    await act(async () => webview.props.onMessage(control('input_pending', { inputId: inputID1 })));
    await act(async () => webview.props.onMessage(control('hello', { capabilities: ['history'], connectionId: 'fedcba9876543210fedcba9876543210' })));
    expect(isDisabled(tree, 'terminal-input')).toBe(true);
    await act(async () => webview.props.onMessage(control('input_result', { inputId: inputID1, outcome: 'accepted' })));
    expect(statusText(tree)).not.toBe('Delivered to terminal');
    await act(async () => tree.unmount());
  });

  it('preserves command and reports partial delivery on first or second non-accepted result and timeout', async () => {
    jest.useFakeTimers();
    const session = { ...mockInputCapableSession, capabilities: ['history', 'terminal:input'] };
    let tree: any;
    await act(async () => { tree = create(React.createElement(FeedScreen, { onBack: () => {}, session: session.id, caps: ['history', 'terminal:input'], initialSessionData: session, initialTab: 'terminal' })); });
    const webview = tree.root.findByType('webview');
    const input = () => tree.root.find((node: any) => node.props['data-testid'] === 'terminal-input');
    const send = () => tree.root.find((node: any) => node.props['data-testid'] === 'terminal-send');
    await act(async () => webview.props.onMessage(control('hello', { capabilities: ['history', 'terminal:input'] })));

    await act(async () => input().props.onChangeText('first failure'));
    await act(async () => send().props.onPress());
    await act(async () => webview.props.onMessage(control('input_pending', { inputId: inputID1, operationId: 'line-1', part: 'text' })));
    await act(async () => webview.props.onMessage(control('input_result', { inputId: inputID1, outcome: 'write_failed' })));
    expect(statusText(tree)).toBe('Possible partial delivery');
    expect(input().props.value).toBe('first failure');

    await act(async () => input().props.onChangeText('second failure'));
    await act(async () => send().props.onPress());
    await act(async () => webview.props.onMessage(control('input_pending', { inputId: inputID2, operationId: 'line-2', part: 'text' })));
    await act(async () => webview.props.onMessage(control('input_result', { inputId: inputID2, outcome: 'accepted' })));
    await act(async () => { jest.advanceTimersByTime(40); });
    await act(async () => webview.props.onMessage(control('input_pending', { inputId: inputID3, operationId: 'line-2', part: 'enter' })));
    await act(async () => webview.props.onMessage(control('input_result', { inputId: inputID3, outcome: 'write_failed' })));
    expect(statusText(tree)).toBe('Possible partial delivery');
    expect(input().props.value).toBe('second failure');

    await act(async () => input().props.onChangeText('timeout'));
    await act(async () => send().props.onPress());
    await act(async () => webview.props.onMessage(control('input_pending', { inputId: inputID1, operationId: 'line-3', part: 'text' })));
    await act(async () => { jest.advanceTimersByTime(3000); });
    expect(statusText(tree)).toBe('Possible partial delivery');
    expect(input().props.value).toBe('timeout');
    await act(async () => tree.unmount());
    jest.useRealTimers();
  });

  it('clears a line operation after page delivery_unknown so a later Send is not stranded or retried', async () => {
    jest.useFakeTimers();
    const session = { ...mockInputCapableSession, capabilities: ['history', 'terminal:input'] };
    let tree: any;
    await act(async () => { tree = create(React.createElement(FeedScreen, { onBack: () => {}, session: session.id, caps: ['history', 'terminal:input'], initialSessionData: session, initialTab: 'terminal' })); });
    const webview = tree.root.findByType('webview');
    const input = () => tree.root.find((node: any) => node.props['data-testid'] === 'terminal-input');
    const send = () => tree.root.find((node: any) => node.props['data-testid'] === 'terminal-send');
    await act(async () => webview.props.onMessage(control('hello', { capabilities: ['history', 'terminal:input'] })));
    await act(async () => input().props.onChangeText('lost websocket'));
    await act(async () => send().props.onPress());
    await act(async () => webview.props.onMessage({ nativeEvent: { data: JSON.stringify({ type: 'delivery_unknown', operationId: 'line-1', part: 'text' }) } }));
    expect(statusText(tree)).toBe('Possible partial delivery');
    await act(async () => { jest.advanceTimersByTime(10000); });
    expect(statusText(tree)).toBe('Possible partial delivery');

    // No automatic retry occurs, and the failure releases the operation lock
    // so an explicit later Send can create the next line operation.
    await act(async () => input().props.onChangeText('later command'));
    await act(async () => send().props.onPress());
    await act(async () => webview.props.onMessage(control('input_pending', { inputId: inputID2, operationId: 'line-2', part: 'text' })));
    expect(statusText(tree)).toBe('Sent to socket');
    await act(async () => tree.unmount());
    jest.useRealTimers();
  });

  it('concurrent ACK: late result cannot overwrite Possible partial delivery', async () => {
    jest.useFakeTimers();
    const session = { ...mockInputCapableSession, capabilities: ['history', 'terminal:input'] };
    let tree: any;
    await act(async () => { tree = create(React.createElement(FeedScreen, { onBack: () => {}, session: session.id, caps: ['history', 'terminal:input'], initialSessionData: session, initialTab: 'terminal' })); });
    const webview = tree.root.findByType('webview');
    await act(async () => webview.props.onMessage(control('hello', { capabilities: ['history', 'terminal:input'] })));

    // Standalone input: pending → timeout → Possible partial delivery.
    await act(async () => webview.props.onMessage(control('input_pending', { inputId: inputID1 })));
    expect(statusText(tree)).toBe('Sent to socket');
    await act(async () => { jest.advanceTimersByTime(3000); });
    expect(statusText(tree)).toBe('Possible partial delivery');

    // Late accepted result arrives — must NOT overwrite.
    await act(async () => webview.props.onMessage(control('input_result', { inputId: inputID1, outcome: 'accepted' })));
    expect(statusText(tree)).toBe('Possible partial delivery');
    jest.useRealTimers();
    await act(async () => tree.unmount());
  });

  it('runs the real daemon → served page → WebView → FeedScreen acknowledgement chain', async () => {
    const daemonRoot = path.resolve(__dirname, '../../companion-daemon');
    const daemonHome = fs.mkdtempSync(path.join(os.tmpdir(), 'pokit-c1-mobile-'));
    const goCache = path.join(os.tmpdir(), 'pokit-c1-go-build-cache');
    const goModCache = path.join(os.tmpdir(), 'pokit-c1-go-mod-cache');
    const daemon = spawn('go', ['run', './cmd/devremote', 'daemon', '--insecure-local-only', '--listen-addr', '127.0.0.1:19171'], {
      cwd: daemonRoot,
      env: {
        ...process.env, HOME: daemonHome, SHELL: '/bin/sh',
        GOCACHE: goCache, GOMODCACHE: goModCache,
      },
      stdio: ['ignore', 'pipe', 'pipe'],
      detached: true,
    });
    let daemonLog = '';
    daemon.stdout.on('data', (b: Buffer) => { daemonLog += b.toString(); });
    daemon.stderr.on('data', (b: Buffer) => { daemonLog += b.toString(); });
    const baseURL = 'http://127.0.0.1:19171';
    let sessionID = '';
    let tree: any;
    let pageSocket: any;
    const browserSocketEvents: string[] = [];

    const waitFor = async (predicate: () => boolean, label: string) => {
      const deadline = Date.now() + 20000;
      while (!predicate() && Date.now() < deadline) {
        await new Promise(resolve => setTimeout(resolve, 10));
      }
      if (!predicate()) throw new Error(`timed out waiting for ${label}; ws=${browserSocketEvents.join('|')}; daemon=${daemonLog}`);
    };

    try {
      await waitFor(() => daemonLog.includes('POKIT daemon 127.0.0.1:19171'), 'daemon startup');
      const createRes = await fetch(`${baseURL}/api/sessions?token=dev-token`, {
        method: 'POST', headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({profileId: 'shell', name: 'TERM-C1 mobile integration'}),
      });
      const createBody = await createRes.text();
      expect({ok: createRes.ok, status: createRes.status, body: createBody}).toEqual(
        expect.objectContaining({ok: true}),
      );
      sessionID = JSON.parse(createBody).id;
      expect(sessionID).toMatch(/^controlled_pty:/);

      const pageRes = await fetch(`${baseURL}/term/?session=${encodeURIComponent(sessionID)}&token=dev-token`);
      expect(pageRes.ok).toBe(true);
      const html = await pageRes.text();
      const script = html.match(/<script>\s*([\s\S]*?)<\/script>\s*<\/body>/)?.[1];
      expect(script).toBeTruthy();

      const nativeFrames: string[] = [];
      class BrowserWebSocket {
        raw: any;
        onopen: any;
        onmessage: any;
        onclose: any;
        onerror: any;
        constructor(url: string) {
          this.raw = new NodeWebSocket(url);
          this.raw.on('open', () => { browserSocketEvents.push('open'); this.onopen?.({}); });
          this.raw.on('message', (data: Buffer, isBinary: boolean) => {
            browserSocketEvents.push(`message:${isBinary ? 'binary' : 'text'}:${data.toString().slice(0, 32)}`);
            this.onmessage?.({data: isBinary ? data : data.toString()});
          });
          this.raw.on('close', (code: number) => { browserSocketEvents.push(`close:${code}`); this.onclose?.({code}); });
          this.raw.on('error', (error: Error) => { browserSocketEvents.push(`error:${error.message}`); this.onerror?.(error); });
        }
        get readyState() { return this.raw.readyState; }
        send(data: string) { this.raw.send(data); }
        close() { this.raw.close(); }
      }
      const terminal = function(this: any) {
        this.open = () => {};
        this.focus = () => {};
        this.clear = () => {};
        this.resize = () => {};
        this.write = () => {};
        this.writeln = () => {};
        this.onScroll = () => {};
        this.onData = () => {};
      };
      const target = {clientHeight: 340, clientWidth: 800, style: {}};
      const page: any = {
        location: {
          protocol: 'http:', host: '127.0.0.1:19171',
          search: `?session=${encodeURIComponent(sessionID)}&token=dev-token`,
        },
        document: {
          getElementById: (id: string) => id === 'status' ? {style: {}} : target,
          addEventListener: () => {},
        },
        Terminal: terminal,
        WebSocket: BrowserWebSocket,
        TextEncoder, TextDecoder, Uint8Array,
        crypto: webcrypto,
        btoa: (value: string) => Buffer.from(value, 'binary').toString('base64'),
        setTimeout: () => 1, setInterval: () => 1, clearInterval: () => {}, clearTimeout: () => {},
        addEventListener: () => {},
        ReactNativeWebView: {postMessage: (raw: string) => nativeFrames.push(raw)},
      };
      page.window = page;
      const context = vm.createContext(page);
      vm.runInContext(script!, context, {filename: 'served-term-page.js'});
      pageSocket = page.ws;
      expect(pageSocket).toBeTruthy();

      const session = {
        id: sessionID, lifecycleState: 'running',
        adapterCapabilities: ['managedLifecycle', 'liveTerminal', 'input'],
        capabilities: ['history', 'terminal:input'],
      };
      // The production component refreshes through listSessions after mount.
      // Point its existing test transport at this daemon-owned session so that
      // refresh preserves the same live-terminal capability boundary.
      Object.assign(mockInputCapableSession, session);
      await act(async () => {
        tree = create(React.createElement(FeedScreen, {
          onBack: () => {}, session: sessionID, caps: ['history'],
          initialSessionData: session, initialTab: 'terminal',
        }));
      });
      const webview = tree.root.findByType('webview');
      let delivered = 0;
      const deliverNativeFrames = async () => {
        const frames = nativeFrames.slice(delivered);
        delivered += frames.length;
        if (frames.length) await act(async () => {
          for (const raw of frames) webview.props.onMessage({nativeEvent: {data: raw}});
        });
      };

      await waitFor(() => nativeFrames.some(raw => JSON.parse(raw).type === 'hello'), 'served page hello');
      await deliverNativeFrames();
      expect(isDisabled(tree, 'terminal-send')).toBe(false);

      // This is the real FeedScreen Send action. Its production injection
      // calls pokitSendInput in the literal daemon page VM; that page's real
      // WebSocket reaches HandleWS and its ACKs come back via postMessage.
      mockWebViewInjection = (source: string) => vm.runInContext(source, context, {filename: 'feed-screen-injection.js'});
      const input = tree.root.find((node: any) => node.props['data-testid'] === 'terminal-input');
      const send = tree.root.find((node: any) => node.props['data-testid'] === 'terminal-send');
      await act(async () => input.props.onChangeText('real daemon line'));
      await act(async () => send.props.onPress());

      await waitFor(() => nativeFrames.filter(raw => JSON.parse(raw).type === 'input_result').length >= 2, 'two real HandleWS ACKs');
      await deliverNativeFrames();
      expect(nativeFrames.filter(raw => JSON.parse(raw).type === 'input_pending')).toHaveLength(2);
      expect(nativeFrames.filter(raw => JSON.parse(raw).type === 'input_result')).toHaveLength(2);
      expect(statusText(tree)).toBe('Delivered to terminal');
    } finally {
      mockWebViewInjection = null;
      try { pageSocket?.close(); } catch {}
      if (tree) await act(async () => tree.unmount());
      if (sessionID) {
        await fetch(`${baseURL}/api/sessions/${encodeURIComponent(sessionID)}/kill?token=dev-token`, {method: 'POST'}).catch(() => {});
      }
      // `go run` owns a short-lived Go wrapper plus the daemon child. Kill the
      // dedicated process group so a failed assertion cannot leak the child.
      try { process.kill(-daemon.pid, 'SIGTERM'); } catch {}
      let fallback: any;
      await Promise.race([
        new Promise(resolve => daemon.once('exit', resolve)),
        new Promise(resolve => { fallback = setTimeout(resolve, 3000); fallback.unref?.(); }),
      ]);
      if (fallback) clearTimeout(fallback);
      fs.rmSync(daemonHome, {recursive: true, force: true});
    }
  }, 60000);

});
