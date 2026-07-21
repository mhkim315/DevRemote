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
    const { children, testID, editable, disabled } = props || {};
    const mapped = {
      ...(testID ? { 'data-testid': testID } : {}),
      ...(editable === false ? { disabled: true } : {}),
      ...(disabled ? { disabled: true } : {}),
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
  return { WebView: ({ onMessage }: any) => R.createElement('webview', { onMessage }) };
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
        type: 'hello', capabilities: ['history', 'terminal:input'],
      }) } });
    });
    expect(isDisabled(tree, 'terminal-input')).toBe(false);
    expect(isDisabled(tree, 'terminal-send')).toBe(false);
    await act(async () => tree.unmount());
  });

  it('labels socket send as delivered only after the exact-generation ACK, then not delivered on timeout', async () => {
    jest.useFakeTimers();
    const session = { ...mockInputCapableSession, capabilities: ['history', 'terminal:input'] };
    let tree: any;
    await act(async () => {
      tree = create(React.createElement(FeedScreen, {
        onBack: () => {}, session: session.id, caps: ['history', 'terminal:input'], initialSessionData: session, initialTab: 'terminal',
      }));
    });
    const webview = tree.root.findByType('webview');
    // PB.7 Input-B: versioned protocol — tracks by inputId, not sequence.
    await act(async () => webview.props.onMessage({ nativeEvent: { data: JSON.stringify({ type: 'input_pending', generation: 7, inputId: 'aaaa111122223333444455556666777788889999aaaabbbbccccddddeeeeffff' }) } }));
    expect(statusText(tree)).toBe('Sent to socket');
    await act(async () => webview.props.onMessage({ nativeEvent: { data: JSON.stringify({ type: 'input_result', inputId: 'aaaa111122223333444455556666777788889999aaaabbbbccccddddeeeeffff', outcome: 'accepted', generation: 7, sequence: 1 }) } }));
    expect(statusText(tree)).toBe('Delivered to terminal');

    await act(async () => webview.props.onMessage({ nativeEvent: { data: JSON.stringify({ type: 'input_pending', generation: 7, inputId: 'bbbb111122223333444455556666777788889999aaaabbbbccccddddeeeeffff' }) } }));
    await act(async () => { jest.advanceTimersByTime(3000); });
    expect(statusText(tree)).toBe('Not delivered');
    await act(async () => tree.unmount());
    jest.useRealTimers();
  });

});
