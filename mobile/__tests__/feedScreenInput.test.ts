import React from 'react';
// React 19 requires this opt-in for state updates flushed by act().
(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
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
    R.useImperativeHandle(ref, () => ({ injectJavaScript: () => {} }));
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

});
