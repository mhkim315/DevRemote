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
import { getBaseURL, setDeviceAuth } from './src/lib/client';
import { canonicalOrigin, completePairing, selectAppRoute, type AuthContext } from './src/lib/authMode';

// E6: explicit test-build gate — does not depend on stored baseURL.
// Set EXPO_PUBLIC_POKIT_NO_LOGIN_LOCAL_TEST=1 for local test builds.
const NO_LOGIN = typeof process !== 'undefined' &&
  process.env?.EXPO_PUBLIC_POKIT_NO_LOGIN_LOCAL_TEST === '1';

function AppContent() {
  const [session, setSession] = useState<Session | null>(NO_LOGIN ? {} as Session : null);
  const { isConnected, loading, connect } = useConnection();

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
        const { origin: currentOrigin, error: originErr } = canonicalOrigin(base);
        if (originErr || !currentOrigin) { setAuthCtx({ mode: 'failed' }); return; }
        if (!p.origin) { setAuthCtx({ mode: 'failed' }); return; }
        if (currentOrigin !== p.origin) { setAuthCtx({ mode: 'failed' }); return; }
        let dk;
        try { dk = createPokitDeviceKey(); } catch { setAuthCtx({ mode: 'failed' }); return; }
        // M3-auth-4A: install the device bearer BEFORE probing so the restored
        // paired session connects with an authenticated REST probe (not the
        // unauthenticated legacy probe).
        const mgr = new TokenManager(p, dk, base);
        setDeviceAuth(mgr);
        setAuthCtx({ mode: 'paired_device', tokenMgr: mgr, baseURL: base });
        connect(base).catch(() => {});
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

  // M3-auth-4A: one authoritative routing decision. A valid paired_device
  // reaches the product WITHOUT a Supabase session (device pairing is the
  // remote auth root); Supabase only gates the legacy/explicit_local_dev path.
  const route = selectAppRoute(authCtx, { loading, session: !!session, isConnected });

  if (route === 'loading') return null;

  // pairing_required / failed must NOT enter RootTabs.
  if (route === 'pairing_required') {
    // Show ConnectScreen with an AWAITABLE onPaired callback (Blocker C): on a
    // saved pairing, install trusted auth state atomically (reload pairing →
    // validate origin → DeviceKey/TokenManager → enter paired_device) and
    // report success. ConnectScreen connects ONLY after this resolves true, so
    // connection state can never race ahead of trusted auth state.
    return (
      <SafeAreaProvider><StatusBar style="light" />
        <ConnectScreen onPaired={async () => {
          const ctx = await completePairing({
            loadPairing,
            getBaseURL,
            createDeviceKey: createPokitDeviceKey,
            makeTokenManager: (p, dk, base) => new TokenManager(p, dk, base),
          });
          // Install the device bearer so the subsequent connect() probe (run by
          // ConnectScreen after this resolves) authenticates as the paired device.
          if (ctx.mode === 'paired_device') setDeviceAuth(ctx.tokenMgr ?? null);
          setAuthCtx(ctx);
          return ctx.mode === 'paired_device';
        }} />
      </SafeAreaProvider>
    );
  }
  if (route === 'failed') {
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
      {route === 'supabase_auth' ? (
        <AuthScreen />
      ) : route === 'product' ? (
        <RootTabs token={session?.access_token} authCtx={authCtx} />
      ) : (
        // device_connect (paired, authenticated probe pending) | legacy_connect
        <ConnectScreen />
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
