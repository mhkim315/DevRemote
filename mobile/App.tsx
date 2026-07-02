import React, { useState } from 'react';
import {StatusBar} from 'expo-status-bar';
import FeedScreen from './src/screens/FeedScreen';
import DashboardScreen from './src/screens/dashboard/DashboardScreen';
import {SafeAreaProvider} from 'react-native-safe-area-context';

export default function App() {
  const [currentScreen, setCurrentScreen] = useState<'dashboard' | 'terminal'>('dashboard');

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
