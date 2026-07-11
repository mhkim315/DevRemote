import React, { useState, useEffect } from 'react';
import { StatusBar } from 'expo-status-bar';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import { Linking } from 'react-native';
import * as Notifications from 'expo-notifications';
import { supabase } from './src/lib/supabase';
import { Session } from '@supabase/supabase-js';
import { registerPushToken } from './src/lib/client';
import { ConnectionProvider, useConnection } from './src/lib/connection';

import AuthScreen from './src/screens/AuthScreen';
import ConnectScreen from './src/screens/ConnectScreen';
import { RootTabs } from './src/navigation/RootNavigator';
import { TokenManager } from './src/lib/authClient';
import { loadPairing } from './src/lib/pairingStore';
import { createPokitDeviceKey } from './modules/pokit-device-key';
import { getBaseURL } from './src/lib/client';

// E6: explicit test-build gate — does not depend on stored baseURL.
// Set EXPO_PUBLIC_POKIT_NO_LOGIN_LOCAL_TEST=1 for local test builds.
const NO_LOGIN = typeof process !== 'undefined' &&
  process.env?.EXPO_PUBLIC_POKIT_NO_LOGIN_LOCAL_TEST === '1';

function AppContent() {
  const [session, setSession] = useState<Session | null>(NO_LOGIN ? {} as Session : null);
  const [tokenMgr, setTokenMgr] = useState<TokenManager | undefined>(undefined);
  const [pairedBaseURL, setPairedBaseURL] = useState<string | undefined>(undefined);
  const [pairingInitDone, setPairingInitDone] = useState(NO_LOGIN); // NO_LOGIN skips this
  const { isConnected, loading } = useConnection();

  // M3-auth-4A: if a pairing exists, create a host-scoped TokenManager.
  // Until this completes, the authenticated terminal path is unavailable
  // and the legacy token path is NOT used as fallback in remote mode.
  useEffect(() => {
    if (NO_LOGIN) { setPairingInitDone(true); return; }
    loadPairing().then(p => {
      if (p && getBaseURL()) {
        // BLOCKER 6: verify the pairing origin matches the current baseURL.
        const base = getBaseURL();
        try {
          const bu = new URL(base);
          const dk = createPokitDeviceKey();
          setTokenMgr(new TokenManager(p, dk, base));
          setPairedBaseURL(base);
        } catch {}
      }
      setPairingInitDone(true);
    }).catch(() => setPairingInitDone(true));
  }, []);

  useEffect(() => {
    // E6: skip Supabase auth entirely when test flag is set.
    if (NO_LOGIN) {
      const devSession = {
        access_token: 'dev-token',
        refresh_token: 'dev-refresh',
        expires_in: 999999,
        token_type: 'bearer',
        user: { id: 'dev-user', email: 'dev@localhost' },
      } as any as Session;
      setSession(devSession);
      return;
    }

    supabase.auth.getSession().then(({ data: { session } }) => {
      setSession(session);
    });

    supabase.auth.onAuthStateChange((_event, session) => {
      setSession(session);
    });
  }, []);

  useEffect(() => {
    async function setupPush() {
      if (!isConnected || !session) return;
      const { status: existingStatus } = await Notifications.getPermissionsAsync();
      let finalStatus = existingStatus;
      if (existingStatus !== 'granted') {
        const { status } = await Notifications.requestPermissionsAsync();
        finalStatus = status;
      }
      if (finalStatus !== 'granted') return;

      const tokenData = await Notifications.getExpoPushTokenAsync({ projectId: 'devremote-mhk' });
      const pushToken = tokenData.data;
      console.log('Push token:', pushToken);

      registerPushToken(session.access_token, pushToken).catch(console.error);
    }
    setupPush();
  }, [isConnected, session]);

  // P1b: handle notification tap → navigate to session.
  useEffect(() => {
    // Cold start: app opened from notification.
    Notifications.getLastNotificationResponseAsync().then(response => {
      if (response) {
        handleNotificationResponse(response);
      }
    });

    // Warm start: notification tapped while app is open.
    const sub = Notifications.addNotificationResponseReceivedListener(response => {
      handleNotificationResponse(response);
    });

    return () => sub.remove();
  }, []);

  function handleNotificationResponse(response: Notifications.NotificationResponse) {
    const data = response.notification.request.content.data;
    const url = data?.['url'] as string | undefined;
    if (url) {
      Linking.openURL(url);
    }
  }

  if (loading || !pairingInitDone) return null;

  return (
    <SafeAreaProvider>
      <StatusBar style="light" />
      {!session ? (
        <AuthScreen />
      ) : !isConnected ? (
        <ConnectScreen />
      ) : (
        <RootTabs token={session.access_token} tokenMgr={tokenMgr} baseURL={pairedBaseURL} />
      )}
    </SafeAreaProvider>
  );
}

export default function App() {
  return (
    <ConnectionProvider>
      <AppContent />
    </ConnectionProvider>
  );
}
