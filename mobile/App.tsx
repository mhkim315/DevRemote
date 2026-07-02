import React from 'react';
import {StatusBar} from 'expo-status-bar';
import FeedScreen from './src/screens/FeedScreen';
import {SafeAreaProvider} from 'react-native-safe-area-context';

export default function App() {
  return (
    <SafeAreaProvider>
      <StatusBar style="light" />
      <FeedScreen onBack={() => {}} />
    </SafeAreaProvider>
  );
}
