import React, { useState, useEffect } from 'react';
import {StatusBar} from 'expo-status-bar';
import FeedScreen from './src/screens/FeedScreen';
import DashboardScreen from './src/screens/dashboard/DashboardScreen';
import {SafeAreaProvider} from 'react-native-safe-area-context';
import * as Notifications from 'expo-notifications';

export default function App() {
  const [currentScreen, setCurrentScreen] = useState<'dashboard' | 'terminal'>('dashboard');

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
      
      // Register with our daemon
      fetch(`https://term.fullcount.kr/push/register?token=${encodeURIComponent(token)}`)
        .catch(console.error);
    }
    setupPush();
  }, []);

  return (
    <SafeAreaProvider>
      <StatusBar style="light" />
      {currentScreen === 'dashboard' ? (
        <DashboardScreen onSelectAgent={() => setCurrentScreen('terminal')} />
      ) : (
        <FeedScreen onBack={() => setCurrentScreen('dashboard')} />
      )}
    </SafeAreaProvider>
  );
}
