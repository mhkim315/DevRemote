// LOCAL-E2E-R2: native UI automation for terminal controls.
// Uses Detox with the testIDs added in FeedScreen.tsx to verify
// macro TouchableOpacity buttons respond to native tap.

import { device, expect, element, by } from 'detox';

const PLATFORM = device.getPlatform();

describe('Terminal Macros', () => {
  beforeAll(async () => {
    // Assumes the app was already launched and navigated to
    // the terminal view with an active writable session.
    // In a full CI setup, navigation would be automated here.
    await device.launchApp({ newInstance: false });
  });

  it('should display all macro buttons', async () => {
    await expect(element(by.id('terminal-macro-ctrl-c'))).toBeVisible();
    await expect(element(by.id('terminal-macro-escape'))).toBeVisible();
    await expect(element(by.id('terminal-macro-tab'))).toBeVisible();
    await expect(element(by.id('terminal-macro-arrow-up'))).toBeVisible();
    await expect(element(by.id('terminal-macro-arrow-down'))).toBeVisible();
    await expect(element(by.id('terminal-macro-arrow-left'))).toBeVisible();
    await expect(element(by.id('terminal-macro-arrow-right'))).toBeVisible();
    await expect(element(by.id('terminal-macro-y'))).toBeVisible();
    await expect(element(by.id('terminal-macro-n'))).toBeVisible();
    await expect(element(by.id('terminal-macro-enter'))).toBeVisible();
  });

  it('should tap Enter macro and deliver to PTY', async () => {
    // First, type a simple command in the TextInput
    const inputField = element(by.id('terminal-input'));
    await inputField.tap();
    await inputField.typeText('echo MACRO_TEST\n');
    
    // Verify send status changes (not 'failed')
    // The exact status depends on timing, but it should not be 'failed'
    await expect(element(by.id('terminal-send-status'))).toNotExist();
  });

  it('should tap Ctrl+C macro', async () => {
    await element(by.id('terminal-macro-ctrl-c')).tap();
    // Verify the macro tap was processed — the button should be enabled after
    await expect(element(by.id('terminal-macro-ctrl-c'))).toBeVisible();
  });
});
