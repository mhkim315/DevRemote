import React, { useState, useEffect } from 'react';
import { StatusBar } from 'expo-status-bar';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import { Linking, View, Text } from 'react-native';
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
import type { AuthContext } from './src/lib/authMode';

// E6: explicit test-build gate — does not depend on stored baseURL.
// Set EXPO_PUBLIC_POKIT_NO_LOGIN_LOCAL_TEST=1 for local test builds.
const NO_LOGIN = typeof process !== 'undefined' &&
  process.env?.EXPO_PUBLIC_POKIT_NO_LOGIN_LOCAL_TEST === '1';

function AppContent() {
  const [session, setSession] = useState<Session | null>(NO_LOGIN ? {} as Session : null);
  const { isConnected, loading } = useConnection();

  // M3-auth-4A: explicit auth mode — no inference from optional props.
  const [authCtx, setAuthCtx] = useState<AuthContext>(() => {
    if (NO_LOGIN) return { mode: 'explicit_local_dev', legacyToken: 'dev-token' };
    return { mode: 'initializing' };
  });

  useEffect(() => {
    if (NO_LOGIN) return;
    loadPairing()
      .then(p => {
        if (!p) { setAuthCtx({ mode: 'pairing_required' }); return; }
        const base = getBaseURL();
        if (!base) { setAuthCtx({ mode: 'pairing_required' }); return; }
        // BLOCKER 1: origin binding — the current base URL must match the
        // canonical origin stored in the pairing record (scheme+host+port).
        let bu: URL;
        try { bu = new URL(base); } catch { setAuthCtx({ mode: 'failed' }); return; }
        if (bu.protocol !== 'https:') { setAuthCtx({ mode: 'failed' }); return; }
        if (bu.username || bu.password || bu.search || bu.hash || (bu.pathname !== '/' && bu.pathname !== '')) {
          setAuthCtx({ mode: 'failed' }); return;
        }
        const currentOrigin = bu.origin;
        if (p.origin && currentOrigin !== p.origin) { setAuthCtx({ mode: 'failed' }); return; }
        let dk;
        try { dk = createPokitDeviceKey(); } catch { setAuthCtx({ mode: 'failed' }); return; }
        setAuthCtx({ mode: 'paired_device', tokenMgr: new TokenManager(p, dk, base), baseURL: base });
      })
      .catch(() => setAuthCtx({ mode: 'failed' }));
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

  if (loading || authCtx.mode === 'initializing') return null;

  // BLOCKER 3: pairing_required / failed must NOT enter RootTabs.
  if (authCtx.mode === 'pairing_required') {
    return (
      <SafeAreaProvider><StatusBar style="light" />
        <View style={{ flex: 1, backgroundColor: '#000', alignItems: 'center', justifyContent: 'center', padding: 24 }}>
          <Text style={{ color: '#45EBE9', fontSize: 18, fontWeight: '800', marginBottom: 12 }}>PAIRING REQUIRED</Text>
          <Text style={{ color: '#8b949e', fontSize: 14, textAlign: 'center' }}>Scan the QR code from your terminal to pair this device.</Text>
        </View>
      </SafeAreaProvider>
    );
  }
  if (authCtx.mode === 'failed') {
    return (
      <SafeAreaProvider><StatusBar style="light" />
        <View style={{ flex: 1, backgroundColor: '#000', alignItems: 'center', justifyContent: 'center', padding: 24 }}>
          <Text style={{ color: '#f85149', fontSize: 18, fontWeight: '800', marginBottom: 12 }}>DEVICE KEY FAILED</Text>
          <Text style={{ color: '#8b949e', fontSize: 14, textAlign: 'center' }}>The device identity could not be initialized. Restart the app to try again.</Text>
        </View>
      </SafeAreaProvider>
    );
  }

  return (
    <SafeAreaProvider>
      <StatusBar style="light" />
      {!session ? (
        <AuthScreen />
      ) : !isConnected ? (
        <ConnectScreen />
      ) : (
        <RootTabs token={session.access_token} authCtx={authCtx} />
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
