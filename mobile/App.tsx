import React, {useEffect, useRef, useState} from 'react';
import {Platform} from 'react-native';
import {StatusBar} from 'expo-status-bar';
import * as Notifications from 'expo-notifications';
import * as Device from 'expo-device';
import HomeScreen from './src/screens/HomeScreen';
import SessionScreen from './src/screens/SessionScreen';
import {SafeAreaProvider} from 'react-native-safe-area-context';

Notifications.setNotificationHandler({
  handleNotification: async () => ({
    shouldShowAlert: true,
    shouldPlaySound: true,
    shouldSetBadge: false,
    shouldShowBanner: true,
    shouldShowList: true,
  }),
});

async function getPushToken(): Promise<string | null> {
  if (!Device.isDevice) {
    return null;
  }

  const {status: existing} = await Notifications.getPermissionsAsync();
  let finalStatus = existing;

  if (existing !== 'granted') {
    const {status} = await Notifications.requestPermissionsAsync();
    finalStatus = status;
  }

  if (finalStatus !== 'granted') {
    return null;
  }

  const token = await Notifications.getExpoPushTokenAsync();
  return token.data;
}

export default function App() {
  const [wsUrl, setWsUrl] = useState<string | null>(null);
  const pushToken = useRef<string | null>(null);

  useEffect(() => {
    getPushToken().then(t => {
      pushToken.current = t;
    });
  }, []);

  const screen = wsUrl ? (
    <SessionScreen
      wsUrl={wsUrl}
      pushToken={pushToken.current}
      onDisconnect={() => setWsUrl(null)}
    />
  ) : (
    <HomeScreen onConnect={url => setWsUrl(url)} />
  );

  return (
    <SafeAreaProvider>
      <StatusBar style="light" />
      {screen}
    </SafeAreaProvider>
  );
}
