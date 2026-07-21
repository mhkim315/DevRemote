import React from 'react';
// react-dom/server has no @types package in the mobile toolchain; the runtime
// renderer is the production React implementation used by this test.
const { renderToStaticMarkup } = require('react-dom/server') as {
  renderToStaticMarkup: (element: React.ReactElement) => string;
};

// Render the production FeedScreen with lightweight host-element shims. This
// keeps the test on the component path without requiring a native runtime.
jest.mock('react-native', () => {
  const R = require('react');
  const host = (tag: string) => (props: any) => {
    const { children, testID, editable, disabled, ...rest } = props || {};
    const mapped = {
      ...rest,
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
jest.mock('react-native-webview', () => ({ WebView: 'div' }));
jest.mock('expo-clipboard', () => ({ getStringAsync: async () => '', setStringAsync: async () => {} }));

import FeedScreen, { serverCapabilitiesFromControl } from '../src/screens/FeedScreen';

const inputCapableSession = {
  id: 'controlled_pty:input-a-mobile',
  lifecycleState: 'running',
  adapterCapabilities: ['input'],
  capabilities: ['history'],
};

function renderFeed(caps: string[], sessionData = inputCapableSession): string {
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

describe('FeedScreen production input authorization', () => {
  it('renders terminal input and Send disabled for an input-capable session without terminal:input', () => {
    const html = renderFeed(['history']);

    expect(controlTag(html, 'terminal-input')).toContain('disabled');
    expect(controlTag(html, 'terminal-send')).toContain('disabled');
  });

  it('renders terminal input and Send enabled only for an input-capable session with terminal:input', () => {
    const authorizedSession = { ...inputCapableSession, capabilities: ['history', 'terminal:input'] };
    const html = renderFeed(['history', 'terminal:input'], authorizedSession);

    expect(controlTag(html, 'terminal-input')).not.toContain('disabled');
    expect(controlTag(html, 'terminal-send')).not.toContain('disabled');
  });

  it('hello capability transition changes the rendered input authorization', () => {
    const before = renderFeed(['history']);
    expect(controlTag(before, 'terminal-send')).toContain('disabled');

    const fromHello = serverCapabilitiesFromControl({
      type: 'hello', capabilities: ['history', 'terminal:input'],
    });
    expect(fromHello).toEqual(['history', 'terminal:input']);

    const after = renderFeed(fromHello!, { ...inputCapableSession, capabilities: fromHello! });
    expect(controlTag(after, 'terminal-send')).not.toContain('disabled');
  });

});
