import React, { useState, useEffect } from 'react';
import {StatusBar} from 'expo-status-bar';
import FeedScreen from './src/screens/FeedScreen';
import DashboardScreen from './src/screens/dashboard/DashboardScreen';
import AuthScreen from './src/screens/AuthScreen';
import {SafeAreaProvider} from 'react-native-safe-area-context';
import * as Notifications from 'expo-notifications';
import { supabase } from './src/lib/supabase';
import { Session } from '@supabase/supabase-js';

import SnippetsScreen from './src/screens/SnippetsScreen';

export default function App() {
  const [session, setSession] = useState<Session | null>(null);
  const [currentScreen, setCurrentScreen] = useState<'dashboard' | 'terminal' | 'snippets'>('dashboard');
  const [currentSession, setCurrentSession] = useState<string>('devremote');

  useEffect(() => {
    supabase.auth.getSession().then(({ data: { session } }) => {
      setSession(session);
    });

    supabase.auth.onAuthStateChange((_event, session) => {
      setSession(session);
    });
  }, []);

  useEffect(() => {
    async function setupPush() {
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
      
      fetch(`https://term.fullcount.kr/push/register?token=${encodeURIComponent(token)}`)
        .catch(console.error);
    }
    setupPush();
  }, []);

  return (
    <SafeAreaProvider>
      <StatusBar style="light" />
      {!session ? (
        <AuthScreen />
      ) : currentScreen === 'dashboard' ? (
        <DashboardScreen 
          token={session.access_token}
          onSelectAgent={(sess) => {
            setCurrentSession(sess);
            setCurrentScreen('terminal');
          }}
          onSnippets={() => setCurrentScreen('snippets')}
        />
      ) : currentScreen === 'snippets' ? (
        <SnippetsScreen onBack={() => setCurrentScreen('dashboard')} />
      ) : (
        <FeedScreen 
          session={currentSession} 
          token={session.access_token} 
          onBack={() => setCurrentScreen('dashboard')} 
        />
      )}
    </SafeAreaProvider>
  );
}
