import React, { useState, useEffect } from 'react';
import { StatusBar } from 'expo-status-bar';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import * as Notifications from 'expo-notifications';
import { supabase } from './src/lib/supabase';
import { Session } from '@supabase/supabase-js';
import AsyncStorage from '@react-native-async-storage/async-storage';
import { getBaseURL, setBaseURL, registerPushToken } from './src/lib/client';
import { ConnectionProvider } from './src/lib/connection';

import AuthScreen from './src/screens/AuthScreen';
import ConnectScreen from './src/screens/ConnectScreen';
import { RootTabs } from './src/navigation/RootNavigator';

export default function App() {
  const [session, setSession] = useState<Session | null>(null);
  const [isConnected, setIsConnected] = useState(false);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    supabase.auth.getSession().then(({ data: { session } }) => {
      setSession(session);
    });

    supabase.auth.onAuthStateChange((_event, session) => {
      setSession(session);
    });

    AsyncStorage.getItem('BASE_URL').then((url) => {
      if (url) {
        setBaseURL(url);
        setIsConnected(true);
      }
      setLoading(false);
    });
  }, []);

  useEffect(() => {
    async function setupPush() {
      if (!isConnected) return;
      const { status: existingStatus } = await Notifications.getPermissionsAsync();
      let finalStatus = existingStatus;
      if (existingStatus !== 'granted') {
        const { status } = await Notifications.requestPermissionsAsync();
        finalStatus = status;
      }
      if (finalStatus !== 'granted') return;

      const tokenData = await Notifications.getExpoPushTokenAsync({ projectId: 'devremote-mhk' });
      const token = tokenData.data;
      console.log('Push token:', token);

      registerPushToken('', token).catch(console.error);
    }
    setupPush();
  }, [isConnected]);

  if (loading) return null;

  return (
    <ConnectionProvider>
      <SafeAreaProvider>
        <StatusBar style="light" />
        {!session ? (
          <AuthScreen />
        ) : !isConnected ? (
          <ConnectScreen onConnect={() => setIsConnected(true)} />
        ) : (
          <RootTabs token={session.access_token} onDisconnect={() => setIsConnected(false)} />
        )}
      </SafeAreaProvider>
    </ConnectionProvider>
  );
}
