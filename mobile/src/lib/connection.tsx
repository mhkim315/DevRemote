import React, { createContext, useContext, useState, useEffect, useCallback } from 'react';
import AsyncStorage from '@react-native-async-storage/async-storage';
import { setBaseURL } from './client';

interface ConnectionState {
  baseURL: string;
  isConnected: boolean;
  loading: boolean;
  connect: (url: string) => Promise<void>;
  disconnect: () => Promise<void>;
}

const ConnectionContext = createContext<ConnectionState>({
  baseURL: '',
  isConnected: false,
  loading: true,
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

  // Restore saved URL on mount. This is the single AsyncStorage read point.
  useEffect(() => {
    AsyncStorage.getItem('BASE_URL').then((url) => {
      if (url) {
        setBaseURLState(url);
        setBaseURL(url);
        setIsConnected(true);
      }
      setLoading(false);
    }).catch(() => setLoading(false));
  }, []);

  const connect = useCallback(async (url: string) => {
    setBaseURLState(url);
    setBaseURL(url);
    setIsConnected(true);
    try {
      await AsyncStorage.setItem('BASE_URL', url);
    } catch (e) {
      console.error('Failed to save BASE_URL', e);
    }
  }, []);

  const disconnect = useCallback(async () => {
    setBaseURLState('');
    setIsConnected(false);
    try {
      await AsyncStorage.removeItem('BASE_URL');
    } catch (e) {
      console.error('Failed to remove BASE_URL', e);
    }
  }, []);

  return (
    <ConnectionContext.Provider value={{ baseURL, isConnected, loading, connect, disconnect }}>
      {children}
    </ConnectionContext.Provider>
  );
}
