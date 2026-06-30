import React, {useState} from 'react';
import {StatusBar} from 'expo-status-bar';
import HomeScreen from './src/screens/HomeScreen';
import SessionScreen from './src/screens/SessionScreen';
import {SafeAreaProvider} from 'react-native-safe-area-context';

export default function App() {
  const [wsUrl, setWsUrl] = useState<string | null>(null);

  if (wsUrl) {
    return (
      <SafeAreaProvider>
        <StatusBar style="light" />
        <SessionScreen wsUrl={wsUrl} onDisconnect={() => setWsUrl(null)} />
      </SafeAreaProvider>
    );
  }

  return (
    <SafeAreaProvider>
      <StatusBar style="light" />
      <HomeScreen onConnect={url => setWsUrl(url)} />
    </SafeAreaProvider>
  );
}
