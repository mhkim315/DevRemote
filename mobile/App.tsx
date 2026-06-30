import React, {useEffect, useRef, useState, useMemo} from 'react';
import {StatusBar} from 'expo-status-bar';
import * as Notifications from 'expo-notifications';
import * as Device from 'expo-device';
import HomeScreen from './src/screens/HomeScreen';
import type {ConnectionConfig} from './src/screens/HomeScreen';
import SessionScreen from './src/screens/SessionScreen';
import {SafeAreaProvider} from 'react-native-safe-area-context';
import {WebSocketTransport} from './src/services/WebSocketTransport';
import {WebRTCTransport} from './src/services/WebRTCTransport';
import type {Transport} from './src/services/types';

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
  const [connection, setConnection] = useState<ConnectionConfig | null>(null);
  const pushToken = useRef<string | null>(null);

  useEffect(() => {
    getPushToken().then(t => {
      pushToken.current = t;
    });
  }, []);

  const transport = useMemo<Transport | null>(() => {
    if (!connection) return null;
    if (connection.type === 'ws') {
      return new WebSocketTransport(connection.url);
    }
    return new WebRTCTransport(connection.signalingUrl, connection.code);
  }, [connection]);

  const screen = transport ? (
    <SessionScreen
      transport={transport}
      pushToken={pushToken.current}
      onDisconnect={() => setConnection(null)}
    />
  ) : (
    <HomeScreen onConnect={config => setConnection(config)} />
  );

  return (
    <SafeAreaProvider>
      <StatusBar style="light" />
      {screen}
    </SafeAreaProvider>
  );
}
