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

import FeedScreen from '../src/screens/FeedScreen';

describe('FeedScreen production input authorization', () => {
  it('renders terminal input and Send disabled without terminal:input capability', () => {
    const html = renderToStaticMarkup(React.createElement(FeedScreen, {
      onBack: () => {},
      session: 'controlled_pty:input-a-mobile',
      caps: ['history'],
      initialTab: 'terminal',
    }));

    expect(html).toMatch(/data-testid="terminal-input"[^>]*disabled/);
    expect(html).toMatch(/data-testid="terminal-send"[^>]*disabled/);
  });

});
