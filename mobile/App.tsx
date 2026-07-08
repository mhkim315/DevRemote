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

function AppContent() {
  const [session, setSession] = useState<Session | null>(null);
  const { isConnected, loading, baseURL } = useConnection();

  useEffect(() => {
    // E6: skip Supabase auth for localhost — use dev token for fast iteration.
    if (baseURL && (baseURL.includes('localhost') || baseURL.includes('127.0.0.1'))) {
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
  }, [baseURL]);

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

  if (loading) return null;

  return (
    <SafeAreaProvider>
      <StatusBar style="light" />
      {!session ? (
        <AuthScreen />
      ) : !isConnected ? (
        <ConnectScreen />
      ) : (
        <RootTabs token={session.access_token} />
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
