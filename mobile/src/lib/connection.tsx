import React, { createContext, useContext, useState, useEffect, useCallback } from 'react';
import AsyncStorage from '@react-native-async-storage/async-storage';
import { setBaseURL, getBaseURL, probeDaemon, setDeviceAuth, ConnectivityFailure } from './client';

// ── R1a: structured connectivity diagnostics ──

export interface ConnectionState {
  baseURL: string;
  isConnected: boolean;
  loading: boolean;
  connectionError: string;
  /** R1a: was the daemon reachable at all? */
  daemonReachable: boolean;
  /** R1a: did sessions load successfully? */
  sessionsLoaded: boolean;
  /** R1a: did sessions load but return 0 sessions? */
  sessionsEmpty: boolean;
  /** R1a: what kind of failure occurred? */
  failure: ConnectivityFailure;
  connect: (url: string) => Promise<void>;
  disconnect: () => Promise<void>;
  /** R1a: re-probe daemon and refresh diagnostics. Call after API failures. */
  refreshDiagnostics: () => Promise<void>;
}

const ConnectionContext = createContext<ConnectionState>({
  baseURL: '',
  isConnected: false,
  loading: true,
  connectionError: '',
  daemonReachable: false,
  sessionsLoaded: false,
  sessionsEmpty: false,
  failure: ConnectivityFailure.None,
  connect: async () => {},
  disconnect: async () => {},
  refreshDiagnostics: async () => {},
});

export function useConnection() {
  return useContext(ConnectionContext);
}

export function ConnectionProvider({ children }: { children: React.ReactNode }) {
  const [baseURL, setBaseURLState] = useState('');
  const [isConnected, setIsConnected] = useState(false);
  const [loading, setLoading] = useState(true);
  const [connectionError, setConnectionError] = useState('');
  const [daemonReachable, setDaemonReachable] = useState(false);
  const [sessionsLoaded, setSessionsLoaded] = useState(false);
  const [sessionsEmpty, setSessionsEmpty] = useState(false);
  const [failure, setFailure] = useState(ConnectivityFailure.None);

  // Restore saved URL on mount with health check.
  useEffect(() => {
    AsyncStorage.getItem('BASE_URL').then(async (url) => {
      if (url) {
        setBaseURLState(url);
        setBaseURL(url);
        const result = await probeDaemon();
        setDaemonReachable(result.reachable);
        setSessionsLoaded(result.sessionsLoaded);
        setSessionsEmpty(result.sessionsEmpty);
        setFailure(result.failure);
        if (result.reachable && result.sessionsLoaded) {
          setIsConnected(true);
          setConnectionError('');
        } else if (!result.reachable) {
          setIsConnected(false);
          setConnectionError('Daemon unreachable. Check that the daemon is running and the URL is correct.');
        } else if (result.failure === ConnectivityFailure.AuthError) {
          setIsConnected(false);
          setConnectionError('Authentication failed. Re-scan the QR code from your terminal.');
        } else if (result.failure === ConnectivityFailure.APIError) {
          setIsConnected(false);
          setConnectionError('Sessions API error (' + result.statusCode + '). Daemon may need restart.');
        } else if (result.sessionsEmpty) {
          // Daemon reachable, API ok, but zero sessions — not an error.
          setIsConnected(true);
          setConnectionError('');
        }
      }
      setLoading(false);
    }).catch(() => setLoading(false));
  }, []);

  const connect = useCallback(async (url: string) => {
    setBaseURLState(url);
    setBaseURL(url);
    setConnectionError('');
    setFailure(ConnectivityFailure.None);
    // R1a: use structured probe for diagnostics.
    const result = await probeDaemon();
    setDaemonReachable(result.reachable);
    setSessionsLoaded(result.sessionsLoaded);
    setSessionsEmpty(result.sessionsEmpty);
    setFailure(result.failure);

    if (result.reachable && result.sessionsLoaded) {
      setIsConnected(true);
      setConnectionError('');
    } else if (!result.reachable) {
      setIsConnected(false);
      setConnectionError('Daemon unreachable. Check that the daemon is running and the URL is correct.');
    } else if (result.failure === ConnectivityFailure.AuthError) {
      setIsConnected(false);
      setConnectionError('Authentication failed. Re-scan the QR code from your terminal.');
    } else if (result.failure === ConnectivityFailure.APIError) {
      setIsConnected(false);
      setConnectionError('Sessions API error (' + result.statusCode + '). Daemon may need restart.');
    } else if (result.sessionsEmpty) {
      // Daemon reachable but no sessions yet — still "connected".
      setIsConnected(true);
      setConnectionError('');
    } else {
      setIsConnected(false);
      setConnectionError(result.errorMessage || 'Connection failed.');
    }

    try {
      await AsyncStorage.setItem('BASE_URL', url);
    } catch (e) {
      console.error('Failed to save BASE_URL', e);
    }
  }, []);

  // R1a: re-probe daemon to refresh diagnostics after a later API failure.
  // Dashboard calls this when listSessions fails to avoid stale diagnostic state.
  const refreshDiagnostics = useCallback(async () => {
    const url = getBaseURL();
    if (!url) return;
    const result = await probeDaemon();
    setDaemonReachable(result.reachable);
    setSessionsLoaded(result.sessionsLoaded);
    setSessionsEmpty(result.sessionsEmpty);
    setFailure(result.failure);
    if (result.reachable && result.sessionsLoaded) {
      setIsConnected(true);
      setConnectionError('');
    }
  }, []);

  const disconnect = useCallback(async () => {
    setBaseURLState('');
    setIsConnected(false);
    setConnectionError('');
    setDaemonReachable(false);
    setSessionsLoaded(false);
    setSessionsEmpty(false);
    setFailure(ConnectivityFailure.None);
    // Drop the device bearer so a later legacy/local connection can't reuse it.
    setDeviceAuth(null);
    try {
      await AsyncStorage.removeItem('BASE_URL');
    } catch (e) {
      console.error('Failed to remove BASE_URL', e);
    }
  }, []);

  return (
    <ConnectionContext.Provider value={{
      baseURL, isConnected, loading, connectionError,
      daemonReachable, sessionsLoaded, sessionsEmpty, failure,
      connect, disconnect, refreshDiagnostics,
    }}>
      {children}
    </ConnectionContext.Provider>
  );
}
