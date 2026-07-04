import React, { useState, useEffect } from 'react';
import {StatusBar} from 'expo-status-bar';
import {SafeAreaProvider} from 'react-native-safe-area-context';
import * as Notifications from 'expo-notifications';
import { supabase } from './src/lib/supabase';
import { Session } from '@supabase/supabase-js';
import AsyncStorage from '@react-native-async-storage/async-storage';
import { config } from './src/config';

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
        config.BASE_URL = url;
        setIsConnected(true);
      }
      setLoading(false);
    });
  }, []);

  useEffect(() => {
    async function setupPush() {
      if (!isConnected) return; // Don't register push if no daemon URL
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
      
      fetch(`${config.BASE_URL}/push/register?token=${encodeURIComponent(token)}`)
        .catch(console.error);
    }
    setupPush();
  }, [isConnected]);

  if (loading) return null;

  return (
    <SafeAreaProvider>
      <StatusBar style="light" />
      {!session ? (
        <AuthScreen />
      ) : !isConnected ? (
        <ConnectScreen onConnect={() => setIsConnected(true)} />
      ) : (
        <RootTabs token={session.access_token} />
      )}
    </SafeAreaProvider>
  );
}
