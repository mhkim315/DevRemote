import React, { useState, useEffect, useCallback } from 'react';
import { StatusBar } from 'expo-status-bar';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import { Linking, View, Text } from 'react-native';
import * as Notifications from 'expo-notifications';
import { supabase } from './src/lib/supabase';
import { Session } from '@supabase/supabase-js';
import { getNotificationStatus, registerPushToken } from './src/lib/client';
import { ConnectionProvider, useConnection } from './src/lib/connection';

import AuthScreen from './src/screens/AuthScreen';
import ConnectScreen from './src/screens/ConnectScreen';
import { RootTabs } from './src/navigation/RootNavigator';
import { TokenManager } from './src/lib/authClient';
import { loadPairing } from './src/lib/pairingStore';
import { createPokitDeviceKey } from './modules/pokit-device-key';
import { getBaseURL, setBaseURL, setDeviceAuth } from './src/lib/client';
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

  // M3-auth-2B: ONE authoritative completion path shared by cold start and
  // post-pair. completePairing binds the persisted pairing to the actual
  // hardware key on THIS device (identity match) BEFORE creating the TokenManager
  // and entering paired_device. It also installs the host-bound device bearer so
  // the subsequent connect() probe authenticates only against the paired origin.
  const completeAndInstall = useCallback(async (): Promise<AuthContext> => {
    const ctx = await completePairing({
      loadPairing,
      getBaseURL,
      createDeviceKey: createPokitDeviceKey,
      makeTokenManager: (p, dk, base) => new TokenManager(p, dk, base),
    });
    if (ctx.mode === 'paired_device' && ctx.tokenMgr && ctx.baseURL) {
      const { origin } = canonicalOrigin(ctx.baseURL);
      if (origin) setDeviceAuth({ tokenManager: ctx.tokenMgr, origin });
    } else {
      // Any non-paired result must not leave a stale bearer installed.
      setDeviceAuth(null);
    }
    setAuthCtx(ctx);
    return ctx;
  }, []);

  useEffect(() => {
    if (NO_LOGIN) return;
    // Restored paired identity is trusted only after key-identity verification;
    // a missing/mismatched key routes to pairing_required, an inaccessible one
    // to failed — never a stuck paired_device on the wrong key.
        // BUGFIX M3-auth-4A: ConnectionProvider restores BASE_URL from AsyncStorage
        // asynchronously, but completeAndInstall calls getBaseURL() synchronously.
        // Pre-load pairing and seed the global base URL before completeAndInstall.
        const init = async () => {
          try {
            const p = await loadPairing();
            if (p?.baseURL && !getBaseURL()) setBaseURL(p.baseURL);
          } catch {}
          return completeAndInstall();
        };
        init()
          .then(ctx => { if (ctx.mode === 'paired_device' && ctx.baseURL) connect(ctx.baseURL).catch(() => {}); })
          .catch(() => setAuthCtx({ mode: 'failed' }));
  }, []);
  // Sync baseURL into authCtx for explicit_local_dev (NO_LOGIN) mode
  // so deriveTerminalAuth can build absolute daemon URLs for the terminal
  // WebView (needed on emulator where Metro and daemon use different ports).
  useEffect(() => {
    if (!NO_LOGIN || !isConnected || authCtx.mode !== 'explicit_local_dev') return;
    const url = getBaseURL();
    if (url && !authCtx.baseURL) setAuthCtx(prev => ({ ...prev, baseURL: url }));
  }, [isConnected, authCtx.mode]);

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

      const paired = await loadPairing();
      registerPushToken(session.access_token, pushToken, paired?.deviceId).catch(console.error);
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

  async function handleNotificationResponse(response: Notifications.NotificationResponse) {
    const data = response.notification.request.content.data;
    const eventId = data?.['eventId'] as string | undefined;
    const sessionId = data?.['sessionId'] as string | undefined;
    const generation = Number(data?.['generation']);
    // data.activityLink is a locator protocol only; never trust it as an
    // authority decision. Re-authorize before opening any activity target.
    if (!session?.access_token || !eventId || !sessionId || !Number.isFinite(generation)) return;
    try {
      const status = await getNotificationStatus(session.access_token, eventId, sessionId, generation);
      if (status.status === 'actionable' && status.activityLink) await Linking.openURL(status.activityLink);
      else await Linking.openURL(`pokit://session/${encodeURIComponent(sessionId)}`);
    } catch (err) { console.warn('notification status unavailable', err); }
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
        <ConnectScreen onPaired={async () => (await completeAndInstall()).mode === 'paired_device'} localTest={NO_LOGIN} />
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
        // device_connect (paired, authenticated probe pending) | legacy_connect.
        // Pass the SAME atomic completion callback so a re-scan here installs a
        // fresh DeviceKey-verified TokenManager + bearer before connecting —
        // never connecting on top of a stale/invalid auth context.
        <ConnectScreen onPaired={async () => (await completeAndInstall()).mode === 'paired_device'} localTest={NO_LOGIN} />
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
