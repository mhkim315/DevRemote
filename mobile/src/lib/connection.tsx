import React, { createContext, useContext, useState, useEffect, useCallback } from 'react';
import AsyncStorage from '@react-native-async-storage/async-storage';
import { setBaseURL, listSessions } from './client';

interface ConnectionState {
  baseURL: string;
  isConnected: boolean;
  loading: boolean;
  connectionError: string;
  connect: (url: string) => Promise<void>;
  disconnect: () => Promise<void>;
}

const ConnectionContext = createContext<ConnectionState>({
  baseURL: '',
  isConnected: false,
  loading: true,
  connectionError: '',
  connect: async () => {},
  disconnect: async () => {},
});

export function useConnection() {
  return useContext(ConnectionContext);
}

export function ConnectionProvider({ children }: { children: React.ReactNode }) {
  const [baseURL, setBaseURLState] = useState('');
  const [isConnected, setIsConnected] = useState(false);
  const [loading, setLoading] = useState(true);
  const [connectionError, setConnectionError] = useState('');

  // Restore saved URL on mount with health check.
  useEffect(() => {
    AsyncStorage.getItem('BASE_URL').then(async (url) => {
      if (url) {
        setBaseURLState(url);
        setBaseURL(url);
        // Verify the saved URL actually works.
        try {
          await listSessions();
          setIsConnected(true);
          setConnectionError('');
        } catch {
          // URL saved but unreachable — show ConnectScreen.
          setIsConnected(false);
          setConnectionError('Saved daemon unreachable. Check connection and retry.');
        }
      }
      setLoading(false);
    }).catch(() => setLoading(false));
  }, []);

  const connect = useCallback(async (url: string) => {
    setBaseURLState(url);
    setBaseURL(url);
    setConnectionError('');
    // Verify before claiming connected.
    try {
      await listSessions();
      setIsConnected(true);
    } catch (e: any) {
      setIsConnected(false);
      const msg = e?.message || String(e);
      setConnectionError('Cannot reach daemon: ' + msg);
      return;
    }
    try {
      await AsyncStorage.setItem('BASE_URL', url);
    } catch (e) {
      console.error('Failed to save BASE_URL', e);
    }
  }, []);

  const disconnect = useCallback(async () => {
    setBaseURLState('');
    setIsConnected(false);
    setConnectionError('');
    try {
      await AsyncStorage.removeItem('BASE_URL');
    } catch (e) {
      console.error('Failed to remove BASE_URL', e);
    }
  }, []);

  return (
    <ConnectionContext.Provider value={{ baseURL, isConnected, loading, connectionError, connect, disconnect }}>
      {children}
    </ConnectionContext.Provider>
  );
}
