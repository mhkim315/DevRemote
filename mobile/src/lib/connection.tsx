import React, { createContext, useContext, useState, useEffect, useCallback } from 'react';
import AsyncStorage from '@react-native-async-storage/async-storage';
import { setBaseURL, getBaseURL } from './client';

interface ConnectionState {
  baseURL: string;
  loading: boolean;
  connect: (url: string) => Promise<void>;
  disconnect: () => Promise<void>;
}

const ConnectionContext = createContext<ConnectionState>({
  baseURL: '',
  loading: true,
  connect: async () => {},
  disconnect: async () => {},
});

export function useConnection() {
  return useContext(ConnectionContext);
}

export function ConnectionProvider({ children }: { children: React.ReactNode }) {
  const [baseURL, setBaseURLState] = useState('');
  const [loading, setLoading] = useState(true);

  // Restore saved URL on mount.
  useEffect(() => {
    AsyncStorage.getItem('BASE_URL').then((url) => {
      if (url) {
        setBaseURLState(url);
        setBaseURL(url);
      }
      setLoading(false);
    }).catch(() => setLoading(false));
  }, []);

  const connect = useCallback(async (url: string) => {
    setBaseURLState(url);
    setBaseURL(url);
    try {
      await AsyncStorage.setItem('BASE_URL', url);
    } catch (e) {
      console.error('Failed to save BASE_URL', e);
    }
  }, []);

  const disconnect = useCallback(async () => {
    setBaseURLState('');
    try {
      await AsyncStorage.removeItem('BASE_URL');
    } catch (e) {
      console.error('Failed to remove BASE_URL', e);
    }
  }, []);

  return (
    <ConnectionContext.Provider value={{ baseURL, loading, connect, disconnect }}>
      {children}
    </ConnectionContext.Provider>
  );
}
